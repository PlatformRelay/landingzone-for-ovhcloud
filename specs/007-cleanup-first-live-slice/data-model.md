# Data model: Cleanup-first live slice
Spec: [spec.md](spec.md) · Research: [research.md](research.md) · Revised: 2026-10-16 (spec-set review round 1)

Every record here lives in a run directory of the live clone (`.local/live/<run-id>/`), in the
probe's files under `~/.config/ovh-lz/accounts/<account>/state/probes/<run-id>/`, or in the
repository; none holds a credential. Every string written to a record passes the run's redactor.

## Listing attempt (FR-001, FR-002)
One failed request of the leftover check, kept in `leftovers.json` under `attempts`:

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | API path of the request (`/cloud/project/<p>/network/private`, or `token`), never a query or a token value |
| `attempt` | int | 1-based attempt number of that request |
| `status` | int, omitted for a transport failure | HTTP status answered |
| `class` | `status` \| `transport` \| `decode` \| `bounds` | what failed; only `status` 429/5xx and `transport` are retried |
| `retried` | bool | whether a wait and another attempt followed |
| `reason` | string, omitted when retried | why no retry followed: `permanent`, `attempts`, `deadline`, `budget`, `cancelled` |

Retry bound: three attempts per request, waits `[5s, 15s]` (`defaultListingBackoff`). A wait is
admitted only if the context's deadline is after `now + wait` and the shared `retryBudget.admit(now,
wait)` is true. A nil budget admits no wait. The retry line on the terminal:
`LZ-LIVE retry listing <path> in <wait>: <status|class> (attempt <n> of 3)`.

## Run record archive (FR-002)
`lz-live probe --cleanup <run-id>` moves every entry of the run directory except `inventory.jsonl`,
`baseline.json` and `attempts/` (so `summary.json`, `leftovers.json`, `errors.txt`, `listings/`,
`observations.json`, plan files) into `attempts/<n>/` (directory 0700, files 0600; `n` = 1 + the
highest existing number), then writes its own. Nothing in `attempts/` is ever rewritten.

## Probe baseline (FR-006)
`baseline.json` in the probe run directory, written before the first `tofu apply` and never
rewritten by a cleanup:
`{"run_id": "<run-id>", "project_ref": "STATE", "listed": {"ovh_cloud_project_alerting": ["<id>", …]}, "errors": []}`.
Kinds in `listed` are nameless kinds only (`namelessKinds`, today `ovh_cloud_project_alerting`). A
non-empty `errors` ends the run before any create. The project is written as its ref, never as the
32-hex id.

## Unreconciled probe run (FR-006, FR-007)
A directory `accounts/<account>/state/probes/<other-run-id>/` that exists when a probe (not a
`--cleanup`) starts. Refusal condition `unreconciled-run`, detail naming the run id and the next
action `task live:probe -- --reviewed-sha <sha> --cleanup <run-id>`. The directory and its files
(`probe.env`, the passphrase, the root record, state if any) stay until that run's destroy, sweep
and leftover check have all passed.

## Budget-alert probe check (FR-007, FR-008)
- Published output: `alert_id` (the root's output; public, not sensitive).
- Observation (`observations.json`): `{"alert_id": "<id>", "project_ref": "STATE", "monthly_threshold": 1, "delay": 3600, "matches_plan": true}`;
  `matches_plan` false, or a missing alert, makes the observation `fail`.
- Sweep record (terminal and evidence): ids deleted (never a baseline id), ids still listed after the
  delete (must be none).

## Project alias refusal (FR-003)
Condition `project-alias`. Detail: `project refs <A> and <B> resolve to one project in account.env;
give each its own project or drop one row`. Neither the id nor `account.env`'s path is printed.
Admitted alias: `(spec.state.project.ref, tenant-environment ref)` when
`spec.sandbox.shared_state_project` is true. Judged refs: the state ref and every tenant
environment's ref of the manifest.

## Two-tenant sandbox fixture (FR-009)
`tests/fixtures/manifests/sandbox-two-tenants/deployments.yaml`: the sandbox manifest plus tenant
`lab` (environment `dev`, ref `LAB_DEV`, `mode: reference`, region `GRA11`, CIDR `10.40.0.0/24`,
VLAN 0), rows `lab-state`, `lab-dev-project`, `lab-dev-gra11-network`, `lab-dev-gra11-runtime`;
`shared_state_project: true`. Accounts used by its tests are built in the test, never read from
`~/.config`: distinct ids for `DEMO_DEV` and `LAB_DEV` with `STATE` = `DEMO_DEV`, and one with all
three equal.

## Retained-infrastructure list (FR-011)
`evidence/retained.md`, one row per approved retained resource: kind, id or name (no 32-hex id: the
project as its ref), owning stack or "bootstrap", why kept, removal path (command or Control Panel
step, locked versions and governance bypass, credential revocation), monthly cost with source or
UNVERIFIED, and the run record it was read from. A resource created by a probe or by a chain's
ephemeral stack is never a row; its existence is a blocker named at the top of the file until a
clean leftover check shows it gone.

## Fixed signatures for the test tasks
A test task writes its tests against these signatures, with a stub whose body returns "not
implemented" (or the zero value plus that error) where the symbol is new; the implementation task
keeps the signature. Existing symbols keep their signature unless a change is listed. Package paths
are under `tools/`. Spec 006's symbols are those of `origin/spec-006-us2` (`cf5db72`); if its merge
renames one, the implementation task follows the merged name and records it.

| Req | Symbol (package) | Signature or contract |
| --- | --- | --- |
| FR-001 | `waitClock`, `wallClock` (`internal/live`) | `type waitClock interface { Now() time.Time; After(d time.Duration) <-chan time.Time }`; `wallClock` is the production clock (as landed by spec 006) |
| FR-001 | `retryBudget` (`internal/live`, spec 006) | unchanged: `func newRetryBudget(end time.Time, reserve time.Duration) *retryBudget`; `func (b *retryBudget) admit(now time.Time, wait time.Duration) bool`; `func (b *retryBudget) keep(d time.Duration)` |
| FR-001 | `LeftoverCheck` (`internal/live`) | new fields: `Backoff []time.Duration` (nil: `defaultListingBackoff`; empty non-nil: no retry), `Budget *retryBudget` (nil: no wait admitted), `Clock waitClock` (nil: `wallClock{}`), `Term io.Writer` (retry lines; nil: discarded). `Check(ctx, inv) Report` unchanged |
| FR-001 | chain wiring (`internal/live`) | `func (r *laneRunner) leftovers(ctx context.Context, check LeftoverCheck) Report` (spec 006) sets `check.Budget` to the same `*retryBudget` its retained reads use, and `check.Clock` to `r.stateReadClock`; the probe core sets `Budget` to `newRetryBudget(<destroy deadline>, minRetryReserve)` |
| FR-001 | `defaultListingBackoff`, `transientListing`, `transportError` (`internal/live`) | `var defaultListingBackoff = []time.Duration{5 * time.Second, 15 * time.Second}`; `func transientListing(err error) (class string, retry bool)`: `("status", true)` for `*statusError` with 429 or 500–599, `("status", false)` for other codes, `("transport", true)` for `*transportError`, `("decode", false)` and `("bounds", false)` otherwise; `type transportError struct { path string; err error }` returned by `apiLister.request` for a failed `Do`, a failed body read and a transport failure of the token request; a token 5xx is a `*statusError` with path `token` |
| FR-002 | `ListingAttempt`, `Report.Attempts` (`internal/live`) | `type ListingAttempt struct { Path string \`json:"path"\`; Attempt int \`json:"attempt"\`; Status int \`json:"status,omitempty"\`; Class string \`json:"class"\`; Retried bool \`json:"retried"\`; Reason string \`json:"reason,omitempty"\` }`; `Report` gains `Attempts []ListingAttempt \`json:"attempts"\`` |
| FR-002 | `keepRecord` (`internal/live`) | `func keepRecord(runDir string) (kept string, err error)`: moves every entry but `inventory.jsonl`, `baseline.json` and `attempts/` into `attempts/<n>/`; returns that directory, or "" when there was nothing to move |
| FR-003 | `CheckProjectAliases`, `ReasonProjectAlias` (`internal/stacks`) | `const ReasonProjectAlias = "project-alias"`; `func CheckProjectAliases(m *Manifest, a BoundAccount) error`: nil, or `*RefusalError{Reason: ReasonProjectAlias}` whose `Detail` names both refs |
| FR-003 | `CondProjectAlias` (`internal/live`) | `const CondProjectAlias = "project-alias"`; returned by `refuse(CondProjectAlias, …)` from the lane's setup (after `BoundAccountFromEnv`, `apply.go:213`) and from `(*stateRun).setup` (`bootstrap_state.go:172`), before any child process |
| FR-006 | `namelessKinds`, `LeftoverCheck.Before`, `probeBaseline` (`internal/live`) | `var namelessKinds = []string{"ovh_cloud_project_alerting"}`; `LeftoverCheck` gains `Before map[string][]string` (type → ids listed before the run; consulted for `namelessKinds` only: an id there is not a candidate, an id absent is); `func (s *session) probeBaseline(ctx context.Context, check LeftoverCheck) error` writes `baseline.json` or returns the listing errors |
| FR-006 | `CondUnreconciledRun`, `unreconciledRuns` (`internal/live`) | `const CondUnreconciledRun = "unreconciled-run"`; `func unreconciledRuns(probesDir, self string) ([]string, error)`: the run ids of directories under `probesDir` other than `self`, sorted; `Probe.Start` refuses when it is non-empty |
| FR-007 | `APIListAlerts`, `APIGetAlert`, `APIDeleteAlert` (`internal/live`) | `APIOp` constants `"ListAlerts"` (GET `/cloud/project/{project}/alerting`), `"GetAlert"` (GET `…/alerting/{id}`), `"DeleteAlert"` (DELETE `…/alerting/{id}`), v1; `APICall{Op, Project, ID}`; `ID` refused unless it is one path segment |
| FR-007, FR-008 | `alerting.Check`, `alerting.Options` (`internal/probes/live/alerting`, new) | `type Options struct { API live.API; Admin live.Credential; RunDir func(runID string) string }`; `func Check(o Options) *live.ProbeCheck` with `Output: "alert_id"`, `Public: []string{"alert_id"}`, `Observe` (read-back, FR-008), `Sweep` (FR-007: reads `baseline.json` from `RunDir(runID)`, refuses without it, deletes only ids absent from it, lists again) |
| FR-007 | registration (`cmd/lz-live`) | `probeCheck` gains `case "budget-alert": return alerting.Check(alerting.Options{API: api, Admin: admin, RunDir: …})` |
| FR-007 | recovery files (`internal/live`) | no new symbol: `Probe.run` (for `Start` and `Cleanup`) keeps `probe.env`, the passphrase and the root record, with or without state, until the check's `Sweep` and the leftover check both passed (`runner.go:1295-1304` today removes them after an error without state) |
| FR-005 | `tests/live/probes/budget-alert` | OpenTofu root: `.terraform.lock.hcl` (copied from `tests/live/probes/alerting/`), `probe.tf` boilerplate as `alerting` (state path pattern ending `/budget-alert.tfstate`), `main.tf` with `data "ovh_me" "account"` and `module "project"` (`source = "../../../../modules/cloud-project"`, `mode = "reference"`, `project_id`, `budget_alert = {enabled = true, monthly_threshold = 1, email = data.ovh_me.account.email, delay = 3600}`); outputs `alert_id` (`module.project.budget_alert_id`) and `run_id` |
| FR-009 | fixture | no new symbol: tests use `stacks.DecodeManifest`, `Reconcile`, `Generate` and `Order` as the `growth` fixture's tests do, and `CheckProjectAliases` |
