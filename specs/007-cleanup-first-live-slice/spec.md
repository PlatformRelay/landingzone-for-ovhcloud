# Feature Specification: Cleanup-first live slice
Feature: `007-cleanup-first-live-slice` · Created: 2026-10-16 · Revised: 2026-10-16 (spec-set review rounds 1 and 2) · Status: draft
ADRs: 0004, 0005, 0006, 0008, 0009, 0019, 0021, 0024
Input: decision D119 (spec 007 scope, 2026-10-16) with its brainstorm and three judges' notes; D117
and D118 (live runs by the coordinating session until the credit expires); the unspecified-work
inventory of 2026-10-15 (A17, A24, B4, B5); spec 005's out-of-scope table; spec 003's probe evidence
T027 and T028; constitution 1.3.0.

## Why and scope
The free-trial credit expires on 2026-10-24 at 12:42. Live runs are the one thing it buys, and the
last week of it should prove what only a live run can, without leaving anything behind that bills
the card afterwards. Spec 003's probe runs showed that the cleanup is what breaks first: the
leftover check failed on OVHcloud listing errors in 2 of 3 runs (`specs/003-platform-feasibility/
evidence/T027.md:24-29`, `T028.md:24-31`), each needing a manual `--cleanup`, and the cleanup then
overwrote the record that held the errors' text. So this spec makes the cleanup reliable first, then
refuses the one manifest/account mismatch that a second tenant would hit (A17), then qualifies the
landing zone's own budget-alert path in a disposable run (P10) whose cleanup can remove an alert
even when its creation was never recorded.

Visible outcome: a probe or chain run whose listing answers one 500 still ends with a clean
leftover check and a record of what failed and how often; an account file that maps two project
refs onto one project is refused before any child process starts; the budget-alert input of
`modules/cloud-project` is shown to create, read back and remove an alert on the sandbox project,
and an alert whose creation outlived a crash is removed by a fresh `--cleanup`; a two-tenant
manifest is proven offline and refused on one project; and before the voucher expires, every piece
of approved retained infrastructure is listed with how to remove it and what it costs per month.

This spec is the second vertical slice in D87's sense, so it **fires the AgentEx trigger**
(constitution VI; D119 Q3). AgentEx (001/T017–T019) is scheduled as its own offline spec from
2026-10-24, outside this live window; nothing here implements it.

**In** (inventory ids in brackets):
- bounded retry of transient errors on leftover listings, inside the run's shared retry budget, and
  a run record that keeps every failed attempt across `--cleanup` (D119 item 1);
- resolved-project aliasing refused before any child process [A17], with its first task
  establishing whether the rule refuses two tenants on one project (D119 item 2);
- a disposable budget-alert apply, read-back and destroy through `modules/cloud-project`, outside
  the retained `project` stack, with a sweep that removes an unrecorded alert and nothing retained
  [B4 alert part, P10] (D119 item 3);
- the two-tenant manifest offline [B5 offline]: A17 refuses two tenant refs on one project, so B5
  does not run live (D119 item 4);
- the live-window procedure of D119 written into the tasks: voucher ceiling, card spend, reviewed
  SHA, the coordinator under D118, the retained-infrastructure list, the cut-off dates;
- tracing spec 007 by default and in CI's source check.

**Out**: narrowed run locks (A24, deferred with its split design), B5 live, a second Public Cloud
project (KD-1), KD-3, multi-environment tenants (B1), a platform-owned bucket (B2), B3, the quota
guard live (P11) and AgentEx. See *Out of scope / postponed (with trigger)*.

## Clarifications

### Session 2026-10-16
Answered from D119, the judges' notes, the code at `df8bb3e` and, for spec 006's retry budget, `main`
at `15bc579` (spec 006 US2, PR #123, merged), and the evidence files. Open items are listed last, each with the
default this spec applies; the coordinator confirmed all four defaults after review round 1.
- Q: Is P10 already observed? → A: For the raw resource, yes: the `alerting` probe created and
  destroyed one `ovh_cloud_project_alerting` on the trial project (spec 005 T010 run 4,
  `20261010T172559Z-eb9b`, `specs/005-first-landing-zone-slice/evidence/T010.md:13`); spec 005's
  premise table still says UNVERIFIED. What has never run live is the landing zone's own path:
  `modules/cloud-project`'s `budget_alert` input (`modules/cloud-project/main.tf:41-50`), its
  validation and the reference-mode read beside it. FR-005 qualifies that path and only that path:
  there is no inline-resource fallback.
- Q: Where does the P10 run live? → A: In a probe root (`tests/live/probes/budget-alert/`), run by
  the probe core with its own encrypted local state, never in the retained `project` stack: a
  retained alert followed by a later state change would orphan or duplicate it (D119 condition,
  from the strong leg's finding on `stages/project/main.tf:20`).
- Q: What does A17 refuse? → A: Two refs of the manifest whose `LZ_PROJECT_ID_<REF>` in the bound
  `account.env` name the same project id, except the KD-1 sharing of the state ref with tenant
  refs under `spec.sandbox.shared_state_project: true` (T098 decision request, option A,
  `specs/005-first-landing-zone-slice/evidence/T098.md`; inventory Q8 recommendation "refuse at the
  adapter"). The decoder already refuses two environments naming one ref
  (`tools/internal/stacks/manifest.go:483-486`), so a second tenant in the one sandbox project needs
  a second ref with the same id, which this rule refuses. T001 confirms this from the code and
  records it; B5 stays offline either way in this spec.
- Q: Do the listing retries share spec 006's retry budget? → A: Yes. Spec 006 FR-008's
  `retryBudget` (`newRetryBudget(end, reserve)`, `admit(now, wait)`, `keep(d)`;
  `tools/internal/live/deadline.go:53-76` on `main`) is built once per leftover reconciliation; the chain passes the
  same budget its retained-state reads used, and the probe core builds one from its destroy
  context's deadline. The clock the waits read is defined in this spec's data model (`waitClock`,
  matching the landed code), since spec 006's data model does not fix one.
- Q: Are narrowed run locks (A24) in scope? → A: No (coordinator, review round 1). Locks are per
  `account` and per `tenant-<t>` (`tools/internal/stacks/lock.go:85-117`); a chain reads every
  retained instance (`tools/internal/live/chain.go:192-198`), so two tenants' chains each need the
  other's tenant lock and changing only lock modes cannot let them run together. The split design
  is recorded in *Out of scope* with its trigger.
- Q: Does a second tenant fire KD-3's trigger? → A: No. The trigger is "the artefact transaction
  starts, or a tenant S3 credential is held by someone other than the maintainer"
  (`specs/005-first-landing-zone-slice/spec.md:348`); a second maintainer-held tenant is neither.
- Q: Are FR-010's guards enforced in code? → A: No. The credit floor, the dates and the 23:00
  recovery limit are coordinator procedure, checked by hand before each run (AGENTS.md "Bounded
  extension": "The spend stop is a manual cost check"). No sensor enforces them and no check claims
  to; the reviewed SHA, the live clone and the redaction are enforced by `lz-live`'s existing host
  guard.
- **Open, owner's, with default (confirmed by the coordinator)** — weekday of the cleanup day. D119
  says "Thursday 23", but 2026-10-23 is a Friday (2026-10-22 is the Thursday). Default: the date
  governs, as in D117's stop (2026-10-23 23:00). The budget-alert run is dropped if it has not
  started by 2026-10-21 23:00; the final chain finishes with its recovery on 2026-10-22; 2026-10-23
  is for verification and cleanup only.
- **Open, owner's, with default (confirmed)** — does the sandbox flag also admit two tenant refs on
  one id? Default: no (D119: "Don't weaken A17 to stage a two-tenant demo").
- **Open, owner's, with default (confirmed)** — the budget alert's recipient (inventory B4
  **[OWNER]**). Default: the account's own contact address, read at plan time from `data.ovh_me` as
  the existing `alerting` root does; no address is committed.
- **Open, owner's, with default (confirmed)** — the listing retry bound. Default: three attempts,
  waiting 5 s and 15 s, each wait admitted by the shared retry budget and the run's deadline.

## Decisions and proposals
- **Approved direction**: D119 (scope and order: retry, A17, P10, then A24/B5 only if A17 permits);
  D118 (named live targets by the coordinating session until 2026-10-24 23:59 or below 20 € of
  credit; reviewed SHA on `main`; independent review before code under `tests/live/` or
  `tools/internal/live/` runs live); D117 (live work stops 2026-10-23 23:00); D107 (tags do not scope
  bucket reads, so isolation rests on one project per tenant); the coordinator's review-round-1
  decisions (A24 deferred, B5 offline, the four defaults confirmed).
- **Proposals, credible alternatives** (detail in `research.md`):
  - listing retries inside the leftover check's lister, classifying by status code, rejecting a
    retry of the whole check (R1);
  - the alias check in the lane's and the bootstrap's setup, before any child process, rejecting a
    decoder check (it never sees ids) and the bootstrap's later binding calls (R2);
  - a pre-apply baseline of nameless kinds, a budget-alert sweep that deletes only ids absent from
    it, and a refusal to start a probe while an earlier probe run is unreconciled, rejecting a tag
    or name (the alert takes neither) and rejecting matching earlier runs' records (R3).

## User Scenarios & Testing

### User Story 1 — A transient listing error does not fail the cleanup (Priority: P1)
The coordinator runs a probe. Its leftover check lists private networks and the API answers 500
once. The check waits, prints one line naming the listing, the status and the attempt, lists again,
and ends `pass`. If the API answers 500 on every attempt, the check ends `fail` with the path, the
last status and the attempt count, and the run record keeps each attempt. When the coordinator
then runs `--cleanup <run-id>`, the first run's whole record stays on disk beside the new one, so
the evidence can quote what failed rather than "believed".
**Independent test**: V001. Interruption: a deadline or a budget that leaves less than the next
wait starts no wait; a cancellation during a wait ends it at once with no further attempt.

### User Story 2 — Two refs on one project are refused before anything runs (Priority: P1)
The maintainer binds an account file in which `LZ_PROJECT_ID_DEMO_DEV` and `LZ_PROJECT_ID_SHOP_DEV`
hold the same id. `task live:plan`, `live:apply`, `live:destroy`, `live:chain` and the account
bootstrap stop before any child process with `project-alias`, naming both refs and not the id, and
the next safe action (give each ref its own project, or drop one row). The sandbox account, where
`STATE` and `DEMO_DEV` share one id under the KD-1 flag, still binds.
**Independent test**: V002.

### User Story 3 — The landing zone's budget alert is shown live and leaves nothing (Priority: P2)
The coordinator runs the `budget-alert` probe on a reviewed SHA. The probe refuses to start while an
earlier probe run is unreconciled; otherwise it records the project's alert ids, applies
`modules/cloud-project` in reference mode with an alert of threshold 1 and delay 3600, reads the
alert back (id, threshold, delay, project) before the destroy, destroys it, and its sweep lists the
alerts again and deletes any id absent from the baseline. If the process dies after the API created
the alert but before any state or inventory was written, the run's recovery files stay, and a fresh
`--cleanup <run-id>` reloads the baseline, deletes only the new alert and checks it is gone; an
alert that existed before the run is never touched.
**Independent test**: V003, V005.

### User Story 4 — A second tenant, proven offline (Priority: P3)
The two-tenant sandbox fixture decodes, reconciles, generates and orders offline. With distinct
project ids it binds; with both tenant refs on the sandbox's one id it is refused by US2's rule,
which is why no second tenant runs live in this spec.
**Independent test**: V004.

### User Story 5 — The live window closes clean and on time (Priority: P1)
Before the final chain, the coordinator writes the list of approved retained infrastructure that
stays after 2026-10-24 12:42, with its removal path and monthly cost. Nothing a disposable probe
created may appear on it: such a resource blocks completion and every later live run until its
deletion is verified. The final chain runs on a reviewed SHA with the retry in place and ends with
a clean leftover check. On 2026-10-23 only verification and cleanup runs start; each run's cost is
recorded.
**Independent test**: V005.

### Edge cases
A 500 on the second page of a paginated listing (resume from the same cursor, no duplicate items);
a 429; a reset connection while sending or while reading the body; a 5xx or a reset from the token
endpoint (retried within the same bound) and a 4xx from it (never retried); a 403 or a 404 where
absence is not admitted (never retried); a 200 with a body that is not JSON (never retried); a
deadline that ends during a wait; a cancellation during a wait; a budget already spent by
retained-state retries; the bearer token inside a retried error (redacted); a retry that succeeds
and lists a real leftover (still `fail`); a second `--cleanup` of the same run (a second archived
record). Two refs with one id where one is the state ref without the flag; three refs with one id;
an `LZ_PROJECT_ID_X` the manifest never uses (not judged). A baseline listing that fails after its
retries (no create); an earlier probe run left under `state/probes/` (the next probe is refused); a
cleanup without the run's baseline (refused, files kept); an alert created by someone else during
the run, or by the owner before a late cleanup (deleted only if it matches the root's plan;
otherwise reported and the sweep fails); a baseline of another run or project, without the alert
kind, incomplete or with errors (invalid: refused); a kill between start-up writes (pre-create,
retired by `--cleanup`); both baseline copies lost after a create (owner recovery); an alert whose
read-back differs from the plan (the observation fails, the destroy and sweep still run).

## Premises

| ID | Premise | Source | Probe | Status |
| --- | --- | --- | --- | --- |
| P1 | The listing errors of runs `20261016T150207Z-be89` and `20261016T150441Z-eb3a` were transient 5xx answers that a retry seconds later would have passed | the coordinator's reading; the error text was overwritten by `--cleanup` (`T027.md:27-29`, `T028.md:28-31`) | the attempt records of T019 and T022 (FR-002) | UNVERIFIED (believed); if the records show 4xx or persistent 5xx, FR-001's classification stands and the finding goes to research R1 |
| P2 | The probe core can initialise and run a root that calls `modules/cloud-project` by a relative source | `lz-live probe` runs `tofu -chdir=<root>` in the reviewed checkout (`tools/internal/live/runner.go:171`); the unit runner initialises with `-lockfile=readonly` (`tools/internal/checks/unit.go:49`) | T010 (offline init from the entry's mirror with the root's pinned lock file), then T019 | believed offline until T010; a live refusal makes T019 `fail` and FR-005 unqualified: there is no fallback |
| P3 | `GET /cloud/project/{serviceName}/alerting` lists every alert id of the project, `GET …/alerting/{id}` reads one, and `DELETE …/alerting/{id}` deletes one | API schema `kb/api/v1/cloud.json:5843-5870` (list, `publicCloudProject:apiovh:alerting/get`) and `:5916-5970` (get; "Delete an alert", `publicCloudProject:apiovh:alerting/delete`) | T019 (baseline, read-back, sweep listing) | doc; UNVERIFIED live for the baseline and sweep use |
| P4 | A budget alert notifies; it does not cap spend | API description "billing alert configurations" (`cloud.json:5867`); provider docs `cloud_project_alerting.md` (delay, email, threshold only) | none: no task relies on a cap | doc |

## Requirements

### Functional requirements
- **FR-001 Listing retry** — the leftover check retries one request (an array page, a
  single-object read, or the token request it needs first) that failed with a status of 429 or
  500–599 or with a transport failure (reset, timeout or refused connection, while sending or while
  reading the answer's body), at most twice after the first attempt, waiting 5 s and then 15 s. A
  4xx answer from the token endpoint is never retried and stays cached as today. A wait starts only
  when the context's deadline and the run's shared retry budget (spec 006 FR-008: in a chain the
  budget its retained-state reads used, in a probe one built from the destroy context's deadline)
  both admit it; a cancellation during a wait ends it with the context's cause and no further
  attempt. Any other failure (another 4xx, a body that does not decode, a page bound, a repeating
  cursor) is never retried. A retried page resumes from the cursor it failed on. Each retry prints
  one line `LZ-LIVE retry listing <path> in <wait>: <status or class> (attempt <n> of 3)`,
  redacted. A listing that still fails leaves the check `fail` (or `blocked` where spec 006 makes a
  refused wait `blocked`), never `pass`; a retry that succeeds still reconciles what it listed, so a
  real leftover is still `fail`. [D119 item 1]
- **FR-002 Run record kept** — `leftovers.json` carries every failed attempt (path, attempt number,
  status or failure class, whether a retry followed, why not), redacted. A probe `--cleanup` (`lz-live
  probe --cleanup <run-id>`) first moves the run directory's whole record, except `inventory.jsonl`,
  `baseline.json` and `observations.json`, into `attempts/<n>/` (`n` one more than the highest existing number, so a slot is never reused), copies
  `observations.json` there too, then writes its own; `observations.json` stays in place unchanged,
  so `Recorded` and the undo step of every later cleanup still read what the run observed
  (`tools/internal/live/runner.go:786-794`). A run directory without a record is cleaned up as
  today. [D119 item 1; T027/T028 learnings]
- **FR-003 Project aliasing refused** — before any child process, the lane's setup (plan, apply,
  destroy, chain) and the bootstrap's setup, before its `state` phase, refuse a bound account in
  which two project refs the manifest uses resolve to one project id: condition `project-alias`,
  naming both refs and never the id, with the next action. The only alias admitted is the state ref
  sharing a tenant environment's id when `spec.sandbox.shared_state_project` is true (KD-1); two
  tenant-environment refs never share an id, with or without the flag. Refs the manifest does not
  use are not judged. [A17]
- **FR-004 A17 consequence first** — T001 records in `research.md` (R2) whether FR-003 refuses two
  tenants on one project, with the code lines and decisions that decide it. On `refuses` B5 stays
  offline (FR-009); on `permits` the owner is asked for a new decision before any live B5 work, which
  this spec does not hold. [D119 item 4]
- **FR-005 Disposable budget alert through the module** — a probe root
  `tests/live/probes/budget-alert/`, with its pinned provider lock file, applies `modules/cloud-project`
  in `reference` mode on the run's project with `budget_alert` enabled (threshold 1 in the account
  currency, delay 3600, the account's own contact address from `data.ovh_me`), and is destroyed by
  the probe core; its state is the probe core's encrypted local state, so nothing enters the retained
  `project` stack and nothing is retained. The plan creates no `ovh_cloud_project`. The run records
  the alert id, the read-back of FR-008, that destroy and sweep removed it, and the run's cost. A run
  of anything but the module path does not qualify FR-005. [P10, B4 alert part]
- **FR-006 Baseline of nameless kinds; start-up phases; no hidden orphans** — a probe start runs
  durable phases in order, each written and synced before the next: (1) `probe.env` and (2) the
  passphrase under `accounts/<account>/state/probes/<run-id>/`; (3) the alert listing of the
  recorded project, written as `baseline.json` in the run's record directory `.local/live/<run-id>/`
  (never under `state/probes/`, whose directory the success path removes) and as a recovery copy
  `accounts/<account>/state/baselines/<run-id>.json`, both before the first `tofu apply`. A baseline
  is valid only with the run's own `run_id`, the digest of the project id `probe.env` records, an
  explicit `ovh_cloud_project_alerting` list (empty allowed, absent not), `complete: true` and no
  errors; a baseline is never rebuilt from a listing taken after a create could have happened. A
  baseline listing that fails on every attempt is written with its errors, and the run ends there
  before any create. The baseline applies to nameless kinds only; for every other kind, and for any
  kind when `Before` is nil or has no entry for it (the chain), today's inventory and run-id
  matching stands.
  A run is **pre-create** when it has no state, no inventory entry and either no baseline in
  either place or a baseline whose listing failed (errors recorded); nothing can have been created.
  A run is **unreconciled** while its directory exists under `state/probes/`. A probe start is
  refused (`unreconciled-run`, naming the run id and `--cleanup <run-id>`) while any other run is
  unreconciled, pre-create ones included (a run still in progress looks the same). A pre-create run
  is retired by its own end (the failed-baseline case) or by `--cleanup <run-id>`, which then needs
  no passphrase, runs no `tofu`, passes the sweep with nothing to delete and removes the run's files
  and its recovery copy. A missing state alone never proves that nothing was created. [D119
  "findable from the run id"]
- **FR-007 Budget-alert sweep and recovery** — the `budget-alert` root has a probe check, registered
  in `tools/cmd/lz-live/probe.go`, whose `Sweep` loads the run's baseline (the record copy, else the
  recovery copy) and validates it (FR-006). With a valid baseline it lists the project's alerts and,
  for each id absent from the baseline, reads the alert: it deletes it through a `DeleteAlert` API
  operation only if threshold, delay and recipient match the root's plan (1, 3600, the account's own
  address from `GET /me`); any other non-baseline alert (one the owner added later, say) is reported
  and makes the sweep fail without deleting it. It then lists again and fails unless every deleted id
  is gone; it never deletes a baseline id. A pre-create run passes the sweep with nothing to delete.
  A baseline that exists but is unreadable or invalid, or a missing baseline beside state or an
  inventory entry, makes the sweep refuse and delete nothing. The run's recovery files (`probe.env`,
  the passphrase, the root record, the recovery copy of the baseline) are kept, with or without
  state, until the sweep and the leftover check have both passed; so a run whose create succeeded
  remotely while its answer was lost, with no state and no inventory, is finished by a fresh
  `--cleanup <run-id>`.
  **Owner recovery when no trustworthy baseline survives**: the coordinator stops; the owner lists
  the project's alerts in the Control Panel, removes by hand only alerts matching the root's plan
  that were created at or after the run's start (the run id's timestamp), and then runs
  `lz-live probe --cleanup <run-id> --accept-lost-baseline`, which deletes nothing, asks on the
  terminal for the run id to be typed back (refused without a terminal; the gate needs a terminal,
  it does not prove a human: what keeps automated sessions from using it is review and their
  briefs), runs the leftover check and retires the files only if it passes.
- **FR-008 Alert read-back** — the root publishes the scalar public output `alert_id` and the
  check output `budget_alert_check = { alert_id = module.project.budget_alert_id }`, a map of
  strings, which `ProbeCheck.Output` selects (the runner decodes the selected output as a map of
  strings, `tools/internal/live/runner.go:721-741`). The probe check's observer reads that alert
  before the destroy and records its id, threshold, delay and project; a missing alert,
  or any value that differs from the plan's, makes the observation `fail`, and the destroy, the sweep
  and the leftover check still run.
- **FR-009 Second tenant offline** — a two-tenant sandbox fixture (`demo` and `lab`, each with one
  `dev` environment on its own ref, the KD-1 flag set) decodes, reconciles, generates and orders,
  each tenant's stacks after its own `tenant-state`; with distinct project ids per ref it binds; with
  both tenant refs on one id it is refused by FR-003. [B5 offline]
- **FR-010 Live-window procedure** — every live task of this spec is run by the coordinating
  session under D118, never by a subagent, from the clean live clone at a reviewed SHA on `main`;
  code under `tests/live/` or `tools/internal/live/` runs live only after its independent review. By
  coordinator procedure, not by code: the spec's live spend stays within about 5 € of voucher credit
  with 0 € planned card spend; no run starts below 20 € of remaining credit (a manual Control Panel
  or `ovhcloud` check), the budget-alert run is dropped if not started by 2026-10-21 23:00, the
  final chain finishes with its recovery on 2026-10-22, and on 2026-10-23 only verification and
  cleanup start, none whose recovery allowance (deadline plus destroy budget) would end after 23:00.
  No run creates a project, a retained bucket or a credential handed to anyone. Each run's evidence
  records its run id, SHA, outcome, retries seen, approximate cost and the credit left.
- **FR-011 Retained-infrastructure list** — before the final chain starts, the coordinator records
  in `evidence/retained.md` the approved retained infrastructure that stays after 2026-10-24 12:42:
  the retained stacks' resources, the bootstrap's service account, policy and state bucket, and the
  project; for each its kind, id or name, its owner, its removal path (including locked object
  versions and the governance bypass, and credentials to revoke) and its monthly cost, cited or
  marked UNVERIFIED, built from the latest run record's listings and retained states with no new API
  call. A resource a disposable probe or a chain's ephemeral stack created is never on this list:
  while one exists, this spec cannot complete and no further feature run starts, until its deletion
  is verified by a clean leftover check. A listing error in the record is not clean; the list says so.
- **FR-012 Traceability** — `task check:specs` with no argument and CI's source check trace spec
  007; every requirement and task of this spec is traced through its `007/` registry keys.

### Key entities
Listing attempt, run record archive (`attempts/<n>/`), probe baseline, unreconciled probe run,
budget-alert probe check (observe, sweep), project alias refusal, two-tenant sandbox fixture,
retained-infrastructure list. See `data-model.md`.

## Success criteria
- **SC-001**: Against the fake API, a leftover check whose listings answer one 500, 503, 429, a
  reset, a body-read reset or a token-endpoint 5xx each ends `pass`; one answering 500 on every
  attempt ends `fail` with three attempts recorded; a 403, a 404 without absence admitted and an
  undecodable body are attempted once; in a chain whose retained-state retries spent the shared
  budget, the next transient listing is not retried. Guards G24–G28 of `contracts/checks.md` each
  have a killed mutant (V001–V003).
- **SC-002**: Every live run of this spec ends with a clean leftover check, either on the run or by
  a `--cleanup` whose kept first record names each failed attempt's status; no run's evidence says
  "believed" about a listing error (V005).
- **SC-003**: The spec's live runs together cost at most about 5 € of voucher credit and 0 € on the
  card, and after 2026-10-24 12:42 nothing remains in the account but what `evidence/retained.md`
  lists as approved retained infrastructure (V005).
- **SC-004**: An aliased pair of tenant refs is refused with zero child processes on plan, apply,
  destroy, chain and the bootstrap, and the sandbox's KD-1 binding is admitted (V002).
- **SC-005**: Through `lz-live`'s probe entry, in fresh processes: a run whose fake create succeeds
  remotely and loses its answer, leaving no state and no inventory, followed by the process's exit
  and a fresh `--cleanup <run-id>`, ends with only the new alert deleted, its absence verified and
  every baseline alert untouched; a kill between any two start-up writes is retired by `--cleanup`
  and the next start then proceeds; a passing run, a completed cleanup and a run whose baseline
  listing failed each leave `state/probes/` without their directory, and the next start is not
  refused (V003).

## Acceptance and predefined verification
Commands exist today; no new Task target is created. Every offline command runs through
`lz-offline` as `task <target> [-- <path>]`. Live commands are host commands run by the coordinator
under D118 from the live clone, never through the entry. Controls in `contracts/checks.md`.

| Check | Requirements | Verify |
| --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | `task test:live-lane` |
| V002 | FR-003, SC-004 | `task test:stacks; task test:live-lane; task test:bootstrap` |
| V003 | FR-005, FR-006, FR-007, FR-008, SC-001, SC-005 | `task test:live-lane` |
| V004 | FR-009 | `task test:stacks` |
| V005 | FR-004, FR-010, FR-011, SC-002, SC-003 | live, coordinator: `task live:probe -- --reviewed-sha <sha> tests/live/probes/budget-alert`; `task live:chain -- --reviewed-sha <sha> all`; records under `evidence/` |
| V006 | FR-012 | `task check:specs -- specs/007-cleanup-first-live-slice; task test:traceability; task test:foundation-ci` |

## Implementation surface
- `tools/internal/live/`: `leftovers.go` (retry, attempt record, nameless-kind baseline),
  `sweep.go` and `runner.go` (record archive, baseline, unreconciled-run refusal, recovery files),
  `apicheck.go` (`ListAlerts`, `GetAlert`, `DeleteAlert`), `apply.go` and `bootstrap_state.go`
  (alias check in setup), `chain.go` (shared budget passed to the listing); their tests.
- `tools/internal/probes/live/alerting/` (new: the `budget-alert` probe check) and
  `tools/cmd/lz-live/probe.go` (its registration and `--accept-lost-baseline`); the production-entry recovery test in
  `tools/cmd/lz-live/`.
- `tools/internal/stacks/adapter.go` (`CheckProjectAliases`); `tests/fixtures/manifests/sandbox-two-tenants/`.
- `tests/live/probes/budget-alert/` (root, lock file, tests) and its row in
  `tests/live/probes/README.md`; `Taskfile.yml`'s `test:live-lane` (the new package and the root's
  `lz-check unit` line).
- `tools/harness/checks.yaml` (keys, inputs), `Taskfile.yml` (spec lists).
- `docs/how-to/run-the-first-slice.md`, `docs/reference/verification-status.md`, `specs/README.md`.

## Out of scope / postponed (with trigger)
| Item | Why not here | Trigger to start |
| --- | --- | --- |
| A24 narrowed run locks | per-tenant lock names cannot let two tenants' chains run together: each chain reads the other's retained instances (`tools/internal/live/chain.go:192-198`) under that tenant's lock (`tools/internal/stacks/lock.go:85-117`). Design kept for later (both review legs, round 1): split each tenant lock into `tenant-<t>` for retained producers (read shared, written exclusive) and `tenant-<t>-eph` for ephemeral writes (exclusive); every writer, the bootstrap included, takes the names consistently; re-derive the lock-set tests | a second tenant applied in one account, or concurrent runs of two tenants on one workstation; also the artefact-transaction question of `specs/005-first-landing-zone-slice/spec.md:342` |
| B5 live (a second tenant applied in the sandbox) | A17 refuses two tenant refs on one project (FR-003, T001); a second ref would also need an owner edit of `account.env` | a second project, or an owner decision that admits tenant/tenant sharing |
| A second Public Cloud project; lifting KD-1 (Q1, B3) | owner order and owner-only termination (e-mailed token, `kb/api/v1/cloud.json` `/cloud/project/{serviceName}/terminate`); voucher coverage of a second project UNVERIFIED (credit "cannot be transferred", `add-cloud-credit-to-project.mdx:9,24`); KD-1's judge has one global flag (`tools/internal/live/observe.go:166-167`) (D119) | a later spec on a funded account; the seam is `spec.state.project` (spec 005) |
| KD-3: platform-owned bucket for `project` state and artefacts (Q2, B2) | trigger not fired (`specs/005-first-landing-zone-slice/spec.md:348`); the live lane has no state-migration path | the artefact transaction starts, or a tenant S3 credential is held by someone other than the maintainer |
| B1 multi-environment tenants | needs a gap assessment against the existing manifest syntax (`stacks/deployments.yaml`), and a live row needs a project per environment (A17 refuses aliasing them) | a second project, or a later offline spec |
| P11 quota guard live | singleton; destroy semantics UNVERIFIED; the run sheet forbids applying it (`tests/live/probes/README.md` row 3) | a cited destroy semantics, or a disposable project |
| AgentEx, 001/T017–T019 (D87; fired by this spec, D119 Q3) | offline work; this window is for live work | its own offline spec from 2026-10-24 |
| Code that enforces FR-010's procedure (credit floor, dates) | a manual cost check by design (AGENTS.md) | a cost API the lane can read, or unattended live runs |
| Owner reads for a later Q1 (voucher binding, `/cloud/eligibility`, the order funnel summary) | input for a later spec only; nothing here depends on them (D119) | owner, optional |

## Dependencies and stop conditions
- Builds on spec 006's live-lane tasks T022–T026, merged on `main` (PR #123, `15bc579`: bounded verbs, destroy
  budget, the shared `retryBudget`, canary trap, Object Lock assertion): FR-001 draws on
  `retryBudget`, and every live-lane file this spec touches changes there.
- Order of live work (D119): T019 (budget alert; also exercises FR-001, FR-002, FR-006–FR-008), not
  started after 2026-10-21 23:00; then T021 (retained list), which depends on T019 only if it ran;
  then T022 (final chain), finished with its recovery on 2026-10-22; then T023 (verification and
  cleanup) on 2026-10-23.
- Stop: no live run below 20 € of credit, outside the dates of FR-010, or with a red check on the
  reviewed SHA. A refuted P2 makes T019 `fail` and FR-005 unqualified; it is not worked around. A
  resource a disposable run created and could not remove stops every later feature run until its
  deletion is verified; it never goes on `evidence/retained.md`.
