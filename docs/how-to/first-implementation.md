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
Start that subset only after the operator ratifies the structure or explicitly accepts
its listed paths, and resolves workflow-scaffolding and constitution activation.

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

Naming implementation waits for the joint call-interface decision. The
[004 proposal](../../specs/004-guided-preconfiguration/tasks.md) waits for its scope
and schedule decision; do not build a synthetic wizard merely because pins exist.
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
selected experiment, or failing required check. Record the precise prerequisite and
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
