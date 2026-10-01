# ADR-0008: Testing strategy
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0005, ADR-0006, ADR-0007, ADR-0010

## Context
Requirement: "very extensive testing of everything". On a billed platform with slow resources (managed
Kubernetes, databases) and order-based projects, extent has to be bought with cheap layers, not by
applying everything on every PR. Prior art: AVM (`avm pr-check`, TFNFR rules, example deployments),
Cloud Foundation Fabric (plan-only inventory tests, README examples executed), LZA (schema validation
before deploy), terraform-aws-modules (pre-commit, examples deployed in CI).

## Options considered
Native `tofu test` only; Terratest only; layered pyramid mixing both.

## Decision (proposed)
A pyramid, cheapest first. Each layer has a Taskfile target (ADR-0007) and a stated cost.

| # | Layer | Runs | Cost | Tool |
|---|---|---|---|---|
| 1 | Static | every PR, pre-commit | none | `tofu fmt/validate`, tflint, Trivy config, terraform-docs drift, YAML/JSON-Schema validation, markdown/link lint, layer-dependency check |
| 2 | Unit | every PR | none | `tofu test` with `command = plan` + `mock_provider`; every module has tests |
| 3 | Policy | every PR | none | conftest/OPA on plan JSON; each policy has pass/fail fixtures |
| 4 | Contract | every PR | none | `CONTRACT.yaml` vs `tofu`-extracted interface; breaking change without major bump fails |
| 5 | Example | every PR | none | every `examples/` dir `init/validate/plan` offline against mocks; README snippets extracted and run |
| 6 | Snapshot | every PR (few stacks) | none | normalised plan JSON golden files for 2–3 blueprints; diffs reviewed |
| 7 | Integration | nightly + release branches | **real money** | `tofu test` `command = apply` or scripted apply/destroy in the sandbox (below) |
| 8 | End-to-end | weekly + before release | **real money** | full blueprint from the starter into a throwaway project; verify via API |
| 9 | Drift | weekly | low | `tofu plan -detailed-exitcode` on the long-lived sandbox, scanner run (ADR-0006) |

**Cost and safety for 7–9**: dedicated sandbox OVH account/project(s) with a billing alert; mandatory
`ttl` tag on everything; a **janitor** job destroys anything past TTL; tests use the smallest flavors and
regions; a **pool** of pre-ordered projects avoids ordering per test (ADR-0005); a budget kill-switch
disables the integration job if month-to-date spend exceeds a configured ceiling.

**Mocking limits**: `mock_provider` can mask provider-side validation. Mitigation: layer 7 exists for every
module; mocks use values copied from real runs (`tests/fixtures/`) and the fixture refresh is a task.

**Coverage rule**: every module has layers 1, 2, 4, 5; every blueprint has 5, 6, 8; every policy has 3.
Coverage is reported as a table in CI (module × layer), not a percentage.

Terratest is **not** used by default; allowed only when probing live endpoints after apply (HTTP/SSH).

## Consequences
- Contributors without an OVH account can complete layers 1–6 (the "zero-cost mode").
- CI needs a secret-gated lane for 7–9 that forks cannot trigger.

## Counterpoints
- Plan snapshots are noisy; limited to a few high-value stacks.
- A single maintainer cannot babysit nightly cost jobs; the budget kill-switch and janitor are mandatory,
  not optional.

## Verification
- Spike: can the `ovh` provider be mocked usefully (computed attributes, nested blocks)?
- Spike: real-apply cost and time of a minimal project baseline; derive the monthly sandbox budget.

## Review log
_(empty)_
