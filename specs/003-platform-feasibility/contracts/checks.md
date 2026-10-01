# Protected spike contracts
All targets are planned, not authorized or run by this planning request.
`task test:sandbox-admission`, `test:sandbox-reaper`: offline suite, no cloud authority.
`task live:preflight`: protected read-only inventory plus verified external admission data.
`task spike:sandbox-failure`, `spike:recovery`, `spike:state-isolation`,
`spike:locking-promotion`, `spike:deny-floor`, `spike:credentials`, `spike:qualification`:
require explicit approved RunConfig, allowed candidate and healthy lease/cleanup boundary.
Live preflight may not create orders/carts. No automatically retried purchase or destroy.
Admission refuses unknown cost, stale billing/inventory, unhealthy reaper, excess reservations,
missing timeout/cleanup authority/holders or non-allowlisted resource scope. Cleanup keeps
its separate authority and does not need a new admitted lease to remove known owned ids.
Replica promotion fails before activation if any original writer path is unfenced. An
expired token or removed secret reference is not proof of old-writer revocation.
Deny probes specify exact canary action/id and approved deletion authority; without it
perform only non-destructive probes and leave destructive coverage blocked. V006 also
requires floor-binding/policy self-removal, missing/present tags, cross-tenant retagging and
applicable child-operation tests; unavailable subcontrols block spike-3/envelope authority.
V003 requires actual pinned-tool state and saved-plan format/key controls, plaintext-input
refusal and separate enforcement mutants, plus private canary scans and valid-key consume.
Federation
requires a disposable account-level scope; no singleton replacement in ordinary account.
Each issuer-specific case uses already-issued credentials; residual windows are recorded
numerically from approved config and measured. A fallback must pass the same probes.
Qualification records ranked spike outcomes 1–7 (2/4 include 002), per-route exclusions,
resource cleanup, actual/exposure costs and whether each dependent implementation may start.

The per-spike ceilings and refutation stops are in spec.md. Approval must bind these
values before a live target starts; deadline/spend stops admission but keeps cleanup
executable. A complete decision packet is not a qualification pass. Aggregate inputs
are local 002/T008/T012/T016 evidence and explicit per-forge status, not adapter availability.

Canonical top-seven names, task/check ownership, timeboxes, caps and refutation ADRs
are in [../spec.md](../spec.md#predefined-spike-bounds-and-refutation-stops); no second ranking
is implied. Spike 2 has local 002 and cloud 003 portions; spike 4 is owned by 002.
