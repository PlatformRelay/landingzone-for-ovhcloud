# ADR-0004: Staged roots, separate state, typed output contracts
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0003, ADR-0005, ADR-0009

## Context
Azure's `caf-enterprise-scale` was retired for a monolithic state, hard permission scoping, many
provider aliases and a huge variable surface. Google FAST and example-foundation use staged roots with
output contracts; AWS LZA failures can force tearing down stacks in reverse order. Smaller state units
let permissions be scoped per layer and failures be recovered without a full teardown.

## Options considered
- **One root, many modules** — simplest, monolith risks above.
- **Staged roots, each its own state**, data passed through a contract.
- **Per-project roots** (one root per OVH project) — high fan-out, strong blast-radius isolation.

## Decision (proposed)
Staged roots forming **one composition graph** (`stages/`, ADR-0016), driven by a profile; tenants are
`for_each` instances over tenant files (ADR-0005) with **per-tenant state keys** (ADR-0009).

Stage set (fixed names; a profile may leave a stage empty, never reorder it):
`00-bootstrap` (manual, once; recoverable via `import`) → `10-account` (deny-floor, groups, roles,
federation hand-off, break-glass, audit sink) → `20-network` (island by default; hub-vrack variant)
→ `30-tenants` (projects, quotas, budget alerts, Keystone machine identities, resource groups)
→ `40-runtimes` (one runtime variant per tenant environment, ADR-0017) → `50-observability`
(streams, alerting per project) → `90-workloads` (examples only).
Separate stages for tenants and runtimes keep a broken cluster from blocking project vending, and
let the two-pipeline rule (ADR-0009) scope credentials per stage.

**Output contract**
- Each stage writes a `stage-outputs.json` (and publishes the same data as tofu outputs) validated against
  a JSON Schema in `schemas/`. Downstream stages read **only** the contract, via the pipeline-provided
  file or `terraform_remote_state` fed from it — never by reaching into another stage's resources.
- Contract changes follow the same semver rules as module interfaces (ADR-0010).
- A stage declares in its manifest: inputs it consumes, outputs it publishes, OVH API permissions it
  needs, and which service account applies it (least privilege per stage).

**Safe by default**: no stage deletes resources it does not own; `prevent_destroy` on state-bearing and
identity-bearing resources; plan-diff review gate shows destroys prominently (AWS LZA v1.16 lesson).

## Consequences
- Stage ordering is explicit; failure at stage N leaves stages < N intact.
- More pipelines and more state buckets/keys; the Taskfile contract (ADR-0007) hides it.
- Cross-stage refactors need `moved` blocks and a documented migration note.

## Counterpoints
- Remote-state coupling is a known Terraform smell; the typed file contract mitigates but adds a
  generation step.
- Seven stages are heavy for `solo`; a profile leaves stages empty (`20-network` island needs no
  hub, `50-observability` may be `none`), so the stage count is constant but the work is not.

## Verification
- Spike: two stages exchanging a contract file in CI on GitHub Actions and GitLab CI without remote state.

## Review log
- 2026-10-01 revision: stage names aligned with the profile model; tenants and runtimes split.
  Source: agent-context/research/BRAINSTORM-2026-10-01-round2.md.
