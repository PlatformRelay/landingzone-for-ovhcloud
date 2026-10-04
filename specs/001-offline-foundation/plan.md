# Implementation Plan: Offline foundation, verification and naming
Date: 2026-10-01 · Spec: [spec.md](spec.md) · Status: draft

## Summary
Build a narrow Go-based check/report harness around the first provider-free naming module.
Use Task as the in-child check interface; use a separately approved launcher as the host entry.
Do not add an agent framework or provision cloud resources.

The current goal (2026-10-02, local decision D61) covers all locally implementable portions
of specs 001–004 whose dependencies and required decisions are satisfied. T001–T009 remain
minimum safety: toolchain, local isolation, reports, trace/DoD and dependency/static checks;
conditional GitHub CI bootstrap T023 follows their evidenced completion. Naming, forge
adapters, latency and full exit retain their own prerequisites and qualification duties.
The full feature remains draft. Use TDD, actual applicable checks and independent review,
then publish coherent increments as scoped PRs, including stacks with explicit parent
branches and review order. Continue eligible work without waiting for operator review;
a PR merges after an independent review and green applicable checks.
Structure, constitution ratification and committed workflow decisions are confirmed.
Add directories with their first real artifact. Minimum safety precedes dependent
IAM/state implementation; independent documentation can proceed earlier. Live authority
and operating prerequisites remain separate. D29 still defers hardened persistence/export
until real profile schemas exist.

The 2026-10-02 operator priority exception (local decision D60) accepts C2 (C010.5/P4)
as **DEFERRED**, nonblocking technical debt [KI-001](../../docs/known-issues/KI-001-ci-source-admission-unqualified.md) for current private
maintainer development, publication, review PR creation and merge. Frozen workflows,
a no-bypass ruleset and the disposable source-admission experiment are later obligations,
not immediate T023 prerequisites. Review PR creation does not establish merge readiness:
tests/evidence, exact pins, read-only authority, T003 isolation, exact-head CI and
independent review remain required. Whole-feature hardened guarantees remain unqualified;
the constitution and ADRs are unchanged.

## Technical Context
- Language: OpenTofu HCL, Go (one tools/go.mod), JSON Schema, YAML, thin Task/Bash glue.
- First implementation pins: OpenTofu 1.13.0, Terramate 0.17.3, ovh/ovh 2.21.0 when
  a provider fixture is introduced. Other tools and the offline image get exact versions
  and verified digests in the initial toolchain task, before fixtures are captured.
- Target: Linux amd64 CI with a digest-pinned OCI image; no cloud credentials or mounts.
  Separate credential-free preparation downloads providers into a checksum-verified filesystem
  mirror and lockfile. Explicit provider installation has no direct/network fallback.
  T003 creates `lz-offline`, installed at an approved absolute path outside the candidate tree
  with independently reviewed source/binary digest. It treats the checkout as data, ignores
  candidate Taskfiles/includes/hooks/configuration on the host, and enters the prepared image
  with network=none before invoking `task check`. Runtime/image/mirror absence fails.
  Candidate code cannot build, replace or select this host launcher; preparation/build/install
  of the approved closure is separate from candidate execution. All Task shortcuts below
  denote child commands, never a host command that loads a candidate Taskfile.
  Full forge qualification later uses a protected verification workflow at an explicitly approved base revision, triggered
  by maintainer dispatch with a candidate SHA. Its trusted launcher obtains a candidate archive
  as data, validates rooted paths, then invokes candidate commands only inside network=none,
  with no runtime socket, host credentials, privileged mounts or protected cache writes.
  Candidate workflow/include/Taskfile edits cannot change the trusted launch configuration.
  Repository-scoped fetch/result tokens remain in the protected outer job and are never
  mounted inside candidate execution. The published check binds candidate/base/launcher/image
  digests and expected publisher; required-check origin enforcement must be qualified.
  Ordinary candidate CI is advisory until that trusted result exists; no pull_request_target
  checkout of fork code. This certifies parent-run fork checks, not fork-owned infrastructure.
  T023 supplies minimal CI for the owned foundation branch after T001–T009 pass.
  Independently reviewed YAML uses full-SHA Actions and an approved digest-pinned
  launcher/image closure outside the candidate; no host checkout/build/source of
  candidate scripts. An ephemeral GitHub-hosted runner provides contents-read fetch,
  isolated candidate commands and a 10-minute timeout. Tokens remain outside the child.
  Actual reviewed source/head/image/publisher metadata binds the result.
  C2 source enforcement remains DEFERRED to [KI-001](../../docs/known-issues/KI-001-ci-source-admission-unqualified.md) under D60,
  not a prerequisite for current private maintainer publication/PR/merge. No workflow/
  ruleset enforcement is qualified. T010–T012 full fork/both-forge/publisher duties remain.
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
| VI Scope | Dependency-led bounded increments across eligible specs; no supported-tuple claim, broad scaffolding or runtime rollout. |
| VII Useful outcomes | Preserve the reference baseline and differentiators; existing acceptance cases cover visible reports/status and failure paths; no new portal/CLI implied. |
| VIII Decisions | Scalar naming/shared context and handwritten independent projections are selected; module splitting needs concrete-consumer diagnosis. Naming tasks are eligible under D61 after their dependencies and this diagnosis. |


This table reviews the full design, not evidence that future gates passed. D60 defers
C2 implementation within the private increment; it does not qualify the hardened design.


## Project Structure
Implementation paths: see spec.md's Implementation surface; no generic src/ tree.
Feature artefacts: research.md, data-model.md, contracts/checks.md, quickstart.md, tasks.md.

## Delivery phases
1. Setup: T001 selects pins, prepares only independently approved external source and
   records version/provenance metadata. It creates a separately approved bounded control
   driver and proves preparation identity/trust/config rejection and cleanup before closure.
   T002 creates the approved external proof driver/tests/compiling stubs with valid and
   behavioural red capture/isolation controls. T003 implements the actual preparation/entry,
   toolchain target and capture admission; approve source/build closure before build, then
   binary/image before installation, and repeat controls green. Only then may T004 capture
   real 1.13 passing/failing JSON streams. See contracts/checks.md for exact control owners
   and private driver boundaries; no candidate configuration executes on the host.
2. US1: test the report adapter and traceability/DoD oracle; implement only fields consumed
   by CI and naming. Empty discovery and absent evidence must be behavioural failures.
3. US2: independently author naming vectors first; implement two organisation templates,
   labels and catalogue applicability. Synthetic limits may test algorithms; unknown actual
   limits are refused for cloud-ready kinds. Add projections and snapshots without overriding
   the subject. Handwrite evaluator/policy projections and independent expected vectors;
   generate documentation initially, not evaluators or tests.
4. US3: publish a short guide router and decision map; test stable actionable diagnostics
   and review-required status with the seeded diagnostic/evidence fixtures in T017. Add both offline forge adapters
   and run green/red controls on disposable repos before claiming portability. Mutate the
   candidate Taskfile, workflow and include to attempt host execution before isolation;
   qualification must show these edits cannot alter the immutable trusted launcher.

## Verification strategy
Applicable L0 is explicit: `tofu fmt -check -recursive modules/naming`,
`tofu -chdir=modules/naming init -backend=false -lockfile=readonly` with the prepared mirror-only CLI configuration,
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
See spec V001–V009 and contracts/checks.md. Every clause of every new sensor gets a
mutation/control; an unchanged green result after removing the guarded behaviour blocks it.
Fixture capture is from pinned tool output, never copied from upstream prose.
The speed check runs the complete applicable naming suite and records discovery counts;
it cannot meet its budget by dropping a layer. A provider-free module uses real pure HCL
for L1/L2 and saved-plan output snapshots for L3, not fake provider output.

## Dependencies and live change ordering
Approved T001 preparation and control proof → approved T002 valid/red boundary driver →
approved T003 actual entry and green capture/isolation gate → T004 tool-output capture →
report/registry → module + projections → forge checks. No later-created gate is a T001
prerequisite; T001 instead owns preparation refusal/cleanup before its outputs are consumed.
T001–T009 establish the report/registry and dependency/static foundation; D61 permits
subsequent eligible tasks rather than ending the run at that minimum. A blocked task
does not stop other independent work whose prerequisites are satisfied.
T023 may then bootstrap only those implemented checks; missing foundation evidence
blocks it. C2 source admission remains deferred to KI-001. A valid actual GitHub PR-head
run and a failing behavior control remain required for merge, not review PR creation.
Its real-tool controls use fixture modules; absent modules/naming checks stay not-run.
Schema validation joins at T016. Partial V001/V006 observations cannot close their
later latency/schema/forge duties or claim a full requirement pass.
Guide authoring can parallel the module after the interface is fixed. No live objects change.
OpenTofu/tool version and runtime gates stop implementation testing, not spec editing.
Full ADR-0019 scaffolding follows two real examples per kind in the first vertical slice;
this phase supplies the registry/guides/diagnostics and makes the deferred duties explicit.

## Complexity tracking
Go is already the ADR-0002 tools language; use standard libraries plus the least mature
schema/YAML parser needed (choose exact pins before fixtures). Do not build custom tflint
plugins, a broad policy compiler, replay proxy or release system here. The pure naming
module gives cheap evidence while cloud naming constraints remain separately unverified.
