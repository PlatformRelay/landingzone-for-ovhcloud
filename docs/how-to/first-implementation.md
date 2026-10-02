# Start the first implementation

This is a guide to implementing the draft planning set, not deployment approval.
No product acceptance command exists until its creating task lands. Read the
[phase gates](../../specs/README.md), the relevant spec/plan/tasks/contracts and
[constitution](../../.specify/memory/constitution.md) before selecting work.

## Select a bounded entry point

The feature numbers are document identifiers, not an approved delivery schedule.
The minimum code prerequisite for protected platform experiments is
[001 T001–T009](../../specs/001-offline-foundation/tasks.md): exact pins, isolated
execution, real report fixtures, traceability and applicable dependency/static checks.
It does not require the naming API, live both-forge qualification or latency benchmark.
The operator authorized that subset on 2026-10-01 with TDD, predefined evidence and
independent review. ADR-0002 is Accepted, constitution 1.2.0 is ratified and the reusable
workflow remains committed. Create directories with their first real artifact.
Keep the complete feature draft: naming, full forge qualification, latency and full exit
are outside this increment. After T001–T009 pass, conditional T023 supplies minimal
GitHub CI for the implemented foundation. The operator granted merge permission;
independent review, actual required checks, PR-head CI and mergeability remain gates.
Missing CI is not a green gate. Material implementation choices are logged for validation.

The 2026-10-02 operator priority exception (local decision D60) accepts C2 (C010.5/P4)
as **DEFERRED**, nonblocking technical debt [KI-001](../known-issues/KI-001-ci-source-admission-unqualified.md) for current private
maintainer development, publication, review PR creation and merge. Frozen workflows,
a no-bypass ruleset and the disposable source-admission experiment are later obligations,
not immediate T023 prerequisites. Review PR creation does not establish merge readiness:
tests/evidence, exact pins, read-only authority, T003 isolation, exact-head CI and
independent review remain required. Whole-feature hardened guarantees remain unqualified;
the constitution and ADRs are unchanged.

Prepare a bounded IAM/state feasibility slice after this minimum safety subset. That
sequence does not grant cloud action, account, recovery or budget authority. Exact pins,
prepared image/cache and actual isolation still need evidence in the foundation tasks;
unavailability or a refuted mechanism blocks dependent work.

[003 T001](../../specs/003-platform-feasibility/tasks.md) can draft the cost/sandbox
operating ADR independently. Keep unresolved setup/custody/account choices explicit;
drafting does not ratify them. Admission implementation waits for the approved ADR
and 001/T009. A project can already exist while identity, cleanup and recovery authority
remain unqualified. The repository working contract records one-off trial funding and
a temporary maintainer-workstation exception for live spikes before pipelines exist.
That exception does not qualify protected CI, narrower roles, issuer revocation or
recovery. Read setup notes without opening secret files; distinguish their documented
permissions from actual tested observations. Account-scoped experiments remain separately gated.

The local [002 rehearsal](../../specs/002-transaction-rehearsal/tasks.md) can qualify
ownership, publication, plan binding and reservations after 001/T009. The platform
aggregate uses local 002/T008, T012 and T016; an unavailable forge adapter blocks forge
readiness without blocking that local evidence. FR-008/SC-003 remain incomplete until
both actual forges qualify. A negative aggregate decision remains a failing/blocked
qualification, even when its report is complete.

Scalar naming with shared context and handwritten independent projections is selected;
its later implementation still needs concrete-consumer diagnosis and is outside this run. The
[004 proposal](../../specs/004-guided-preconfiguration/tasks.md) now permits a bounded
renderer prototype and written journey; hardened persistence/export waits for real profile
schemas. Its draft needs follow-up alignment and is outside this foundation run.
The [upstream trace](../reference/upstream-reference-map.md#translation-into-the-planning-set)
identifies existing checks and future adaptation specs. Network/firewall topology,
real profile exports and the installation-doc renderer are future consumer work.

## Implement one task at a time

1. Use an isolated feature checkout with a clean tree and an explicit base revision.
   Keep one logical change per commit. Record coordination and raw review output locally.
2. Read the selected task's requirements, relevant ADRs, dependencies, Verify and Evidence.
   Confirm each prerequisite actually passed for its stated scope; a checked box or
   CLOSED-WITH-GAPS outcome is not proof that a missing dependency is available.
3. A test-authoring task retains a valid control and records behavioral red against a
   compiling subject boundary. Missing executable, syntax failure or outage is blocked,
   not the red. Capture external output from the exact pinned binary with its command.
4. Implement the dependent task and run the same controls green. Challenge each guarded
   clause separately. Record revision, input digest, tool identity, environment, discovery
   counts and expected/observed result. Store raw evidence under `.local/evidence/` at
   the task's specified path; any shared evidence index points to those packets.
5. Review the task diff in a fresh context. Verify every finding against its cited code
   before fixing it. Reviews also read the current decision log and open operator items
   where those local records exist; report their scope and commissioning accurately.
6. Close only the part supported by evidence. Docs-only authoring needs content review,
   not invented behavioral tests. Mixed code/configuration/examples require their checks.
   Update the task state and keep a resumable local progress record.

Commit subjects use `:gitmoji: type(scope): summary`. Preserve linear history. Never
add authoring/review attribution to product files or commit/PR text. A change accepting
more or running fewer checks needs a separate justification line in its commit body.
A review of planning documents does not qualify the future commands they describe.

## Stop and retain a useful result

Stop at an unanswered decision, unmet dependency, unverified premise outside the
selected experiment, or failing required check, subject to the scoped C2 deferral above.
Record the precise prerequisite and
continue only independent authorized work. Do not silently change scope, version floor,
account boundary, auto-merge activation, naming projections or the wizard threat model.

Current funding is one-off EUR 200 trial credit, with no automatic monthly refill.
Live execution needs explicit approved action/resource scope, numerical RunConfig,
external inventory/lease health, scoped cleanup, usable recovery package and validated
budget reservation. [003's per-spike table](../../specs/003-platform-feasibility/spec.md#predefined-spike-bounds-and-refutation-stops)
defines maximum elapsed time, exposure and the claim withdrawn on refutation.
Timeout/spend exhaustion stops new writes; owned-resource reconciliation and cleanup
keep their separate authority. Unknown prices block admission. Never infer account-wide
IAM/federation approval from a project's creation or change the ordinary account's IdP.

At the selected task limit, report closed tasks, open owner gates, actual checks,
evidence paths, review findings and commit IDs. Re-run cross-artifact consistency and
dependency checks after any task/spec change. Hand off a reviewable branch; publishing,
merging or live actions require the authority for that specific run.
