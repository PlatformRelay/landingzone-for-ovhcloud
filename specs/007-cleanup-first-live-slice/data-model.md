# Data model: Cleanup-first live slice
Spec: [spec.md](spec.md) · Research: [research.md](research.md)

Every record here lives in a run directory of the live clone (`.local/live/<run-id>/`) or in the
repository; none holds a credential. Every string written to a record passes the run's redactor.

## Listing attempt (FR-001, FR-002)
One failed request of the leftover check, kept in `leftovers.json` under `attempts`:

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | API path of the request (`/cloud/project/<p>/network/private`), never a query or a token |
| `attempt` | int | 1-based attempt number of that request |
| `status` | int, omitted for a transport failure | HTTP status answered |
| `class` | `status` \| `transport` \| `decode` \| `bounds` | what failed; only `status` 429/5xx and `transport` are retried |
| `retried` | bool | whether a wait and another attempt followed |
| `reason` | string, omitted when retried | why no retry followed: `permanent`, `attempts`, `deadline`, `budget` |

Retry bound: three attempts per request, waits `[5s, 15s]` (`defaultListingBackoff`). A wait is
admitted only if `ctx`'s deadline is after `now + wait` and the check's `retryBudget.admit(now,
wait)` is true (spec 006 data-model *Retry budget*). The retry line on the terminal:
`LZ-LIVE retry listing <path> in <wait>: <status|class> (attempt <n> of 3)`.

## Run record archive (FR-002)
`--cleanup <run-id>` moves `summary.json` and `leftovers.json` of the run directory, when present,
into `attempts/<n>/` (directory 0700, files 0600; `n` = 1 + the highest existing number), then
writes its own. Nothing in `attempts/` is ever rewritten.

## Probe baseline (FR-006)
`baseline.json` in the probe run directory, written before the first `tofu apply`:
`{"run_id": "<run-id>", "project": "<redacted project ref>", "listed": {"ovh_cloud_project_alerting": ["<id>", …]}, "errors": []}`.
A non-empty `errors` ends the run before any create. The project id is written as the run's
project ref, never as the 32-hex id (the docs check refuses 32-hex ids in committed files; run
records follow the same habit).

## Project alias refusal (FR-003)
Condition `project-alias`. Detail: `project refs <A> and <B> resolve to one project in account.env;
give each its own project or drop one row`. Neither the id nor `account.env`'s path is printed.
Admitted alias: `(spec.state.project.ref, tenant-environment ref)` when
`spec.sandbox.shared_state_project` is true. Judged refs: the state ref and every tenant
environment's ref of the manifest.

## Lock request (FR-007)
`{name, shared}`: `name` is `account` or `tenant-<t>` (`lock.go`'s `lockName`); `shared` true for a
name the run only reads. Order: `account` first, then tenants sorted (unchanged). A name requested
both ways is exclusive. Lock files and modes unchanged (`<dir>/<name>.lock`, 0600).

## Two-tenant sandbox fixture (FR-008)
`tests/fixtures/manifests/sandbox-two-tenants/deployments.yaml`: the sandbox manifest plus tenant
`lab` (environment `dev`, ref `LAB_DEV`, `mode: reference`, region `GRA11`, CIDR `10.40.0.0/24`,
VLAN 0), rows `lab-state`, `lab-dev-project`, `lab-dev-gra11-network`, `lab-dev-gra11-runtime`;
`shared_state_project: true`. Accounts used by its tests (in the test, never on disk under
`~/.config`): distinct ids for `STATE`/`DEMO_DEV`/`LAB_DEV` with `STATE` = `DEMO_DEV`, and one with
all three equal.

## Retained-resource list (FR-011)
`evidence/retained.md`, one row per resource: kind, id or name (no 32-hex id: the project as its
ref), owning stack or "bootstrap", why kept, removal path (command or Control Panel step, locked
versions and governance bypass, credential revocation), monthly cost with source or UNVERIFIED,
and the run record it was read from.

## Fixed signatures for the test tasks
A test task writes its tests against these signatures, with a stub whose body returns "not
implemented" (or the zero value plus that error) where the symbol is new; the implementation task
keeps the signature. Existing symbols keep their signature unless a change is listed. Package paths
are under `tools/`.

| Req | Symbol (package) | Signature or contract |
| --- | --- | --- |
| FR-001 | `LeftoverCheck` (`internal/live`) | new fields: `Backoff []time.Duration` (nil: `defaultListingBackoff`; empty non-nil: no retry), `Budget *retryBudget` (nil: deadline only), `Clock waitClock` (nil: wall clock; spec 006's interface), `Term io.Writer` (retry lines; nil: discarded). `Check(ctx, inv) Report` unchanged |
| FR-001 | `defaultListingBackoff`, `transientListing` (`internal/live`) | `var defaultListingBackoff = []time.Duration{5 * time.Second, 15 * time.Second}`; `func transientListing(err error) (class string, retry bool)`: `("status", true)` for `*statusError` with 429 or 500–599, `("status", false)` for other codes, `("transport", true)` for a `*transportError`, `("decode", false)` and `("bounds", false)` otherwise |
| FR-001 | `transportError` (`internal/live`) | `type transportError struct { path string; err error }`; `Error()` is `quiet`'s text for the call; returned by `apiLister.request` for a failed `Do` |
| FR-002 | `ListingAttempt`, `Report.Attempts` (`internal/live`) | `type ListingAttempt struct { Path string \`json:"path"\`; Attempt int \`json:"attempt"\`; Status int \`json:"status,omitempty"\`; Class string \`json:"class"\`; Retried bool \`json:"retried"\`; Reason string \`json:"reason,omitempty"\` }`; `Report` gains `Attempts []ListingAttempt \`json:"attempts"\`` |
| FR-002 | `keepRecord` (`internal/live`) | `func keepRecord(runDir string) (kept string, err error)`: moves `summary.json` and `leftovers.json` into `attempts/<n>/`; returns that directory, or "" when neither file exists |
| FR-003 | `CheckProjectAliases`, `ReasonProjectAlias` (`internal/stacks`) | `const ReasonProjectAlias = "project-alias"`; `func CheckProjectAliases(m *Manifest, a BoundAccount) error`: nil, or `*RefusalError{Reason: ReasonProjectAlias}` whose `Detail` names both refs |
| FR-003 | `CondProjectAlias` (`internal/live`) | `const CondProjectAlias = "project-alias"`; the lane returns `refuse(CondProjectAlias, …)` before its first child process |
| FR-006 | `probeBaseline`, `baselineFile` (`internal/live`) | `func (s *session) probeBaseline(ctx context.Context, check LeftoverCheck) error` writes `baselineFile` (existing name `baseline.json`) in the run directory or returns the listing errors; `LeftoverCheck` gains `Before map[string][]string` (type → ids listed before the run; an id there is never a candidate, an id of a nameless kind absent from it is) |
| FR-007 | `LockStore.TryLockShared`, `LockRequest`, `RunLockModes`, `HoldRunModes` (`internal/stacks`) | `LockStore` gains `TryLockShared(name string) (release func() error, err error)` (`LOCK_SH|LOCK_NB`; `ErrLocked` while an exclusive holder exists); `type LockRequest struct { Name string; Shared bool }`; `func RunLockModes(m *Manifest, write, read []string) ([]LockRequest, error)`; `func HoldRunModes(store LockStore, m *Manifest, write, read []string) (func() error, error)`; `HoldRun(store, m, ids)` stays and equals `HoldRunModes(store, m, ids, nil)` |
| FR-007 | chain locks (`internal/live`) | no new symbol: tests drive `runLane` through `ChainOptions` with the fake runner and a `DirLocks` store in a temporary directory |
| FR-005 | `tests/live/probes/budget-alert` | OpenTofu root: `probe.tf` boilerplate as `alerting` (state path pattern ending `/budget-alert.tfstate`); `main.tf` with `module "project"` (`source = "../../../../modules/cloud-project"`, `mode = "reference"`, `project_id`, `budget_alert = {enabled = true, monthly_threshold = 1, email = data.ovh_me.account.email, delay = 3600}`); outputs `alert_id` and `run_id` |
| FR-008 | fixture | no new symbol: tests use `stacks.DecodeManifest`, `Reconcile`, `Generate` and `Order` as the `growth` fixture's tests do, and `CheckProjectAliases` |
