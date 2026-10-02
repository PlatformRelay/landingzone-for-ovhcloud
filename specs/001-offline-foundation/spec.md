# Feature Specification: Offline foundation, verification and naming
Created: 2026-10-01 · Status: draft · ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021

## Why and scope
Give contributors a free, deterministic feedback loop before any cloud resource exists.
Deliver the first pure naming module and the verification harness it exercises.
Exclude provisioning, auto-merge execution, runtime families, cloud scans and release claims.

## Authorized delivery scope
The current goal (2026-10-02, local decision D61) authorizes all locally implementable
portions of specs 001–004 whose dependencies and required decisions are satisfied, with
tests first, predefined evidence and independent reviews. Publish coherent increments,
including stacked PRs with explicit parent branches and review order; continue eligible
work without waiting for operator review. No PRs may be merged during this goal; earlier
merge permission is superseded. T001–T009 remain minimum safety before dependent work;
conditional T023 adds only minimal GitHub CI after those tasks have evidenced completion.
D29's hardened persistence/export deferral until real profile schemas exist remains in force.
The 2026-10-02 operator priority exception (local decision D60) accepts C2 as
**DEFERRED**, nonblocking technical debt [KI-001](../../docs/known-issues/KI-001-ci-source-admission-unqualified.md)
for current private maintainer development, branch publication, review PR creation and
merge. C010.5/P4 remain owed, not passed; whole-feature hardened guarantees remain
unqualified. This exception changes neither the constitution nor ADRs. Creating a review
PR does not establish merge readiness: actual checks, exact-head CI and independent review remain required.
ADR-0002 is Accepted,
constitution 1.2.0 is ratified and reusable `.specify/` scaffolding stays committed.
Create each implementation directory with its first real artifact. T010–T022 become
eligible according to their own prerequisites; no whole-feature V/FR/SC pass or supported
tuple is claimed from the foundation subset. Dependent IAM/state implementation follows
minimum safety, with separate live authority and prerequisites. Independent documentation
may proceed earlier; a blocked task does not stop other eligible work.

## User Scenarios & Testing
### User Story 1 — Trust local feedback (Priority: P1)
A contributor checks a change with no cloud authority. Given a valid module, the check
reports what ran; given a missing tool, empty discovery or malformed report, it fails visibly.
Independent test: V001–V003 and V006–V007.
### User Story 2 — Resolve names and ownership labels (Priority: P2)
An organisation supplies its own segment order and hierarchy. Given either of two
organisation templates, names are stable and required labels exist; impossible templates,
authorisation-key overrides and invalid limits are rejected. Independent test: V004–V005.
### User Story 3 — Find the next correct action (Priority: P3)
A new maintainer follows the guide and gets a rule-specific diagnostic and an honest DoD
status, including review obligations. Independent test: V008 and V003.

## Premises
| ID | Mechanism premise | Probe | Evidence status |
| --- | --- | --- | --- |
| P1 | OpenTofu 1.13 features and JSON stream match our proposed minimum | Capture version and passing/failing `tofu test -json` using pinned binary | UNVERIFIED; exact project pins must be prepared and captured |
| P2 | Offline isolation blocks subprocess cloud calls, not merely top-level calls | Run an intentional outbound probe inside the same container as provider tests | UNVERIFIED; implementation acceptance V001 |
| P3 | Resource naming/tag limits and metadata applicability are known per kind | Primary-source catalogue rows and sandbox probes where source is insufficient | UNVERIFIED; unknown kinds cannot be advertised as supported |
| P4 | GitHub admits candidates with frozen approved workflow source and refuses every unreviewed workflow path before execution | Later source-admission proof tracked in [KI-001](../../docs/known-issues/KI-001-ci-source-admission-unqualified.md), outside minimal T023 | DEFERRED by the 2026-10-02 operator priority exception (D60); unqualified, nonblocking for current private maintainer development/publication/PR/merge |

## Requirements
- **FR-001**: MUST satisfy each clause below.
  - **C001.1**: Pin the exact toolchain.
  - **C001.2**: Reject version mismatch or a missing required tool.
- **FR-002**: MUST satisfy each clause below.
  - **C002.1**: Run offline checks without cloud credentials or credential mounts.
  - **C002.2**: Deny outbound network, including provider subprocesses.
  - **C002.3**: Enter isolation through a separately installed, digest-approved immutable launcher outside the candidate tree before any candidate Taskfile, workflow, hook or provider executes; candidate commands/configuration cannot select or replace it.
  - **C002.4**: Fetch pinned tools/providers only in separate credential-free preparation; qualify mirror-only provider installation with lockfile and no network fallback.
- **FR-003**: MUST satisfy each clause below.
  - **C003.1**: Wrap upstream reports without losing diagnostics.
  - **C003.2**: Reject crash or malformed/truncated output.
  - **C003.3**: Reject zero tests or skipped required checks.
  - **C003.4**: Reject cleanup errors rather than reporting pass.
- **FR-004**: MUST satisfy each clause below.
  - **C004.1**: Enforce requirement → relevant ADR → paths → check → evidence traceability.
  - **C004.2**: Reject an unrelated spec-wide blanket task ADR list; accept a justified phase aggregate with per-requirement rationale.
  - **C004.3**: Distinguish fail, blocked, not-run and review-required in DoD.
  - **C004.4**: Exempt docs-only tasks from invented behavioral tests.
- **FR-005**: MUST satisfy each clause below.
  - **C005.1**: Resolve configurable names and labels in a provider-free module.
  - **C005.2**: Preserve explicit import names.
  - **C005.3**: Preserve deterministic truncation and algorithm version stability.
  - **C005.4**: Reject invalid inputs.
  - **C005.5**: Report limits only for evidenced resource kinds.
- **FR-006**: MUST satisfy each clause below.
  - **C006.1**: Keep names.yaml and organisation schemas consistent with module test vectors.
  - **C006.2**: Keep policy projections consistent with independent expected data.
  - **C006.3**: Reject unknown keys.
  - **C006.4**: Reject tenant-controlled authorization keys.
- **FR-007**: MUST satisfy each clause below.
  - **C007.1**: Run applicable L0 format checks.
  - **C007.2**: Run applicable HCL type/validation and lint checks.
  - **C007.3**: Run applicable data-schema checks.
  - **C007.4**: Reject invalid layer dependencies.
  - **C007.5**: Select changed directories and transitive consumers, including aliases, generated files, subdirectories, external sources and generated-instance fixtures required by ADR-0002.
  - **C007.6**: Fail or select the full suite on unknown changed paths or unresolved references; never silently select none.
- **FR-008**: MUST satisfy each clause below.
  - **C008.1**: Use the same offline task contract with pinned setup on GitHub and GitLab.
  - **C008.2**: Execute parent-run fork checks only inside the trusted boundary.
  - **C008.3**: Withhold secrets/cloud network from the fork check.
  - **C008.4**: Prevent privileged cache writes.
  - **C008.5**: State that fork-owned infrastructure is outside enforcement scope.
- **FR-009**: MUST satisfy each clause below.
  - **C009.1**: Provide stable actionable LZ diagnostics.
  - **C009.2**: Bind DoD to actual evidence.
  - **C009.3**: Generate the ADR decision map.
  - **C009.4**: Publish progressive guides only for implemented artifact kinds.
- **FR-010**: MUST provide the bounded foundation's GitHub CI bootstrap.
  - **C010.1**: Run only implemented foundation checks after T001–T009 have evidenced completion.
  - **C010.2**: Use exact Action pins, bounded execution and read-only repository fetch authority; no cloud/deployment credentials, secret-bearing environment, privileged runner or writable shared cache.
  - **C010.3**: Bind independently reviewed workflow/launcher source and image digests to the candidate head; execute candidate commands only inside the verified T003 boundary, with fetch tokens outside it.
  - **C010.4**: Reject missing checks, zero discovery, failures, cancelled/skipped runs, stale source/head or missing evidence instead of accepting a check name alone.
  - **C010.5 — DEFERRED**: Enforce independently approved workflow/evaluator source before candidate publication or runner execution. The frozen-workflow/no-bypass ruleset and disposable source-admission experiment are later [KI-001](../../docs/known-issues/KI-001-ci-source-admission-unqualified.md) obligations under the 2026-10-02 operator priority exception (D60), not immediate prerequisites for current private maintainer development/publication/PR/merge. Owned-branch filters or review after execution alone do not qualify this deferred guarantee.
  - **C010.6**: Retain T010–T012's protected publisher, fork and both-forge qualification as separate uncompleted duties.

Each numbered clause inherits its parent requirement’s V-check and creating tasks.
For every guarded clause, implementation records a distinct expected outcome and
valid/defect control; parent coverage alone cannot satisfy an untested child clause.

## Acceptance and predefined verification
All commands are **planned**, with creating tasks in tasks.md. Task/Go check commands are
in-child operations entered through T003's separately approved `lz-offline`; never invoke
a candidate Taskfile on the host. Boundary tests exercise this actual advertised entry. No product acceptance or live cloud
check has run. Evidence below is initially `not-run`; paths are under `.local/evidence/`.

| Check | Requirements | Criterion: positive and negative outcomes | Verify (planned) | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | tool pins accepted; wrong/missing binary, credential/runtime-socket mount or successful outbound probe fails; malicious Taskfile/workflow/include cannot execute candidate code on the connected host; full naming suite meets 120s with discovery counts | `task verify:toolchain; task test:offline-boundary; task verify:latency` | `001/toolchain-and-boundary.json` |
| V002 | FR-003, SC-002 | real passing stream renders tests; failure, truncated stream, zero tests, skip, cleanup fault all reject | `task test:reports` | `001/reports.json` |
| V003 | FR-004, SC-003 | focused task mapping and justified aggregate accepted; unrelated blanket ADR lists, unmapped requirement/task and fake green evidence rejected; docs exemption accepted | `task test:traceability; task check:specs; task dod -- modules/naming` | `001/traceability.json` |
| V004 | FR-005, SC-004 | two templates and independent collision/truncation/import/upgrade vectors pass; impossible or unknown kind rejects | `task test:naming; task snap:check -- modules/naming` | `001/naming.json` |
| V005 | FR-006 | valid hierarchy labels pass; missing managed-by/managed-in/instance/release, unknown keys or tenant auth-key edits reject in applicable projections | `task test:naming-policy; task generate:check` | `001/naming-policy.json` |
| V006 | FR-007 | valid module and data pass real L0 checks; unformatted/malformed HCL, pinned linter violation and malformed/empty schema discovery fail; leaf/consumer closure exact, alias/generated/subdirectory/external/generated-instance fixtures retain consumers or fail/widen on unresolved classification; reverse edge/cycle/unresolved path rejects, unknown diff selects full suite | `task test:dependencies; task test:static; task lint -- modules/naming; task schema:check` | `001/dependencies.json` |
| V007 | FR-008 | valid offline job and broken behaviour have matching green/red reports on both real forges; parent-run fork has no privilege, including malicious Taskfile/workflow/include controls; fork-owned runner is not certified | `task verify:forge-offline` | `001/forge-offline.json` |
| V008 | FR-009 | seeded defect yields stable id, location, observed/expected and fix; missing evidence is not-run and expert duty is review-required; decision map is fresh | `task test:agentex; task decision-map:check` | `001/agentex.json` |
| V009 | FR-010, SC-005 | reviewed source and exact head run the implemented foundation checks green on GitHub; broken behavior is red; unpinned Action, widened permissions/trigger, credential/socket/cache exposure, host candidate execution, skipped check, zero discovery or stale head/source is rejected; absent CI blocks merge, not review PR creation; C010.5/P4 source-admission controls remain DEFERRED in [KI-001](../../docs/known-issues/KI-001-ci-source-admission-unqualified.md), preventing whole-FR-010/full-feature acceptance | `task test:foundation-ci; task ci:foundation`, then inspect actual PR checks/run metadata and matching task packet; creator T023 for the active subset, KI-001 for deferred controls | `001/foundation-ci.json` |

## Success Criteria
- **SC-001**: All required offline checks run in <=120 seconds for the naming change on an exclusive self-hosted runner with fixed CPU/memory and digest-pinned OS/image after tool preparation; unknown allocation or overlapping jobs blocks the measurement; a timeout fails (V001).
- **SC-002**: Every report fault case and each guarded clause has a behavioural red control and valid unusual input (V002).
- **SC-003**: All non-docs requirements and tasks have predefined checks; no missing evidence is displayed as pass (V003).
- **SC-004**: Two differing organisation templates preserve names across label/profile changes and algorithm upgrades (V004).
- **SC-005**: The foundation PR has an actual successful GitHub run for its independently reviewed exact head/source and a recorded failing behavioral control; local/synthetic reports, skipped or stale runs cannot satisfy this criterion (V009).

## Edge cases
Empty repo/diff; changed shared tool; conflicting catalogue limits; duplicate YAML keys;
hash collisions; Unicode, very long coordinates; empty test filters; killed tool; tampered
evidence; absent container runtime; stale generated output; malicious fork cache writes.

## Implementation surface
Go probe/test helpers live in `tools/internal/probes/`; root `tests/` holds their fixtures.
`mise.toml`, `Taskfile.yml`, `tools/go.mod`, `tools/internal/{checks,report,namingdata}/`,
`tools/cmd/{lz-check,lz-offline}/`, `tools/cmd/json2junit/`, `modules/naming/`, `names.yaml`,
`schemas/{naming,labels,check-report}.schema.json`, `harness/checks.yaml`,
`harness/capabilities.yaml`, `tests/{check,fixtures,security}/`, `policies/{plan,assent}/`,
`pipelines/{github,gitlab}/` (including T023's foundation source manifest), `AGENTS.md`, `harness/guides/`, `docs/reference/decision-map.md`.

## Dependencies and stop conditions
The structure, ratification and workflow-location decisions are confirmed; D61 expands
implementation authorization to eligible work across specs 001–004 without changing task
dependencies or granting merge authority. Tool preparation and actual P1/P2 observations
remain due in those bounded proof tasks; failure blocks dependent work. Minimum local safety
for dependent probes is T001–T009; naming, forge and latency do not gate that subset.
T003 qualifies local entry; T023 retains independently reviewed workflow/launcher source,
exact pins, isolation and actual exact-head CI evidence for merge. C2 pre-execution source
admission is DEFERRED to [KI-001](../../docs/known-issues/KI-001-ci-source-admission-unqualified.md) under D60 and does not
block current private maintainer publication, review PR creation or merge. No workflow/ruleset
enforcement is qualified by this exception. T011 remains the later full forge wrapper.
An unevidenced naming kind is experimental and blocks
cloud use of that kind; fixtures may use explicitly synthetic constraints. New sensors
require independent protected review. All targets above are planned; tasks.md names the
creator. Evidence starts not-run and goes under private `.local/evidence/`; sanitised
summaries may be attached under this spec. Full AgentEx generators and release drills
remain mandatory for the first vertical slice after two real examples, not phase-1 claims.
