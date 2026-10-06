# ADR-0024: Cost and sandbox operations
- Status: Accepted
- Date: 2026-10-06
- Related: ADR-0008, ADR-0009, ADR-0021, ADR-0022; spec 003 FR-001–FR-003

## Context

The sandbox has **one-off EUR 200 trial funding**, with no monthly reset. This is the
total exposure ceiling, not a fresh allowance for each experiment or a provider-enforced
hard cap. Existing consumption and outstanding liabilities reduce the remaining amount.
Billing alerts and delayed usage reports cannot establish sufficient headroom alone.
Selective live experiments buy evidence that offline checks cannot provide; recurring
recording, idle reference infrastructure and a release-wide suite are not funded by default.

Cancellation, process death and partial API responses can leave billable resources after
the runner disappears. Tags and local state alone cannot prove ownership or completeness.
An admission design that needs a successful failure drill to bootstrap its own cleanup
also cannot safely start that drill. Cost, inventory and cleanup must therefore be prepared
outside the experiment and survive its loss.

This ADR records a proposed operating contract. Authoring and independently reviewing it
can close the documentation task; neither action ratifies it, supplies missing numbers,
qualifies a live mechanism or authorizes execution. Only the operator accepts this ADR.
Dependent implementation requires the approved design, and live work additionally requires
observed prerequisites and explicit bounded experiment authority.

## Options considered

| Option | Assessment |
| --- | --- |
| Budget alerts, tags and runner-local cleanup | Inadequate: delayed billing and runner death leave unreserved or undiscoverable liabilities. |
| External durable reservations and resource intents, scoped independent cleanup | Proposed: survives loss of runner/state and makes unknown exposure an admission refusal. Adds reconciliation and bootstrap work. |
| A sandbox project per run or persistent pool | Outside current authority and funding. Additional projects, a pool or a second region need a separate operator decision. |

## Decision

### Scope and existing dispositions

Use one **pre-existing**, operator-approved sandbox project and approved region. Record
their actual identifiers in private approved configuration; none is supplied here. An
allowlisted project is not account isolation. Independently scoped fixtures inside it do
not prove cross-project or cross-account isolation. No orders, carts, purchases, broad
production policy changes or account-singleton mutation follow from this document.

Existing dispositions remain in force: D8's funding clarification sets the one-off ceiling;
D47 separates resource allocation reservations from financial exposure; D49 permits one
maintainer plus a separately encrypted sealed offline copy for initial development.
That copy must be independently usable without the original account/KMS, with recovery
qualified in a clean environment. It is not a second human custodian, and maintainer-loss
or production two-person custody remains unqualified. This is the operator amendment
allowed by FR-001 C001.3 and recorded in the spec's
[development custody amendment](../../specs/003-platform-feasibility/spec.md#development-custody-amendment-d49).
The plan and T006 use that limited custody shape; actual package availability and recovery
evidence remain prerequisites for their respective checks, not consequences of this amendment.

D53 chooses OVHcloud S3 as the first state implementation and qualification target;
later backend adapters retain explicit selection and need their own evidence. Deployment
project/region and backend location/identity are distinct. This neither authorizes a new
paid project nor selects the external inventory's storage service. A backend accessible
only through the fixture being destroyed cannot be the sole cleanup record.

### Budget units and accounting

Account in EUR using exact decimal or integer minor-unit arithmetic; round estimated
liabilities upward to cents. Record original provider billing units and convert explicitly:
quantity, per-second/hour/month or operation rate, minimum charging period, rounding,
region, product and applicable taxes/fees. No silent currency conversion or assumed free
SKU is allowed. Any conversion needs an approved source, timestamp and conservative rate.
Credit eligibility, expiry and tax treatment require observation and owner disposition;
the nominal EUR 200 does not prove EUR 200 is still usable.

Maintain two separate durable ledgers, linked by run and operation identifiers:

- **Allocation:** resource quantities and ownership/overlap constraints, such as counts,
  capacity and exclusive writers. Available quota is not available money.
- **Exposure:** settled charges, incurred but unbilled estimates, reserved future charges,
  cleanup liabilities and unresolved liabilities. Available money is not resource authority.

Before admission, the cumulative settled charges plus conservatively estimated unsettled
liabilities plus unconsumed reservations (including the candidate run) must not exceed
EUR 200 or any lower approved ceiling. Separately establish that current usable credit
can cover remaining obligations; missing or expired credit data rejects admission.
Reconcile billing against identifiable operations/time intervals so an estimate moves to
settled cost without double counting or dropping liability. Ambiguous overlap retains a
conservative overestimate and blocks new admission until reconciled.

Every estimate covers preparation, create/update/delete operations, minimum billed periods,
idle compute, storage/snapshots/replicas, network/request fees where applicable, recording,
probes, retries and teardown. Reserve the possible recurring tail through the approved
cleanup/reconciliation horizon, including delayed deletion. Failure to bound that tail
blocks admission. A process timeout does not stop provider billing. A possible charge
outside the model is unknown exposure, never zero. Report overshoot if it occurs; the
ledger is an admission control, not a guarantee against provider or cleanup failure.

Only observed billing reconciliation can reduce incurred liabilities. Expired leases,
successful test exit, missing state or deletion requests do not release financial exposure.
Confirmed deletion stops future reservation accrual only when the applicable charging
semantics are known; delayed bills remain covered. Publish sanitized actual/estimated
cost, outstanding exposure and cleanup status per experiment and in the ADR-0008 weekly
cost summary. Report unavailable actuals as unknown, with reservations retained.

### Prices, freshness and approved configuration

Each price observation binds its source, capture time, effective validity, currency,
tax/credit treatment, exact product/region and billing unit to the run configuration.
Freshness is checked against approved numeric maximum ages for prices, billing, credit,
inventory and reaper heartbeat. An absent age bound, missing timestamp, unverifiable
clock, future-dated observation or expired evidence rejects admission. A catalogue entry
for a different product/region or an unverified zero price is insufficient.

Recheck before admission and exposure-increasing actions or lease renewal. Stale prices,
unknown recurring charges, stale billing, orphan inventory, exhausted aggregate exposure,
unavailable ledgers and unhealthy cleanup stop new admission and new billable writes.
They leave scoped cleanup executable. If an updated conservative estimate exceeds the
reservation, stop and reconcile; do not silently enlarge the cap or borrow another run's
reservation. Fresh data alone cannot grant a new approval.

### Durable reservations and resource-intent inventory

Store leases, both ledgers and resource intents durably outside the runner, its local state
and disposable fixtures, with independently usable read/recovery access. Backend choice,
retention, access controls and recovery procedure require approval and qualification.
An unavailable or inconsistent store fails closed for new work. Protect audit history and
retain unresolved intents across process death, lease expiry and reaper outage.

Admission atomically checks aggregate exposure and allocation/concurrency constraints and
reserves both, using a stable idempotency key. A duplicate request returns the same result;
it cannot reserve twice or execute twice. A partial multi-ledger update remains pending
and permits no provider mutation until reconciled. Bind the lease to the approved project,
region, principal scope, experiment, source/input/tool digests, approval, price snapshot,
deadline, spend cap and cleanup authority. Use fenced ownership so an expired worker cannot
resume writes after another worker acquires the lease. Store failure never grants a lease.

Persist each create/update intent **before** the API call: run/operation identity, target
project/region, resource kind, expected ownership, approved action and bounded exposure.
Persist returned IDs independently of local state and tags. A create response without an
ID or a death before ID persistence leaves an unresolved intent, not a safe retry. Reconcile
using the durable request identity and bounded provider discovery within the approved scope;
provider idempotency/discovery behaviour remains UNVERIFIED until probed. Ambiguous identity
blocks retry and new admission; it never authorizes deleting every matching tag.

Track requested, reserved, active, reconciling and cleaned states with unresolved outcomes
retained. A run is financially settled only after delayed-charge reconciliation. Recovery
must account for each recorded intent and ID, including partial writes and missing state.
Inventory completeness and ownership must be established before clearing an orphan block.

### Deadlines, concurrency and experiment stops

RunConfig must contain approved numeric maximum elapsed seconds, monetary ceilings,
concurrent lease/resource limits, per-call timeouts, bounded retries/pagination, cleanup
escalation horizons and freshness windows. Overlapping writers to the same fixture/backend
are forbidden even when global concurrency would permit them. No default concurrency or
platform timing is inferred here. Lease extension requires fresh approval and an atomic
headroom check; retries and restart do not reset the experiment clock or spend allowance.

The [canonical spike table](../../specs/003-platform-feasibility/spec.md#predefined-spike-bounds-and-refutation-stops)
owns the draft maxima and named downstream stops. Its existing ceilings are reproduced
for review, **not approved run values**; RunConfig may reduce them, while increases require
a reviewed operator amendment. Convert hours/minutes to seconds explicitly in implementation.

| Spike | Draft elapsed ceiling | Draft exposure ceiling | Refutation stop |
| --- | --- | --- | --- |
| 1 Independent decryption / clean bootstrap | 8 h | EUR 30 | Withdraw affected ADR-0009 bootstrap/key-loss and ADR-0022 recovery readiness; no deployment using the refuted route. |
| 2 State ownership / root ordering | 4 h | EUR 20 | Gate ADR-0004 ownership/artifact isolation and the affected ADR-0009 state route. Local roots cannot rescue cloud isolation. |
| 3 Floor / native recovery | 4 h | EUR 10 | Withdraw affected ADR-0006 floor/tag-envelope claims and gate ADR-0018/0021 authority; failed native recovery forbids floor binding. |
| 4 Merge authorization / races (owned by 002) | 8 h total; 10 min per forge run | EUR 0 cloud spend | Gate ADR-0005/0007 unattended merge/apply for stale, forged or racy paths; an absent adapter is blocked. Runner costs/capacity are separate prerequisites. |
| 5 Credential issuance / revocation | 4 h | EUR 10 | Withdraw affected ADR-0009/0018 JIT/bridge claims; fallback needs its own passing suite. |
| 6 Sandbox failure safety | 4 h | EUR 20 | Stop all new ADR-0008 live admission on ownership/reaper failure or orphan; retain reconciliation/cleanup. |
| 7 Locking / interruption / promotion | 8 h | EUR 20 | Withdraw ADR-0009/0022 readiness on concurrent writes, unfenced old writer or excessive lag; no replica activation. |

The draft caps total EUR 110; the remainder is **at most** EUR 90 before other existing
liabilities, not an additional grant. Elapsed limits cover admission, setup and probe
retries; spend caps cover setup, probe and teardown. Cleanup headroom is reserved before
starting. At deadline/cap exhaustion stop new writes, record `blocked`, and continue scoped
cleanup/reconciliation. Actual refutation records `fail` with the named stop, even if a
deadline also expires. Missing observations, timeout or cap exhaustion never become pass.
Completing the decision table does not close an incomplete experiment. Gate its dependent
follow-ups; replacement architecture needs a new operator decision.

### Cleanup authority and health bootstrap

Cleanup is a separate restricted execution path, usable when run admission fails. Its
authority binds an independent sandbox project allowlist and known owned resource IDs;
tags are discovery hints only. For missing IDs, reconcile durable intent to independently
verified ownership before deletion. Refuse foreign, wrong-project or ambiguous deletion.
Keep pending liabilities and escalate unresolved ownership without broadening authority.
Cleanup may delete/revoke the approved fixture's resources/credentials, never create new
experimental resources or gain general account rights to repair its own failure.

Bootstrap is a separately approved sequence before general live admission:

1. Establish the external inventory, ledgers, independent cleanup runner/identity, private
   recovery access, alert destination and approved numeric health thresholds. Demonstrate
   offline ownership/refusal and crash-recovery controls before live fixtures.
2. Perform the approved read-only preflight on the actual project, scoped identities,
   recovery package, prices/credit and runner health. Unknown setup stays blocked. An empty
   inventory or heartbeat alone does not prove deletion rights or safe cleanup.
3. Separately authorize a minimal cleanup canary, with an explicit fixture or bounded
   creation lease, exposure reservation, deadline, exact cleanup scope and independent
   recovery path if the canary fails. This bounded bootstrap exception tests the cleanup
   premise; it cannot admit ordinary experiments. If no safe recoverable fixture can be
   authorized, bootstrap remains blocked rather than creating one implicitly.
4. Observe canary deletion and inventory reconciliation before process-kill/partial-write/
   reaper-outage drills. Those drills require their own bounded authorization and healthy
   external cleanup; only their passing evidence can qualify ordinary live admission.

Health includes recent authenticated heartbeat, durable inventory accessibility, ownership
reconciliation and current scoped cleanup capability evidence. Loss of health or expired
cleanup authority stops new admission; alert and use the pre-approved independent recovery
path to restore scoped cleanup. Revoke/fence expired runners while retaining cleanup access.
A failed cleanup is never reported as a successful run, and records/reservations survive.

### Open operator disposition before implementation or live admission

| Required disposition | Current state |
| --- | --- |
| Accept this operating design and align dependent configuration/spec wording | Proposed; docs task closure supplies no ratification. |
| Exact existing project/region, protected runner and scoped read/apply/cleanup identities | Not provided or observed here; the documented temporary workstation exception is not a blanket live grant. |
| Usable credit, expiry/tax/fee treatment, existing charges and approved price sources | Unknown; no current headroom assertion. |
| Numeric price/billing/credit/inventory/heartbeat maximum ages | Open; absent values reject admission. |
| Numeric concurrency, allocation limits, retry/call bounds, lease duration, cleanup reserve/horizon and escalation | Open; no silent defaults. |
| Approved run/spike deadlines and caps within the draft ceilings; lag and per-issuer residual windows | Open; draft maxima and platform measurements are not interchangeable. |
| Durable store/location, recovery access, retention, cleanup bootstrap fixture and authority, alert recipient | Open; no service procurement or canary execution authorized. |
| D49 development recovery package and clean-environment observation | Approved limited custody shape; actual recovery UNVERIFIED. Production human-loss custody remains unqualified. |

Resolve these in a versioned private approval/RunConfig with evidence references; sanitize
published dispositions. Secrets and raw state/plan/API logs never enter tracked records.
Writing this draft does not require choosing the missing owner values. T002/T003 retain
their approved-design dependency; actual setup/preflight/canary gates remain prerequisites
for live work. Independent offline tasks may proceed under their own satisfied dependencies.

## Consequences

Crash recovery and delayed billing become explicit operating obligations. Conservative
unknown handling can block experiments even when the account appears to have credit;
the next safe action is reconciliation, fresh price evidence or scoped cleanup. External
storage and cleanup introduce their own cost and failure modes, which must be included in
the same budget. A runtime/spend cap bounds admitted intent, not provider liability after
an uncontained failure. Small experiments and independent recovery limit that risk.

## Counterpoints (kept even if overruled)

- A single ledger is simpler, but quotas and money use different units and release rules.
  Keep separate allocation/exposure records with atomic admission and shared provenance.
- A sealed offline copy is practical for solo development but cannot recover a missing
  human. Preserve the narrower approved claim rather than imply two-person custody.
- An external store and canary add setup work; tags and `finally` alone cannot recover
  unknown partial creates or a killed runner. Qualification must test those failures.

## Verification (how we will know / spikes)

T001 is prose-only: review scope, units/accounting, open decisions, price rejection,
reservation/intent durability, cleanup bootstrap and all stop conditions. Documentation
review is distinct from the ADR acceptance protocol and from runtime evidence.
T002–T005 own future offline valid/rejection, concurrency, duplicate reservation,
partial-create, process-death and reaper controls; T006 owns actual read-only preflight;
T007–T008 own separately approved cleanup-canary and failure-drill observations. All are
planned, not executed by this ADR. Their passing controls must demonstrate that blocked
admission leaves cleanup usable and cannot hide unresolved inventory or costs.

Current private-development CI source-admission hardening is deferred under D60 (KI-001),
not a prerequisite introduced by this draft and not qualified protection. Candidate
configuration must still not execute before local isolation. Neither repository workflow
authority nor this ADR grants cloud authority. No cloud or credential operation was performed to draft this ADR.

## Review log

- 2026-10-02: Proposed draft prepared for independent documentation review. Operator
  acceptance, numeric/setup dispositions and live evidence remain outstanding.
