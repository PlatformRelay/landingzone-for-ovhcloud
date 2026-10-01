# Reconciler and transaction contracts
Planned targets: `task test:instances`, `instances:check`, `test:selection`,
`test:transaction`, `test:plan-binding`, `test:tenant-authority`, `test:reservations`,
`policy`, `verify:forge-transaction`; creator tasks are in tasks.md.
Inputs use strict schemas and canonical rooted paths; traversal, symlinks escaping root,
ambiguous ids, unknown fields and changed evaluator data reject before execution.
The reconciler calls Terramate create, writes metadata and invokes generate; generated
HCL is committed/fresh and runnable as vanilla OpenTofu. It may not emit HCL itself.
State keys and instance ids are immutable. Every edge references a typed output artefact.
Selection = changed instances + changed external generations + transitive consumers;
ordering = topological waves, cycles reject. No Terraform remote-state reads.
Store operations: read status/current; reserve generation; set pending; put immutable
object; publish current with expected generation; acquire/release sorted transaction locks;
reserve/commit/release aggregate exposure; reconcile uncertain operation. Duplicate
same-digest publication is idempotent; delayed older generation cannot regress current.
A consumer re-reads status/current while locks are held through apply. If the store becomes
unavailable, or owner/lock liveness becomes uncertain, no new apply starts. Crash/uncertain
apply freezes downstream work until reconciliation; no unlocked time-of-check-to-use gap.
Approvals bind every PlanBinding field and required checks at candidate/merged revision.
Real forge tokens are scoped to disposable repos, protected, not used by offline tests.
Evidence uses 001 report schema; actual forge metadata may remain private. No real cloud
credentials, auto-purchases or production operations occur in this rehearsal.
