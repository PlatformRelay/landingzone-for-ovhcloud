---
references: []
last_verified: 7406a5791afb1ea7049f63c3cae03583d3a3c3c0
---

# Glossary

The project's own words and id families, one or two lines each. Where an id family has a
canonical table, the entry links it instead of copying it. Most ids belong to one spec: `005/T049`
is task T049 of spec 005, and a bare id means the spec under discussion. The layers and the stage
order are drawn in the [architecture overview](../explanation/architecture-overview.md).

## Building blocks

| Term | Meaning |
| --- | --- |
| **module** | Layer 1, under `modules/`: a thin wrapper over one OVHcloud product concern. It may call only `modules/naming`. [ADR-0003](../adr/0003-layered-taxonomy-and-module-naming.md) |
| **component** | Layer 2, under `components/`: a landing-zone feature built from modules. Either a family with variants (`components/<family>/<variant>`, such as `identity/ovh-native`) or a singleton (`state-backend`, `project-factory`). |
| **family / variant** | A component kind with interchangeable implementations behind one output contract, such as runtime `managed-only`. Only one variant per family exists today. [ADR-0017](../adr/0017-runtime-families-and-output-contract.md) |
| **stage** | Layer 3, under `stages/`: reusable code for one deployment step (`bootstrap`, `tenant-state`, `account-governance`, `project`, `project-network`, `runtime`). It calls one component and has no backend and no credentials. [ADR-0004](../adr/0004-staged-roots-and-output-contracts.md) |
| **stack** | One deployed copy of a stage: a generated directory under `stacks/` with its own state, backend key and lock. "Stage = reusable code; stack = one deployed instance of it." |
| **instance** (deployment instance) | One row of `stacks/deployments.yaml`, with an immutable `id` such as `demo-dev-project`; each instance becomes one stack. |
| **`deployments.yaml`** (manifest) | `stacks/deployments.yaml`, the list of instances. Edit a row, then `task stacks:reconcile` creates missing stack directories and `task stacks:generate` renders their `_lz_*` files. |
| **tenant / environment / region / slot** | The dimensions that identify an instance. A slot (runtime only) tells two runtimes in one region apart. |
| **envelope / `outputs.json`** | What a stack publishes after an apply, checked against `schemas/outputs/`. The only way stacks share values; no stack reads another's state. |
| **data edge / authority edge** | Why one stack runs after another: it reads the other's outputs (data), or it runs as a principal or with state credentials the other creates (authority). |
| **retained / ephemeral** | Whether a live chain keeps a stack (state buckets, identities, the project) or destroys it on exit (`project-network`, `runtime`). Fixed per stage in `tools/internal/stacks/stages.go`. |
| **profile / golden path** | A preset over the composition graph for one kind of adopter. None exists yet. [ADR-0016](../adr/0016-golden-paths-as-profiles.md) |

## Identity and runs

| Term | Meaning |
| --- | --- |
| **authority / principal** | Who applies a stage: `bootstrap` (the account admin service account), `platform` or `tenant` (deployer clients created by `account-governance`). A stack gets only its authority's credentials. |
| **offline entry / `lz-offline`** | The digest-pinned sandbox that runs every CI check with the pinned toolchain, no network and no credentials. |
| **candidate** | The checkout an offline run judges, given as `--candidate <path>`; the entry snapshots it read-only and runs the checks on a private copy, so the checkout is never written. |
| **live lane / `lz-live`** | The host-side path to a real account: `task bootstrap:account` and the `live:*` targets, run by the maintainer only. |
| **host guard** | `lz-live`'s refusal to run inside the offline entry, from a linked worktree, on a dirty tree or at a commit other than the reviewed one on `origin/main`. |
| **owner session** | A task only the maintainer can run, because it needs the live lane and account credentials. |
| **lane** | A path a run is allowed to take, as in *live lane* above. In the delivery record it also means one unit of work on its own branch. |
| **chain** | `task live:chain`: apply the selected stacks in order, destroy the ephemeral ones in reverse, then run the leftover check. |
| **probe** | A small live root under `tests/live/probes/` that tests one premise against a real account through `task live:probe`, then destroys itself. |
| **run id / leftover check / inventory** | Each live run gets an id (such as `20261013T075309Z-443f`) and records every resource it creates (the inventory). Afterwards a read-only listing of the account must find nothing unexpected; only what the retained stacks' states hold, and the admin client and policy, are exempt. |
| **sandbox** | The maintainer's trial OVHcloud account, the only account the live lane has used. |

## Id families

| Id | Meaning | Canonical list |
| --- | --- | --- |
| **FR-n / SC-n** | Functional requirement / success criterion of a spec. | [spec 005: requirements](../../specs/005-first-landing-zone-slice/spec.md#functional-requirements), [success criteria](../../specs/005-first-landing-zone-slice/spec.md#success-criteria) |
| **Tnnn** | A task in a spec's `tasks.md`. `evidence/Tnnn.md` is the record that closed it. | [spec 005 tasks](../../specs/005-first-landing-zone-slice/tasks.md) |
| **Vnnn** | A predefined acceptance check of a spec, with positive and negative controls. | [spec 005 V-checks](../../specs/005-first-landing-zone-slice/contracts/checks.md#v-checks) |
| **Pn** | A premise about OVHcloud or tool behaviour, UNVERIFIED until a probe or a capture observes it. Not to be confused with a user story's priority (P1–P3). | [spec 005 premises](../../specs/005-first-landing-zone-slice/spec.md#premises) |
| **KD-n** | A known deviation from the design, accepted with a condition for lifting it (KD-1: shared sandbox project; KD-2: live runs from a workstation; KD-3: `project` state in the tenant bucket). | [spec 005 known deviations](../../specs/005-first-landing-zone-slice/spec.md#known-deviations) |
| **Rn** | A research item of a spec. | [spec 005 research](../../specs/005-first-landing-zone-slice/research.md) |
| **Gn** | A security guard whose test must be shown to fail when the guard is weakened (mutation proof). | [spec 005 guards](../../specs/005-first-landing-zone-slice/contracts/checks.md#security-guards-with-mandatory-mutation-proof-fr-013) |
| **USn** | A user story of a spec (US1 … US5 in spec 005). | [spec 005 user scenarios](../../specs/005-first-landing-zone-slice/spec.md#user-scenarios--testing) |
| **Dnn** | An operator decision. The decision log is kept by the maintainers outside the repository; records cite the number only. | — |
| **L0–L10** | The test layers: L0 static, L1 unit, L2 contract … L7 live apply, L8 golden-path end to end. The slice uses L0, L1, two L2 variants and L7. | [ADR-0008](../adr/0008-testing-strategy.md), [spec 005 R17](../../specs/005-first-landing-zone-slice/research.md#r17-test-layers-for-this-slice) |
| **KI-nnn** | A known issue: a verified finding or accepted limitation. | [known issues](../known-issues/README.md) |
| **LZ-xxx-nnn** | A rule id from the patterns catalogue, such as `LZ-HCL-001`. | [patterns catalogue](patterns-catalogue.md) |
| **ADR-nnnn** | An architecture decision record. | [ADR index](../adr/README.md) |

## Process words

| Term | Meaning |
| --- | --- |
| **evidence** | The record that closes a task (`specs/<spec>/evidence/Tnnn.md`): what ran, its result, and what was not verified. |
| **outcome words** | `pass`, `fail`, `blocked`, `not-run`, `review-required`, and for live assertions `known-deviation <id>`. A missing tool or an empty result is never `pass`. |
| **capture** | Recorded output of a pinned tool, kept as a test fixture (the `capture:*` targets). |
| **DoD / `task dod`** | The definition-of-done checks for a path, such as `task dod -- modules/naming`. |
| **assent** | The planned self-service merge gate for tenant changes. [ADR-0005](../adr/0005-declarative-tenant-model-and-assent.md) |
| **TACO** | A Terraform/OpenTofu automation platform such as Atlantis or Spacelift. [ADR-0007](../adr/0007-pipeline-portability.md) |
| **forge** | The Git hosting service: GitHub or GitLab. |
