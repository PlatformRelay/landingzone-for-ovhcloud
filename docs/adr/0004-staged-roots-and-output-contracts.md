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
Staged roots, with per-project roots *inside* the project-factory stage via `for_each` over factory files
(ADR-0005) and a documented upgrade path to per-project roots if state size demands it.

Default stage set for the reference blueprint (names fixed in the blueprint manifest):
`00-bootstrap` (manual, once) → `10-identity` → `20-network` → `30-projects` → `40-observability` →
`90-workloads` (examples only).

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
- Six stages may be too coarse for small installations: the starter ships a `minimal` scenario with
  three stages.

## Verification
- Spike: two stages exchanging a contract file in CI on GitHub Actions and GitLab CI without remote state.

## Review log
_(empty)_
