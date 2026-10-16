# Research: Cleanup-first live slice
Date: 2026-10-16 · Revised: 2026-10-16 (spec-set review round 1) · Spec: [spec.md](spec.md) · Code
read at `df8bb3e` (main) and spec 006's US2 branch (`origin/spec-006-us2` at `cf5db72`, PR #123)

Decision records outside the repository: D117–D119, the judges' notes behind D119 and the
coordinator's decisions on review round 1. Claims about OVHcloud cite the API schema or the provider
docs (`kb/`, the maintainer's offline mirror of `api.ovh.com` and `terraform-provider-ovh`) or are
marked UNVERIFIED.

## R1 Listing retry (FR-001, FR-002)
**Finding.** The leftover check lists through `lister.pages` and `lister.objectRaw`
(`tools/internal/live/leftovers.go:275-340`). A non-2xx answer becomes `*statusError{path, code}`
(`:248-253`); a failed `Do` goes through `quiet` (`:160-176`); a failed body read is a plain error
(`:236`); a failed token request is cached in `tokErr` and fails every later request (`:202-209`).
Each lands in `Report.Errors` at once, with no second attempt. The probe runs of 2026-10-16 ended
`fail (0 leftovers, 1 errors)` and `fail (0 leftovers, 13 errors)`; `--cleanup` then passed with
nothing to remove (`specs/003-platform-feasibility/evidence/T027.md:24-29,62-63`,
`T028.md:24-31,66-67`). The errors' text was lost: `--cleanup` rewrote `summary.json` and
`leftovers.json` (`T028.md:132-133`), and it also rewrites `errors.txt` and `listings/*.json`
(`tools/internal/live/runner.go:1028,1036`, `files.go:586`). The chain's baseline listing has the
same exposure: "a baseline listing with errors ends the run there" (`chain.go:9-10`). A cached token
5xx is one plausible source of the 13 errors (UNVERIFIED, P1).

**Decision.**
- Retry inside the lister, per request: a whole-check retry would re-list every kind for one failed
  page and hide which request failed; `apiLister.request` cannot see the shared budget or report
  attempts.
- Transient: 429, 500–599, and transport failures while sending or while reading the body (the body
  read returns `*transportError` too). The token request is retried under the same rule; a 4xx from
  the token endpoint is a refusal and stays cached (fail closed). Not transient: every other 4xx, a
  decode failure, the page bound, a repeating cursor. This mirrors `transient()` for `tofu` output
  (`runner.go:289-306`).
- Waits 5 s and 15 s (three attempts); the state-read retry waits 10 s and 30 s (`runner.go:798`)
  because a `tofu init` is heavier. Default confirmed by the coordinator.
- **Shared budget.** Spec 006 builds one `retryBudget` per reconciliation in `laneRunner.leftovers`
  (`origin/spec-006-us2:tools/internal/live/chain.go:789`, `newRetryBudget(end, minRetryReserve)`)
  and spends it on retained-state reads. The chain hands that same value to the leftover check's
  listings, so retries of either kind draw from one allowance; the probe core builds one from its
  destroy context's deadline. A nil budget means "no retry admitted" (as spec 006's own reading of
  "none"), never "deadline only", so a missing wire fails toward fewer retries. Pinned by a
  chain-level test (T004).
- **Clock.** The landed code has `waitClock{Now() time.Time; After(d) <-chan time.Time}` and
  `wallClock` (`origin/spec-006-us2:tools/internal/live/deadline.go:78-91`), but spec 006's data
  model does not fix it. This spec fixes that interface in its own data model and uses it; if spec
  006's merge renames it, T006 follows the merged name and records it.
- A cancellation during a wait selects on `ctx.Done()` and returns the context's cause; no attempt
  follows.
- A retried page resumes from its cursor; a successful retry still goes through reconciliation, so a
  real leftover listed on the retry is `fail`.
- FR-002 moves the whole run record except `inventory.jsonl` and `baseline.json` (which the cleanup
  needs) into `attempts/<n>/`; this turns P1 from "believed" into an observation.

## R2 Resolved-project aliasing (FR-003, FR-004)
**Question for T001.** Does FR-003's rule refuse two tenants on the one sandbox project?

**What the code and records say (to be confirmed by T001 against the code at its start).**
- The decoder lets one project ref belong to one tenant environment only: a second owner is
  `DUPLICATE_NAME` (`tools/internal/stacks/manifest.go:483-486`).
- The decoder never sees ids. Refs resolve to ids only in the lane, from `account.env`
  (`BoundAccountFromEnv`, `tools/internal/live/publish.go:117-145`, called in the lane's setup at
  `apply.go:213`), and in the bootstrap, whose `setup` reads `account.env` before the `state` phase
  (`bootstrap_state.go:172-188`; `Rest` runs `setup` then `state`, `:157-161`). The bootstrap's
  later binding calls (`:527`, `:575`) run after `state` has already run `tofu` (`init` at `:304`,
  apply at `:356`), so the check belongs in `setup`, not there.
- Nothing compares resolved ids today, so two refs on one id pass `DUPLICATE_NAME` and
  `DUPLICATE_VLAN` (T098 *Harness gaps*).
- The only sharing the manifest admits is the state ref on a tenant ref, under
  `spec.sandbox.shared_state_project` (`manifest.go:583-590`, KD-1, D88).
- T098's decision request, option A; inventory Q8 "refuse at the adapter"; D119: "Don't weaken A17
  to stage a two-tenant demo"; the coordinator confirmed "one ref per project" (round 1).

**Expected outcome.** A second tenant in the one project needs a second ref with the same id as
`DEMO_DEV`; FR-003 refuses it. A17 **refuses** two tenants on one project; B5 stays offline.

**Outcome (T001):** _not yet recorded._

## R3 Nameless resources: baseline, sweep and recovery (FR-006, FR-007, FR-008)
**Finding.** `ovh_cloud_project_alerting` takes `service_name`, `delay`, `email` and
`monthly_threshold` only (`kb/mirror/terraform-provider-ovh/docs/resources/cloud_project_alerting.md`,
*Argument Reference*): no name or tag can carry the run id. Today:
- the leftover check matches it only by the inventory's id (`tests/live/probes/README.md:56`), and
  the inventory is written from the apply's outputs;
- `probeCheck` returns nil for any root but six (`tools/cmd/lz-live/probe.go:33-47`), so a
  `budget-alert` run has no `Sweep` (`runner.go:1158-1164`);
- the leftover lister sends GET only (`leftovers.go:196`), and no API operation deletes an alert
  (`apicheck.go:48-76`);
- after an error with no state, the runner removes the passphrase and the root record unless the run
  is unreconciled (`runner.go:1295-1304`), and only a check with a `Sweep` marks a failed leftover
  check unreconciled (`sweep.go:47-56`).
So an alert created by an apply whose answer was lost would have no state, no inventory, no sweep
and, after the process exits, possibly no recovery files: nothing could remove it.

**Decision.**
- **Baseline.** Before the first create, the probe run lists `/cloud/project/<p>/alerting`
  (`kb/api/v1/cloud.json:5843`) into `baseline.json` in the run directory. It applies to nameless
  kinds only; for named kinds the chain's rule stands (`Before` "never decides",
  `leftovers.go:93-95`).
- **Sweep.** A `budget-alert` probe check (new package `tools/internal/probes/live/alerting`),
  registered in `probeCheck`, whose `Sweep` lists the alerts, deletes each id absent from the
  baseline (`DELETE /cloud/project/{serviceName}/alerting/{id}`, "Delete an alert", action
  `publicCloudProject:apiovh:alerting/delete`, `kb/api/v1/cloud.json:5916-5970`), lists again and
  fails unless they are gone. Without a readable baseline it refuses and deletes nothing.
- **Recovery files.** Kept until the sweep and the leftover check pass, with or without state, so a
  fresh `--cleanup` can reload the baseline. Pinned by a production-entry test through
  `tools/cmd/lz-live` with a fresh process (T013), as spec 003's T037 did for the sweep.
- **Earlier orphans.** Two options were weighed: match baseline ids against earlier runs'
  `inventory.jsonl` and `leftovers.json`, or refuse to start while an earlier probe run is
  unreconciled. Chosen: the refusal. A probe run's directory under
  `accounts/<account>/state/probes/<run-id>/` is removed only when its destroy, sweep and leftover
  check passed (`runner.go:1306-1312`), so its presence is exactly "unreconciled"; the refusal needs
  no record from another checkout and cannot be fooled by a record that was overwritten or never
  written. Cost: a stale directory from an older run must be cleaned up first, which is the point.
- **Read-back.** The check's `Observe` reads `GET …/alerting/{id}` for the published `alert_id`
  before the destroy and records id, threshold, delay and project; a mismatch fails the observation
  and the runner still destroys and sweeps (the observer never ends the run early).
- **Risk.** An alert created by someone else during the run is deleted by the sweep as the run's.
  The sandbox has one maintainer; the evidence names any id the sweep deleted.

## R4 The budget-alert root (FR-005)
- **Probe, not the `project` stack.** A retained alert in `project` state would be orphaned or
  duplicated by a later state change of that stack (D119); a probe has its own encrypted state.
- **Through the module, no fallback.** The raw resource ran on 2026-10-10 (spec 005 T010 run 4).
  What has not run is `modules/cloud-project` with `budget_alert` (`main.tf:41-50`, validation at
  `variables.tf:58-61`) and its reference-mode read (`main.tf:29-33`); in reference mode the module
  creates no project (`main.tf:9`). An inlined resource would not qualify this, so the round-0
  fallback is dropped (review round 1).
- **Initialisation offline first.** The unit runner initialises with `-lockfile=readonly`
  (`tools/internal/checks/unit.go:49`, `static.go:130`), so the root carries its pinned
  `.terraform.lock.hcl` (the `alerting` root's: the module uses only `ovh/ovh` 2.21.0) from T010.
  T010's mocked plan through the entry is the offline observation of P2.
- **Values.** Threshold 1 and delay 3600, as the `alerting` root
  (`tests/live/probes/alerting/main.tf:8-13`); the address from `data.ovh_me`.
- **Not a cap; cost.** "billing alert configurations" (`cloud.json:5867`): a notification (P4). No
  price for an alert in the sources read (UNVERIFIED that it is free); T010 run 4's alert cost
  nothing visible.

## R5 Narrowed run locks (A24): deferred
A chain locks its selection, its ephemeral stacks and every retained instance
(`tools/internal/live/chain.go:186-198`), because the leftover check and the L7 collector read
retained states (T062). Locks are per `account` and per `tenant-<t>`, exclusive
(`tools/internal/stacks/lock.go:85-117`). Round 0 proposed reader/writer modes on the same names;
both review legs showed it cannot work: the `demo` chain holds `tenant-demo` exclusively for its
ephemeral writes, while the `lab` chain must read `demo-state` under `tenant-demo` shared, and the
reverse. The design that can work is a split: `tenant-<t>` for retained producers (shared to read,
exclusive to write) and `tenant-<t>-eph` for ephemeral writes (exclusive), with every writer (lane,
bootstrap) taking the names consistently. Since A17 keeps the second tenant offline, nothing in this
window needs it; it is out of scope with its trigger (spec). Counterpoint kept: concurrent tenant
runs are also a trigger of the artefact transaction (`specs/005-first-landing-zone-slice/spec.md:342`),
which the later spec must answer.

## R6 The live window (FR-010, FR-011)
- **Dates.** Voucher expiry 2026-10-24 12:42; live work stops 2026-10-23 23:00 (D117); D118's grant
  ends 2026-10-24 23:59 or below 20 € of credit. D119's "Thursday 23" names a Friday; the date
  governs. Cut-offs: the budget-alert run is dropped if not started by 2026-10-21 23:00; the final
  chain finishes with its recovery on 2026-10-22; 2026-10-23 is verification and cleanup only.
- **Procedure, not code.** The credit floor and the dates are checked by the coordinator by hand
  (AGENTS.md "Bounded extension": "The spend stop is a manual cost check … not code"); `lz-live`
  enforces the reviewed SHA, the live clone and redaction.
- **Runs planned.** One budget-alert probe (T019), one final chain (T022), cleanup runs as needed
  (T023). A full chain has cost well under 1 € (INBOX spend total, run `20261016T082140Z-1797`, per
  D119's brainstorm); the five probe runs of 2026-10-16 cost about nothing (same source). Ceiling:
  about 5 € of voucher, 0 € card.
- **Recovery allowance.** Deadline plus destroy budget (spec 006 FR-002: at least
  `DefaultDeadline`).
- **What stays, and what may not.** Approved retained infrastructure: the retained stacks
  (`account-governance`, `demo-state`, `demo-dev-project`), the state buckets under Object Lock
  (governance mode, D104) with their versions, the bootstrap's service account, policy and account
  bucket, and the trial project. Each gets a removal path and a monthly cost in
  `evidence/retained.md` (prices cited or UNVERIFIED). Anything a probe or a chain's ephemeral stack
  created is never on that list: it blocks completion and later runs until a clean leftover check
  shows it gone (review round 1).
