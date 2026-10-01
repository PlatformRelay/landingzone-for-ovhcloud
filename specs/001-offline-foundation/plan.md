# Implementation Plan: Offline foundation, verification and naming
Date: 2026-10-01 · Spec: [spec.md](spec.md) · Status: draft

## Summary
Build a narrow Go-based check/report harness around the first provider-free naming module.
Use Task as the local/CI interface. Do not add an agent framework or provision cloud resources.

## Technical Context
- Language: OpenTofu HCL, Go (one tools/go.mod), JSON Schema, YAML, thin Task/Bash glue.
- First implementation pins: OpenTofu 1.13.0, Terramate 0.17.3, ovh/ovh 2.21.0 when
  a provider fixture is introduced. Other tools and the offline image get exact versions
  and verified digests in the initial toolchain task, before fixtures are captured.
- Target: Linux amd64 CI with a digest-pinned OCI image; no cloud credentials or mounts.
  Separate credential-free preparation downloads providers into a checksum-verified cache.
  Local `task check` uses the prepared image with network=none; absence of runtime/cache fails.
  CI uses a protected verification workflow at an explicitly approved base revision, triggered
  by maintainer dispatch with a candidate SHA. Its trusted launcher obtains a candidate archive
  as data, validates rooted paths, then invokes candidate commands only inside network=none,
  with no runtime socket, host credentials, privileged mounts or protected cache writes.
  Candidate workflow/include/Taskfile edits cannot change the trusted launch configuration.
  Repository-scoped fetch/result tokens remain in the protected outer job and are never
  mounted inside candidate execution. The published check binds candidate/base/launcher/image
  digests and expected publisher; required-check origin enforcement must be qualified.
  Ordinary candidate CI is advisory until that trusted result exists; no pull_request_target
  checkout of fork code. This certifies parent-run fork checks, not fork-owned infrastructure.
- Storage: tracked schemas, independent vectors and sanitised tool fixtures; raw results in
  gitignored `.local/evidence/`. No hosted service, cloud state or telemetry.
- Tests: Go unit/contract tests, pure `tofu test` plan tests, Conftest and assent fixtures,
  snapshot diffs; clause-specific mutations of each new gate. Reports preserve tool messages.
- Budget: EUR 0 cloud spend. Naming changed-path suite <=120s after preparation on
  exclusive self-hosted runner with fixed CPU/memory and digest-pinned OS/image;
  no overlapping jobs, and cache preparation is recorded; unknown allocation blocks measurement. Initial CI total <=10min, with preparation time reported separately.

## Constitution Check
| Principle | Before research / after design |
| --- | --- |
| I Evidence | Premises listed; only bounded proof tasks can start with UNVERIFIED mechanisms; dependent implementation blocked. |
| II Traceability | Spec acceptance V checks predefined; task generation must map creators, requirements, outcomes and evidence. Docs-only exemption retained. |
| III Test first | Behaviour controls precede implementation; every sensor clause challenged; actual pinned output fixtures required. |
| IV Authority | One state owner, trusted evaluator and protected execution boundaries explicit; no authoring cloud authority. |
| V Recovery | Missing observations fail closed; private evidence and separate cleanup/recovery paths required. Live readiness remains blocked on setup. |
| VI Scope | Bounded first increment; no supported-tuple claim, broad scaffolding or runtime rollout. |
| VII Useful outcomes | Preserve the reference baseline and differentiators; existing acceptance cases cover visible reports/status and failure paths; no new portal/CLI implied. |
| VIII Decisions | Approved direction is distinct from pending interfaces; phase 001 naming T013 gates the joint choice; dependent work respects that gate. |


Draft design conforms; this table is a review of the design, not evidence that future gates passed.


## Project Structure
Implementation paths: see spec.md's Implementation surface; no generic src/ tree.
Feature artefacts: research.md, data-model.md, contracts/checks.md, quickstart.md, tasks.md.

## Delivery phases
1. Setup: test the pin verifier and isolated execution boundary, then build Task targets
   and offline image preparation. Capture real 1.13 passing and failing JSON streams.
2. US1: test the report adapter and traceability/DoD oracle; implement only fields consumed
   by CI and naming. Empty discovery and absent evidence must be behavioural failures.
3. US2: independently author naming vectors first; implement two organisation templates,
   labels and catalogue applicability. Synthetic limits may test algorithms; unknown actual
   limits are refused for cloud-ready kinds. Add projections and snapshots without overriding
   the subject. Generate only projection data/vectors, not generic artefact scaffolds.
4. US3: publish a short guide router and decision map; test stable actionable diagnostics
   and review-required status with the seeded diagnostic/evidence fixtures in T017. Add both offline forge adapters
   and run green/red controls on disposable repos before claiming portability. Mutate the
   candidate Taskfile, workflow and include to attempt host execution before isolation;
   qualification must show these edits cannot alter the immutable trusted launcher.

## Verification strategy
Applicable L0 is explicit: `tofu fmt -check -recursive modules/naming`,
`tofu -chdir=modules/naming init -backend=false` from prepared cache,
`tofu -chdir=modules/naming validate -json`, `tflint --chdir=modules/naming --format=json`
with committed exact built-in rule config, and strict names/org-label-schema validation.
T008 writes valid and malformed/unformatted/linter fixtures plus empty-discovery and
each-clause controls; T009 creates `task lint`/`task test:static`; T016 creates
`task schema:check`. The final latency/exit gate consumes those actual observations.
Provider-resource trivy checks are not-applicable to this resource-free naming module,
not an empty green scan; custom tflint extensions wait for a real resource-module consumer.
Docs-only checks are exempt by the user request; module interface docs are generated
with implementation and reviewed, without inventing docs acceptance tests.
One check registry in harness/checks.yaml records id, requirement, artefact kind, command,
creator task, discovery scope, fixture provenance and evidence status. `task check:specs`
validates mappings; `task dod -- <path>` evaluates implemented checks and displays missing
checks as not-run. Docs-only tasks remain explicitly exempt, not falsely passed.
See spec V001–V008 and contracts/checks.md. Every clause of every new sensor gets a
mutation/control; an unchanged green result after removing the guarded behaviour blocks it.
Fixture capture is from pinned tool output, never copied from upstream prose.
The speed check runs the complete applicable naming suite and records discovery counts;
it cannot meet its budget by dropping a layer. A provider-free module uses real pure HCL
for L1/L2 and saved-plan output snapshots for L3, not fake provider output.

## Dependencies and live change ordering
Tool preparation → isolation → report/registry → module + projections → forge checks.
Guide authoring can parallel the module after the interface is fixed. No live objects change.
OpenTofu/tool version and runtime gates stop implementation testing, not spec editing.
Full ADR-0019 scaffolding follows two real examples per kind in the first vertical slice;
this phase supplies the registry/guides/diagnostics and makes the deferred duties explicit.

## Complexity tracking
Go is already the ADR-0002 tools language; use standard libraries plus the least mature
schema/YAML parser needed (choose exact pins before fixtures). Do not build custom tflint
plugins, a broad policy compiler, replay proxy or release system here. The pure naming
module gives cheap evidence while cloud naming constraints remain separately unverified.
