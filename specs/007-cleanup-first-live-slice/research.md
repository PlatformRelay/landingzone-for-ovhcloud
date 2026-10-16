# Research: Cleanup-first live slice
Date: 2026-10-16 · Spec: [spec.md](spec.md) · Code read at `df8bb3e` (main) and, for spec 006's
retry budget, its task branch (`tools/internal/live/deadline.go`, `retryBudget`)

Decision records outside the repository: D117–D119 and the judges' notes behind D119. Claims about
OVHcloud cite the API schema or the provider docs (`kb/`, the maintainer's offline mirror of
`api.ovh.com` and `terraform-provider-ovh`) or are marked UNVERIFIED.

## R1 Listing retry (FR-001, FR-002)
**Finding.** The leftover check lists through `lister.pages` and `lister.objectRaw`
(`tools/internal/live/leftovers.go:275-340`). A non-2xx answer becomes `*statusError{path, code}`
(`:248-253`), a transport failure goes through `quiet` (`:160-176`), and either is appended to
`Report.Errors` at once, with no second attempt. The probe runs of 2026-10-16 ended `fail (0
leftovers, 1 errors)` and `fail (0 leftovers, 13 errors)`; `--cleanup` then passed with nothing to
remove (`specs/003-platform-feasibility/evidence/T027.md:24-29,62-63`, `T028.md:24-31,66-67`). The
errors' text was lost: `--cleanup` rewrote `summary.json` and `leftovers.json` (`T028.md:132-133`).
The chain's baseline listing has the same exposure: "a baseline listing with errors ends the run
there" (`tools/internal/live/chain.go:9-10`).

**Decision.** Retry inside the lister, per request, not the whole check:
- a whole-check retry would re-list every kind (18 regions of bucket listings, `leftovers.go:20-27`)
  for one failed page, and would hide which request failed;
- classify by status code: 429 and 500–599 and transport failures are transient; every other 4xx,
  a decode failure, the page bound and a repeating cursor are not. This mirrors `transient()` for
  `tofu` output (`tools/internal/live/runner.go:289-306`), which also treats 5xx and resets as
  transient, and keeps 4xx fail-closed;
- waits 5 s and 15 s (three attempts). The state-read retry waits 10 s and 30 s
  (`runner.go:798`) because a `tofu init` is heavier; a listing is one GET. Default, open to the
  owner (spec Clarifications);
- every wait is admitted by spec 006's `retryBudget` (`admit(now, wait)`) and the context deadline,
  so retries cannot take the reserve the remaining listings need;
- a page resumes from the cursor that failed, so a retry cannot duplicate or drop items;
- the attempt record (FR-002) is what turns P1 from "believed" into an observation.

**Rejected.** Retrying in `apiLister.request` (below the lister): it would hide attempts from the
report and could not see the shared budget. A longer backoff: the evidence shows single failures,
and the check runs inside the destroy-on-exit budget.

## R2 Resolved-project aliasing and its consequence for a second tenant (FR-003, FR-004)
**Question for T001.** Does FR-003's rule refuse two tenants on the one sandbox project?

**What the code and records say (to be confirmed by T001 against the code at its start).**
- The decoder lets one project ref belong to one tenant environment only: a second owner is
  `DUPLICATE_NAME` ("project %s is owned by %s and %s/%s", `tools/internal/stacks/manifest.go:483-486`).
- The decoder never sees ids. Refs resolve to ids only in the lane, from `account.env`
  (`BoundAccountFromEnv`, `tools/internal/live/publish.go:117-145`; called at `apply.go:213` and
  `bootstrap_state.go:527,575`). Nothing compares the resolved ids today, so two refs on one id pass
  `DUPLICATE_NAME` and `DUPLICATE_VLAN` (T098 *Harness gaps*,
  `specs/005-first-landing-zone-slice/evidence/T098.md`).
- The only sharing the manifest admits is the state ref on a tenant ref, under
  `spec.sandbox.shared_state_project` (`manifest.go:583-590`, KD-1, D88).
- T098's decision request, option A: refuse "two `LZ_PROJECT_ID_<REF>` of one manifest" naming one
  id "unless it is the KD-1 state/tenant sharing". The inventory's Q8 recommends "refuse at the
  adapter". D119: "Don't weaken A17 to stage a two-tenant demo"; the judges: "Never weaken alias
  refusal to manufacture a two-tenant live demonstration".

**Expected outcome.** A second tenant in the one project needs a second ref (the decoder forbids
sharing one), and that ref resolves to the same id as `DEMO_DEV`: FR-003 refuses it. So A17
**refuses** two tenants on one project, and A24 and B5 stay offline (FR-008); FR-009's tasks close
as not run. "Permits" would need a new deviation flag for tenant/tenant sharing, which D119 rules
out.

**Outcome (T001):** _not yet recorded._

## R3 Finding nameless resources from the run id (FR-006)
`ovh_cloud_project_alerting` takes `service_name`, `delay`, `email` and `monthly_threshold` only
(`kb/mirror/terraform-provider-ovh/docs/resources/cloud_project_alerting.md`, *Argument Reference*):
no name and no tag can carry the run id. Today the leftover check matches it by the id the
inventory recorded (`tests/live/probes/README.md` row "project alert", run-sheet note at line 56),
and the inventory is written from the apply's outputs, so an apply that fails after the API created
the alert leaves an alert nobody can attribute.

**Decision.** Before its first create, a probe run lists `/cloud/project/<p>/alerting`
(`kb/api/v1/cloud.json:5843`, action `publicCloudProject:apiovh:alerting/get`) and writes the ids to
`baseline.json` in the run directory, as the chain already does for every kind (`chain.go:89-100`).
A listed id absent from the baseline is a candidate of the run; an id in it is never one. The
baseline is written before the first `tofu apply`, so it exists whatever the apply does. A failed
baseline listing stops the run before any create (fail closed, as the chain does).

**Risk.** An alert created by someone else during the run is reported as the run's leftover. The
sandbox has one maintainer; removal still needs an explicit `--cleanup <run-id>`, so a wrong
attribution is visible before anything is deleted.

## R4 The budget-alert run (FR-005)
- **Probe, not the `project` stack.** A retained alert in `project` state would be orphaned or
  duplicated by a later state change of that stack (D119, from the strong leg's finding on
  `stages/project/main.tf:20`); a probe has its own encrypted state under the run id and is
  destroyed in `finally`.
- **Through the module.** The raw resource was already created and destroyed on the trial project
  (spec 005 T010 run 4, `20261010T172559Z-eb9b`). What has not run is the landing zone's path:
  `modules/cloud-project` with `budget_alert` (`main.tf:41-50`, the validation at
  `variables.tf:58-61`) and its reference-mode read (`data.ovh_cloud_project`, `main.tf:29-33`). In
  reference mode the module creates no project (`count = var.mode == "adopt" ? 1 : 0`, `main.tf:9`).
- **Values.** Threshold 1 and delay 3600, as the `alerting` root (`tests/live/probes/alerting/
  main.tf:8-13`); the delay is one of `cloud.AlertingDelayEnum` (`kb/api/v1/cloud.json`). The
  address is the account's own (`data.ovh_me`), so no address is committed.
- **Not a cap.** The API calls these "billing alert configurations" (`cloud.json:5867`): a
  notification, not a spending limit (P4). The spend ceiling stays a manual check (AGENTS.md).
- **Cost.** An alert has no price in the API or provider docs read (UNVERIFIED that it is free);
  the T010 run 4 alert cost nothing visible.

## R5 Narrowed run locks (FR-007)
**Finding.** A chain takes the locks of its selection, its ephemeral stacks and **every retained
instance** (`tools/internal/live/chain.go:186-198`), because the leftover check and the L7 collector
read retained states (T062). Locks are per `account` and per `tenant-<t>` and exclusive
(`tools/internal/stacks/lock.go`, `LOCK_EX|LOCK_NB`). So any chain holds `account` and every
tenant's lock, and two tenants' partial chains exclude each other (D100's counterpoint).

**Decision.** Reader/writer locks on the same files: `LOCK_SH` for the lock names of instances a
chain only reads, `LOCK_EX` for those it applies or destroys; a name in both is exclusive. A shared
lock still excludes every writer, so T062's race (reading a state while another run writes it)
stays closed. Two partial chains over different tenants' ephemeral stacks then share `account` and
each other's tenant lock, and hold their own exclusively.

**Rejected.** Per-instance lock names: they would change every name the lane and the bootstrap
take, and the tenant-state rule ("an account-tenant stack also takes its tenant's lock",
`lock.go`) would need re-deriving. Dropping the retained locks altogether: reopens T062.

**Counterpoint recorded.** Spec 005 names "concurrent tenant runs" as a trigger of the artefact
transaction (`specs/005-first-landing-zone-slice/spec.md:342`). With FR-007, two runs are
concurrent only over different tenants' ephemeral stacks; every write to a producer (an
`account-governance`, `tenant-state` or `project` apply) is exclusive against every reader, so no
artefact is published while another run consumes it. This spec reads the trigger as "concurrent
runs over one producer's artefact", which the locks still exclude. A reviewer may read it
literally; then FR-007 is the trigger, and the transaction spec is owed before FR-007 is used live.
FR-007 is offline only by default (R2).

## R6 The live window (FR-010, FR-011)
- **Dates.** Voucher expiry 2026-10-24 12:42; live work stops 2026-10-23 23:00 (D117); D118's grant
  ends 2026-10-24 23:59 or below 20 € of credit. D119's "Thursday 23" names a Friday; the date
  governs (spec Clarifications, open with default).
- **Runs planned.** One budget-alert probe (T018), one final chain (T023), cleanup runs as needed
  (T024); a two-tenant chain only if T001 permits (T021). A full chain has cost well under 1 €
  (INBOX spend total, run `20261016T082140Z-1797`, per D119's brainstorm); the five probe runs of
  2026-10-16 cost about nothing (same source). Ceiling for the spec: about 5 € of voucher, 0 € card.
- **Recovery allowance.** A run's recovery allowance is its deadline plus its destroy budget
  (spec 006 FR-002: at least `DefaultDeadline`); a run is not started when that sum would end after
  2026-10-23 23:00.
- **What stays.** The retained stacks of the sandbox (`account-governance`, `demo-state`,
  `demo-dev-project`), the state buckets under Object Lock (governance mode, D104) with their
  versions, the bootstrap's service account and policy, and the trial project itself. Each needs a
  removal path and a monthly cost in `evidence/retained.md` (FR-011). Object Storage and project
  prices: cite the pricing source used, or mark UNVERIFIED.

## R7 Two tenants live (FR-009, gated)
If T001 records "permits" (not expected, R2): add a `lab` tenant with one `dev` environment on ref
`LAB_DEV` to `stacks/deployments.yaml`, reconcile and generate, bind `LZ_PROJECT_ID_LAB_DEV` (the
owner's account file; the coordinator does not edit `~/.config/ovh-lz/`), and run one chain over
both tenants. Isolation is not claimed (KD-1; D107: tags do not scope bucket reads). The account
file edit is the owner's, which is one more reason the default is "refuses".
