# Research and decisions
## Decision: distinguish orchestration from the transaction
ADR-0007 assigns materialisation/generation to Terramate and transaction waves/fencing to
our driver. https://terramate.io/docs/cli/stacks/configuration documents wants/after;
https://terramate.io/docs/cli/orchestration/run-order covers order. Installed 0.17.1
`terramate run --help` exposes dependency flags but does not qualify the 0.17.3 proposal.
Before implementation depends on those semantics, capture actual changed-set/order output
with the proposed pin. Alternative relying on --changed alone rejected.
## Decision: lock across fence and apply
An unlocked current re-read cannot stop a producer updating one instant later. Use one
store coordination interface for producer publications and consumer apply windows. Prove
cross-process local implementation and crash recovery; qualify any future remote backend
separately. Alternative check-only fencing rejected. This does not prove cloud-store locks.
## Decision: no invented assent adapter
The source ADRs require policy-driven auto-merge on both forges even though GitHub support
was unbuilt at design baseline. Pin and probe the actual dependency at implementation time;
if missing, block the real qualification task and name the upstream dependency. Local CEL
fixtures can still run. Do not replace assent with another auto-merge engine.
## Decision: disposable state first
Provider-free/local-state roots prove the graph/transaction seam at zero cloud cost. Live
state authority and cloud deny rules are 003's responsibility. Real forge protocol tests
need sandbox repos and tokens but not cloud authority. Alternative applying OVH roots first
would combine unproven budget/cleanup with unproven transaction assumptions.
Primary project evidence: ADR-0004/0005/0007/0021, read from baseline 116405b.
All platform/forge mechanism premises not observed here remain explicitly UNVERIFIED.
