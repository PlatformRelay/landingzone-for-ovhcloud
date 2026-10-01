# Feature Specification: Offline foundation, verification and naming
Created: 2026-10-01 · Status: draft · ADRs: 0002, 0003, 0007, 0008, 0011, 0019, 0021

## Why and scope
Give contributors a free, deterministic feedback loop before any cloud resource exists.
Deliver the first pure naming module and the verification harness it exercises.
Exclude provisioning, auto-merge execution, runtime families, cloud scans and release claims.

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
| P1 | OpenTofu 1.13 features and JSON stream match our proposed minimum | Capture version and passing/failing `tofu test -json` using pinned binary | UNVERIFIED; local 1.10.3 is inadequate |
| P2 | Offline isolation blocks subprocess cloud calls, not merely top-level calls | Run an intentional outbound probe inside the same container as provider tests | UNVERIFIED; implementation acceptance V001 |
| P3 | Resource naming/tag limits and metadata applicability are known per kind | Primary-source catalogue rows and sandbox probes where source is insufficient | UNVERIFIED; unknown kinds cannot be advertised as supported |

## Requirements
- **FR-001**: MUST pin the exact toolchain and reject version mismatch or a missing required tool.
- **FR-002**: MUST run offline checks with no cloud credentials, credential mounts or outbound network, including provider subprocesses; a trusted immutable launcher enters isolation before executing any candidate Taskfile, workflow, hook or provider; preparation may fetch pinned tools/providers separately without credentials.
- **FR-003**: MUST wrap upstream reports without losing diagnostics; crash, malformed/truncated output, zero tests, skipped required checks and cleanup errors never yield pass.
- **FR-004**: MUST enforce requirement → ADR → paths → check → evidence traceability; DoD distinguishes fail, blocked, not-run and review-required, and docs-only tasks are exempt.
- **FR-005**: MUST resolve configurable names and labels in a provider-free module; preserve explicit import names, deterministic truncation and algorithm version stability; reject invalid inputs and report limits by evidenced resource kind.
- **FR-006**: MUST keep names.yaml and organisation naming/label schemas consistent across module test vectors and policy projections; reject unknown and tenant-controlled authorisation keys.
- **FR-007**: MUST run the applicable L0 format, HCL type/validation, lint and data-schema checks and check layer dependencies and select changed directories plus transitive consumers; unknown changed paths or unresolved references fail or select the full suite, never silently select none.
- **FR-008**: MUST run the same offline task contract on GitHub and GitLab with pinned setup; parent-run fork checks execute only inside the trusted boundary and have no secrets or cloud network and cannot write privileged caches; fork-owned infrastructure is outside our enforcement scope.
- **FR-009**: MUST provide stable actionable LZ diagnostics, evidence-bound DoD and a generated ADR decision map; publish progressive guides for the implemented artefact kinds.

## Acceptance and predefined verification
All commands are **planned**, with creating tasks in tasks.md. No implementation or live
check has run. Evidence below is initially `not-run`; paths are under `.local/evidence/`.

| Check | Requirements | Criterion: positive and negative outcomes | Verify (planned) | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | tool pins accepted; wrong/missing binary, credential/runtime-socket mount or successful outbound probe fails; malicious Taskfile/workflow/include cannot execute candidate code on the connected host; full naming suite meets 120s with discovery counts | `task verify:toolchain; task test:offline-boundary; task verify:latency` | `001/toolchain-and-boundary.json` |
| V002 | FR-003, SC-002 | real passing stream renders tests; failure, truncated stream, zero tests, skip, cleanup fault all reject | `task test:reports` | `001/reports.json` |
| V003 | FR-004, SC-003 | complete non-docs mapping accepted; unmapped requirement/task and fake green evidence rejected; docs exemption accepted | `task test:traceability; task check:specs; task dod -- modules/naming` | `001/traceability.json` |
| V004 | FR-005, SC-004 | two templates and independent collision/truncation/import/upgrade vectors pass; impossible or unknown kind rejects | `task test:naming; task snap:check -- modules/naming` | `001/naming.json` |
| V005 | FR-006 | valid hierarchy labels pass; missing managed-by/managed-in/instance/release, unknown keys or tenant auth-key edits reject in applicable projections | `task test:naming-policy; task generate:check` | `001/naming-policy.json` |
| V006 | FR-007 | valid module and data pass real L0 checks; unformatted/malformed HCL, pinned linter violation and malformed/empty schema discovery fail; leaf/consumer closure exact, reverse edge/cycle/unresolved path rejects, unknown diff selects full suite | `task test:dependencies; task test:static; task lint -- modules/naming; task schema:check` | `001/dependencies.json` |
| V007 | FR-008 | valid offline job and broken behaviour have matching green/red reports on both real forges; parent-run fork has no privilege, including malicious Taskfile/workflow/include controls; fork-owned runner is not certified | `task verify:forge-offline` | `001/forge-offline.json` |
| V008 | FR-009 | seeded defect yields stable id, location, observed/expected and fix; missing evidence is not-run and expert duty is review-required; decision map is fresh | `task test:agentex; task decision-map:check` | `001/agentex.json` |

## Success Criteria
- **SC-001**: All required offline checks run in <=120 seconds for the naming change on the recorded pinned CI runner after tool preparation; a timeout fails (V001).
- **SC-002**: Every report fault case and each guarded clause has a behavioural red control and valid unusual input (V002).
- **SC-003**: All non-docs requirements and tasks have predefined checks; no missing evidence is displayed as pass (V003).
- **SC-004**: Two differing organisation templates preserve names across label/profile changes and algorithm upgrades (V004).

## Edge cases
Empty repo/diff; changed shared tool; conflicting catalogue limits; duplicate YAML keys;
hash collisions; Unicode, very long coordinates; empty test filters; killed tool; tampered
evidence; absent container runtime; stale generated output; malicious fork cache writes.

## Implementation surface
Go probe/test helpers live in `tools/internal/probes/`; root `tests/` holds their fixtures.
`mise.toml`, `Taskfile.yml`, `tools/go.mod`, `tools/internal/{checks,report,namingdata}/`,
`tools/cmd/lz-check/`, `tools/cmd/json2junit/`, `modules/naming/`, `names.yaml`,
`schemas/{naming,labels,check-report}.schema.json`, `harness/checks.yaml`,
`harness/capabilities.yaml`, `tests/{check,fixtures,security}/`, `policies/{plan,assent}/`,
`pipelines/{github,gitlab}/`, `AGENTS.md`, `harness/guides/`, `docs/reference/decision-map.md`.

## Dependencies and stop conditions
Start with test/tool preparation. An unevidenced naming kind is experimental and blocks
cloud use of that kind; fixtures may use explicitly synthetic constraints. New sensors
require independent protected review. All targets above are planned; tasks.md names the
creator. Evidence starts not-run and goes under private `.local/evidence/`; sanitised
summaries may be attached under this spec. Full AgentEx generators and release drills
remain mandatory for the first vertical slice after two real examples, not phase-1 claims.
