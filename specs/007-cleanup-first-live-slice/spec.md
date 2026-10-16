# Feature Specification: Cleanup-first live slice
Feature: `007-cleanup-first-live-slice` · Created: 2026-10-16 · Status: draft
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
landing zone's own budget-alert path in a disposable run (P10), and only then turns to a second
tenant.

Visible outcome: a probe or chain run whose listing answers one 500 still ends with a clean
leftover check and a record of what failed and how often; an account file that maps two tenant
project refs onto one project is refused before any `tofu` call; the budget-alert input of
`modules/cloud-project` is shown to create and remove an alert on the sandbox project, with nothing
retained; two tenants' partial chains no longer exclude each other on one workstation (offline
proof); and before the voucher expires, every resource that stays is listed with how to remove it
and what it costs per month.

This spec is the second vertical slice in D87's sense, so it **fires the AgentEx trigger**
(constitution VI; D119 Q3). AgentEx (001/T017–T019) is scheduled as its own offline spec from
2026-10-24, outside this live window; nothing here implements it.

**In** (inventory ids in brackets):
- bounded retry of transient errors on leftover listings, and a run record that keeps every failed
  attempt across `--cleanup` (D119 item 1);
- resolved-project aliasing refused where refs resolve [A17], with its first task establishing
  whether the rule refuses two tenants on one project (D119 item 2);
- a disposable budget-alert apply and destroy through `modules/cloud-project`, outside the retained
  `project` stack, nothing retained [B4 alert part, P10] (D119 item 3);
- narrowed run locks [A24] and a second tenant [B5]: offline, and live only if A17 permits two
  tenants on one project (D119 item 4);
- the live-window guards of D119 written into the tasks: voucher ceiling, card spend, reviewed SHA,
  the coordinator under D118, the retained-resource list, the 23:00 stop;
- tracing spec 007 by default and in CI's source check.

**Out**: a second Public Cloud project (KD-1), KD-3, multi-environment tenants (B1), a
platform-owned bucket (B2), B3, the quota guard live (P11) and AgentEx. See *Out of scope /
postponed (with trigger)*.

## Clarifications

### Session 2026-10-16
Answered from D119, the judges' notes, the code at `df8bb3e` and the evidence files. Open items are
listed last, each with the default this spec applies until the owner answers.
- Q: Is P10 already observed? → A: For the raw resource, yes: the `alerting` probe created and
  destroyed one `ovh_cloud_project_alerting` on the trial project (spec 005 T010 run 4,
  `20261010T172559Z-eb9b`, `specs/005-first-landing-zone-slice/evidence/T010.md:13`); spec 005's
  premise table still says UNVERIFIED. What has never run live is the landing zone's own path:
  `modules/cloud-project`'s `budget_alert` input (`modules/cloud-project/main.tf:41-50`), its
  validation and the reference-mode read beside it. FR-005 qualifies that path.
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
  records the consequence; until it closes, every US4 live task is gated.
- Q: Do the listing retries share spec 006's retry budget? → A: Yes. Spec 006 FR-008 introduces one
  retry budget per leftover check for retained-state reads (`retryBudget`); listing waits are
  admitted by the same budget, so retries never eat the time the read and the remaining listings
  need.
- Q: Does a second tenant fire KD-3's trigger? → A: No. The trigger is "the artefact transaction
  starts, or a tenant S3 credential is held by someone other than the maintainer"
  (`specs/005-first-landing-zone-slice/spec.md:348`); a second maintainer-held tenant is neither.
- **Open, owner's, with default** — weekday of the cleanup day. D119 says "Thursday 23", but
  2026-10-23 is a Friday (2026-10-22 is the Thursday). Default: the date governs, as in D117's stop
  (2026-10-23 23:00). The last feature run (the final chain, T023) finishes with its recovery on
  2026-10-22; 2026-10-23 is for verification and cleanup only, and no run starts on it whose
  recovery allowance would cross 23:00.
- **Open, owner's, with default** — does the sandbox flag also admit two tenant refs on one id?
  Default: no (D119: "Don't weaken A17 to stage a two-tenant demo"). With this default, A24 and B5
  stay offline and T020–T021 close as not run.
- **Open, owner's, with default** — the budget alert's recipient (inventory B4 **[OWNER]**).
  Default: the account's own contact address, read at plan time from `data.ovh_me` as the existing
  `alerting` root does; no address is committed or stored outside the provider's state.
- **Open, owner's, with default** — the listing retry bound. Default: three attempts, waiting 5 s
  and 15 s, each wait admitted only by the shared retry budget and the run's deadline.

## Decisions and proposals
- **Approved direction**: D119 (scope and order: retry, A17, P10, then A24/B5 only if A17 permits);
  D118 (named live targets by the coordinating session until 2026-10-24 23:59 or below 20 € of
  credit; reviewed SHA on `main`; independent review before code under `tests/live/` or
  `tools/internal/live/` runs live); D117 (live work stops 2026-10-23 23:00); D100's counterpoint
  (lock only what a chain needs before a second tenant exists); D107 (tags do not scope bucket
  reads, so isolation rests on one project per tenant).
- **Proposals, credible alternatives** (detail in `research.md`):
  - listing retries inside the leftover check's lister, classifying by status code, rejecting a
    retry of the whole check (R1);
  - the alias check in the stacks adapter, called by the lane where the account binds, rejecting a
    decoder check, which never sees ids (R2);
  - a pre-apply baseline of nameless kinds for probe runs, rejecting a tag or name (the alert takes
    neither, `cloud_project_alerting.md`, argument reference) (R3);
  - shared locks for what a chain only reads, rejecting per-instance locks, which would change
    every lock name the lane and the bootstrap use (R5).

## User Scenarios & Testing

### User Story 1 — A transient listing error does not fail the cleanup (Priority: P1)
The coordinator runs a probe. Its leftover check lists private networks and the API answers 500
once. The check waits, prints one line naming the listing, the status and the attempt, lists again,
and ends `pass`. If the API answers 500 on every attempt, the check ends `fail` with the path, the
last status and the attempt count, and the run record keeps each attempt. When the coordinator
then runs `--cleanup <run-id>`, the first record stays on disk beside the new one, so the evidence
can quote what failed rather than "believed".
**Independent test**: V001. Interruption: a deadline that leaves less than the next wait starts no
wait and reports the error at once.

### User Story 2 — Two refs on one project are refused before anything runs (Priority: P1)
The maintainer binds an account file in which `LZ_PROJECT_ID_DEMO_DEV` and `LZ_PROJECT_ID_SHOP_DEV`
hold the same id. `task live:plan` stops before any `tofu` call with `project-alias` naming both
refs and not the id, and the next safe action (give each ref its own project, or drop one row). The
sandbox account, where `STATE` and `DEMO_DEV` share one id under the KD-1 flag, still binds.
**Independent test**: V002.

### User Story 3 — The landing zone's budget alert is shown live and leaves nothing (Priority: P2)
The coordinator runs the `budget-alert` probe on a reviewed SHA. The probe records the project's
alert ids before it creates anything, applies `modules/cloud-project` in reference mode with an
alert of threshold 1 and delay 3600, reads the alert back, destroys it and checks that its id is no
longer listed. If the apply fails after the API created the alert, the leftover check still finds
it (an id absent from the baseline) and `--cleanup <run-id>` removes it; an alert that existed
before the run is never touched.
**Independent test**: V003, V005.

### User Story 4 — Two tenants' partial chains do not exclude each other (Priority: P3)
On a two-tenant manifest, a partial chain over one tenant's network and runtime and a partial chain
over the other's run at the same time on one workstation: each holds its own tenant's lock
exclusively and the locks of what it only reads shared. An apply that writes a retained instance
the other chain reads is still refused while that chain runs. The two-tenant sandbox fixture
generates, orders and plans offline, and with both tenant refs on the sandbox's one id it is
refused by US2's rule. Live: only if T001 records that A17 permits two tenants on one project
(default: it does not, and the live part is not run).
**Independent test**: V004 (offline), V005 (live, gated).

### User Story 5 — The live window closes clean and on time (Priority: P1)
Before the final chain, the coordinator writes the list of everything that stays after 2026-10-24
12:42, with its removal path and monthly cost. The final chain runs on a reviewed SHA with the
retry in place and ends with a clean leftover check. On 2026-10-23 only verification and cleanup
runs start, none whose recovery would cross 23:00; each run's cost is recorded.
**Independent test**: V005.

### Edge cases
A 500 on the second page of a paginated listing (resume from the same cursor, no duplicate items);
a 429; a reset connection; a 403 or a 404 where absence is not admitted (never retried); a 200
with a body that is not JSON (never retried); a deadline that ends during a wait; a budget whose
reserve is already used up; the bearer token inside a retried error (redacted); a second
`--cleanup` of the same run (a second archived record, not an overwrite); a run whose destroy
failed before any leftovers record existed. Two refs with one id where one is the state ref
without the flag; three refs with one id; an `LZ_PROJECT_ID_X` the manifest never names (not
judged). A baseline listing that fails after its retries (the run stops before the first create);
an alert created by someone else during the run (reported as a leftover, removed only by an
explicit `--cleanup`). A process holding a shared lock killed (the kernel releases it); a chain
whose selection changed while its locks were taken (refused, as today).

## Premises

| ID | Premise | Source | Probe | Status |
| --- | --- | --- | --- | --- |
| P1 | The listing errors of runs `20261016T150207Z-be89` and `20261016T150441Z-eb3a` were transient 5xx answers that a retry seconds later would have passed | the coordinator's reading; the error text was overwritten by `--cleanup` (`T027.md:27-29`, `T028.md:28-31`) | the attempt records of T018 and T023 (FR-002) | UNVERIFIED (believed); if the records show 4xx or persistent 5xx, FR-001's classification stands and the finding goes to research R1 |
| P2 | The probe core can run a root that calls `modules/cloud-project` by a relative source | `lz-live probe` runs `tofu -chdir=<root>` in the reviewed checkout (`tools/internal/live/runner.go:171`); not observed with a module call | T010's mocked plan through `lz-check unit` (offline init from the entry's mirror), then T018 | believed; fallback: none needed for the mocked plan; if the live init refuses, T018 records it and the root inlines the module's resource with its validation copied (weaker evidence, said so) |
| P3 | `GET /cloud/project/{serviceName}/alerting` lists every alert id of the project, so a pre-apply baseline finds a nameless alert | API schema `kb/api/v1/cloud.json:5843-5870` ("List all billing alert configurations for a public cloud project", action `publicCloudProject:apiovh:alerting/get`) | T018 (baseline, then listing after apply and after destroy) | doc; UNVERIFIED live for the baseline use |
| P4 | A budget alert notifies; it does not cap spend | API description "billing alert configurations" (`cloud.json:5867`); provider docs `cloud_project_alerting.md` (delay, email, threshold only) | none: no task relies on a cap | doc |
| P5 | `flock(2)` with `LOCK_SH` and `LOCK_EX` on one open file description per holder gives the reader/writer exclusion FR-007 needs, across processes, released on exit | Linux `flock(2)`; today's exclusive locks rely on the same call (`tools/internal/stacks/lock.go`) | T013's fresh-process cases | believed (OS behaviour, tested offline) |

No task relies on an OVHcloud claim beyond P1–P4; each live observation is recorded in its task's
evidence.

## Requirements

### Functional requirements
- **FR-001 Listing retry** — the leftover check retries one listing request (an array page or a
  single-object read) that failed with a status of 429 or 500–599 or with a transport failure
  (reset connection, timeout, refused connection), at most twice after the first attempt, waiting
  5 s and then 15 s. A wait is started only when the run's deadline and the check's retry budget
  (spec 006 FR-008) both admit it; otherwise the error stands with "no retry" and the reason. Any
  other failure (another 4xx, a body that does not decode, a page bound or a repeating cursor) is
  never retried. A retried page resumes from the cursor it failed on. Each retry prints one line
  `LZ-LIVE retry listing <path> in <wait>: <status or class> (attempt <n> of <max>)`, redacted. A
  listing that still fails leaves the check `fail`, never `pass`. [D119 item 1]
- **FR-002 Listing record kept** — `leftovers.json` carries every failed listing attempt (path,
  attempt number, status or failure class, whether a retry followed), redacted. A `--cleanup` of a probe
  run (`lz-live probe --cleanup <run-id>`) first moves the run's existing `summary.json` and `leftovers.json` into
  `attempts/<n>/` of the run directory (`n` the next free number), then writes its own; a run
  without those files is cleaned up as today. [D119 item 1; T027/T028 learnings]
- **FR-003 Project aliasing refused** — before any `tofu` call, the lane (plan, apply, destroy,
  chain) and the bootstrap's state phase refuse a bound account in which two project refs that the
  manifest uses resolve to one project id: condition `project-alias`, naming both refs and never
  the id, with the next action. The only alias admitted is the state ref sharing a tenant
  environment's id when `spec.sandbox.shared_state_project` is true (KD-1); two tenant-environment
  refs never share an id, with or without the flag. Refs the manifest does not use are not judged.
  [A17]
- **FR-004 A17 consequence first** — before any task of US4 starts, T001 records in
  `research.md` (R2) whether FR-003 refuses two tenants on one project, with the code lines and
  decisions that decide it, and the consequence: US4's live tasks run only on "permits"; on
  "refuses" they close as not run, and A24 and B5 are delivered offline. [D119 item 4]
- **FR-005 Disposable budget alert** — a probe root `tests/live/probes/budget-alert/` applies
  `modules/cloud-project` in `reference` mode on the run's project with `budget_alert` enabled
  (threshold 1 in the account currency, delay 3600, the account's own contact address from
  `data.ovh_me`), reads the alert back, and is destroyed by the probe core; its state is the probe
  core's encrypted local state, so nothing enters the retained `project` stack and nothing is
  retained. The plan creates no `ovh_cloud_project`. The run records the alert id, its threshold
  and delay as read back, that destroy removed it, and the run's cost. [P10, B4 alert part]
- **FR-006 Nameless kinds found from the run id** — before its first create, a probe run lists
  the project's alerts and writes the ids, redacted, to `baseline.json` in its run directory; a
  failed baseline listing (after FR-001's retries) ends the run before any create. The leftover
  check and `--cleanup <run-id>` treat an alert id absent from that baseline as the run's; an id
  present in it is never a leftover of the run and never removed. [D119 "findable from the run id"]
- **FR-007 Narrowed run locks** — a lock store offers shared as well as exclusive locks. A chain
  takes exclusive locks for the instances it applies or destroys (its selection and its ephemeral
  stacks) and shared locks for the retained instances it only reads (leftover check, L7 collector).
  Two shared holders of one lock coexist; a shared and an exclusive holder never do; locks are still
  taken without waiting in the fixed order, released on exit, and the selection is re-checked under
  them. `apply`, `destroy` and `plan` keep their exclusive locks. [A24, D100]
- **FR-008 Second tenant offline** — a two-tenant sandbox fixture (`demo` and `lab`, each with one
  `dev` environment on its own ref, the KD-1 flag set) decodes, reconciles and generates; with
  distinct project ids per ref it binds and the run order puts each tenant's stacks after its own
  `tenant-state`; with both tenant refs on one id it is refused by FR-003. Two partial chains over
  its two tenants hold their locks together in the fake runner. [B5 offline]
- **FR-009 Second tenant live (gated)** — only when T001 records "permits": the sandbox manifest
  gains the second tenant's rows and one chain applies and destroys both tenants' ephemeral stacks
  on the one project, with the leftover check clean; isolation is not claimed (KD-1). On "refuses"
  this requirement is met by recording, in its tasks' evidence, that it was not run and why. [B5
  live]
- **FR-010 Live-window guards** — every live task of this spec is run by the coordinating session
  under D118, never by a subagent, from the clean live clone at a reviewed SHA on `main`; code
  under `tests/live/` or `tools/internal/live/` runs live only after its independent review. The
  spec's live spend stays within about 5 € of voucher credit with 0 € planned card spend; no run
  starts below 20 € of remaining credit, on or after 2026-10-23 except verification and cleanup,
  or when its recovery allowance (deadline plus destroy budget) would end after 2026-10-23 23:00.
  No run creates a project, a retained bucket or a credential handed to anyone. Each run's evidence
  records its run id, SHA, outcome, retries seen and approximate cost.
- **FR-011 Retained-resource list** — before the final chain starts, the coordinator records in
  `evidence/retained.md` every resource that stays in the account after 2026-10-24 12:42: kind, id
  or name, why it is kept, its removal path (including locked object versions and the governance
  bypass, and credentials to revoke) and its monthly cost, cited or marked UNVERIFIED, built from
  the latest run record's listings and retained states with no new API call. A listing error in
  that record is not clean; the list says so.
- **FR-012 Traceability** — `task check:specs` with no argument and CI's source check trace spec
  007; every requirement and task of this spec is traced through its `007/` registry keys.

### Key entities
Listing attempt, run record archive (`attempts/<n>/`), probe baseline, project alias refusal, lock
request (name, shared), two-tenant sandbox fixture, retained-resource list. See `data-model.md`.

## Success criteria
- **SC-001**: Against the fake API, a leftover check whose listings answer one 500, 503, 429 or
  reset each ends `pass`, and one answering 500 on every attempt ends `fail` with three attempts
  recorded; a 403, a 404 without absence admitted and an undecodable body are attempted once. Each
  guard of `contracts/checks.md` G24–G28 has a killed mutant (V001–V004).
- **SC-002**: Every live run of this spec ends with a clean leftover check, either on the run or by
  a `--cleanup` whose kept first record names each failed attempt's status; no run's evidence says
  "believed" about a listing error (V005).
- **SC-003**: The spec's live runs together cost at most about 5 € of voucher credit and 0 € on the
  card, and after 2026-10-24 12:42 nothing remains that `evidence/retained.md` does not list (V005).
- **SC-004**: An aliased pair of tenant refs is refused before any `tofu` call on every lane verb
  and the bootstrap's state phase, and the sandbox's KD-1 binding is admitted (V002).
- **SC-005**: Two partial chains over different tenants run concurrently in the fake runner, and a
  write to a retained instance another chain reads is refused while it runs (V004).

## Acceptance and predefined verification
Commands exist today; no new Task target is created. Every offline command runs through
`lz-offline` as `task <target> [-- <path>]`. Live commands are host commands run by the coordinator
under D118 from the live clone, never through the entry. Controls in `contracts/checks.md`.

| Check | Requirements | Verify |
| --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | `task test:live-lane` |
| V002 | FR-003, SC-004 | `task test:stacks; task test:live-lane` |
| V003 | FR-005, FR-006, SC-001 | `task test:live-lane` |
| V004 | FR-007, FR-008, SC-005 | `task test:stacks; task test:live-lane` |
| V005 | FR-004, FR-009, FR-010, FR-011, SC-002, SC-003 | live, coordinator: `task live:probe -- --reviewed-sha <sha> tests/live/probes/budget-alert`; `task live:chain -- --reviewed-sha <sha> all`; records under `evidence/` |
| V006 | FR-012 | `task check:specs -- specs/007-cleanup-first-live-slice; task test:traceability; task test:foundation-ci` |

## Implementation surface
- `tools/internal/live/`: `leftovers.go` (retry, attempt record), `sweep.go` and `runner.go`
  (record archive, probe baseline), `apply.go` and `bootstrap_state.go` (alias check call),
  `chain.go` (lock modes); their tests.
- `tools/internal/stacks/`: `adapter.go` (`CheckProjectAliases`), `lock.go` (shared locks); their
  tests; `tests/fixtures/manifests/sandbox-two-tenants/`.
- `tests/live/probes/budget-alert/` and its row in `tests/live/probes/README.md`; the root's
  `lz-check unit` line in `Taskfile.yml`'s `test:live-lane`.
- `tools/harness/checks.yaml` (keys, inputs), `Taskfile.yml` (spec lists).
- `docs/how-to/run-the-first-slice.md`, `docs/reference/verification-status.md`,
  `specs/README.md`; gated: `stacks/deployments.yaml` (FR-009 only).

## Out of scope / postponed (with trigger)
| Item | Why not here | Trigger to start |
| --- | --- | --- |
| A second Public Cloud project; lifting KD-1 (Q1, B3) | owner order and owner-only termination (e-mailed token, `kb/api/v1/cloud.json` `/cloud/project/{serviceName}/terminate`); voucher coverage of a second project UNVERIFIED (credit "cannot be transferred", `add-cloud-credit-to-project.mdx:9,24`); KD-1's judge has one global flag (`tools/internal/live/observe.go:166-167`) (D119) | a later spec on a funded account; the seam is `spec.state.project` (spec 005) |
| KD-3: platform-owned bucket for `project` state and artefacts (Q2, B2) | trigger not fired (`specs/005-first-landing-zone-slice/spec.md:348`); the live lane has no state-migration path | the artefact transaction starts, or a tenant S3 credential is held by someone other than the maintainer |
| B1 multi-environment tenants | needs a gap assessment against the existing manifest syntax (`stacks/deployments.yaml`), and a live row needs a project per environment (A17 refuses aliasing them) | a second project, or a later offline spec |
| B3 (second project for a tenant row) | as the first row | as the first row |
| P11 quota guard live | singleton; destroy semantics UNVERIFIED; the run sheet forbids applying it (`tests/live/probes/README.md` row 3) | a cited destroy semantics, or a disposable project |
| AgentEx, 001/T017–T019 (D87; fired by this spec, D119 Q3) | offline work; this window is for live work | its own offline spec from 2026-10-24 |
| Concurrent live runs of two tenants | FR-007 is proven in fakes; live concurrency needs a second tenant live, gated by A17 | FR-009 runs, or two tenant projects exist |
| Owner reads for a later Q1 (voucher binding, `/cloud/eligibility`, the order funnel summary) | input for a later spec only; nothing here depends on them (D119) | owner, optional |

## Dependencies and stop conditions
- Depends on spec 006's live-lane tasks T022–T026 on `main` (bounded verbs, destroy budget, the
  shared `retryBudget`, canary trap, Object Lock assertion): FR-001 draws on `retryBudget`, and
  every file US1–US4 touch is changed there. US1 starts after that branch lands.
- T001 gates US4: on "refuses" (the default), T020 and T021 close as not run.
- Order of live work (D119): T018 (budget alert, which also exercises FR-001/FR-002 and FR-006),
  then T022 (retained list), then T023 (final chain) on 2026-10-22 at the latest, then T024
  (verification and cleanup) on 2026-10-23. T021, if T001 permits, runs before T022.
- Stop: no live run below 20 € of credit, after the 23:00 limit of FR-010, or with a red check on
  the reviewed SHA. A refuted P2 stops T018 until T012's fallback lands; a live run that leaves a
  leftover it cannot remove stops every later live task until it is removed or listed in
  `evidence/retained.md` with its cost.
