# Checks and command contracts: Cleanup-first live slice
Revised: 2026-10-16 (spec-set review round 1)

This spec creates no Task target; it adds test files, one probe-check package and one probe root
(each with its line in `test:live-lane`) and registry keys. The entry admits exactly `lz-offline
--candidate <checkout> -- task <target> [-- <path>]` (`tools/cmd/lz-offline/main.go`), so every
offline control runs as a Task target through it. Live controls are host commands run by the
coordinating session under D118 from the clean live clone at a reviewed SHA on `main`, never
through the entry and never by a subagent.

Outcome words: `pass`, `fail`, `blocked`, `not-run`, `review-required` (constitution V). A refused
precondition, empty discovery or a listing error is never `pass`. "Red" is a behavioural failure on
the unfixed tree; a compile error, a missing symbol or an initialisation failure is not red.

## V-checks

| Check | Requirements | Positive control | Negative controls (each must fail) | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | fake API answering 500, 503, 429, a reset, a body-read reset or a token 5xx once, then 200: check `pass`, one retry line each, attempts recorded; second-page 500 resumes at its cursor with no duplicate; `--cleanup` keeps the first run's whole record under `attempts/1/` | 500 on every attempt reported `pass` or retried more than twice; 403, 404 (absence not admitted), a token 4xx or an undecodable body retried; a wait started past the deadline, after cancellation, or refused by the budget; a chain whose retained reads spent the budget still retrying a listing; a retry that lists a real leftover reported `pass`; the token in a retry line or record; `--cleanup` overwriting `leftovers.json`, `errors.txt` or `listings/`, or an earlier `attempts/<n>/` | PR + `evidence/T003.md`–`T007.md` |
| V002 | FR-003, SC-004 | sandbox manifest with `STATE` = `DEMO_DEV` under the KD-1 flag binds on every verb and in the bootstrap; an unused `LZ_PROJECT_ID_X` with a used id binds | `DEMO_DEV` = `SHOP_DEV`, `DEMO_DEV` = `DEMO_PROD`, `STATE` = a tenant ref without the flag, three refs on one id with the flag: admitted, or refused after any child process (plan, apply, destroy, chain, bootstrap), or the refusal printing the id | PR + `evidence/T008.md`, `T009.md` |
| V003 | FR-005, FR-006, FR-007, FR-008, SC-001, SC-005 | mocked plan of `budget-alert`: one `module.project.ovh_cloud_project_alerting.this[0]` on the run's project, threshold 1, delay 3600; baseline written before the fake's first apply; sweep deleting only the new alert and verifying it gone; read-back recorded; production-entry recovery: lost create answer, no state or inventory, process exit, fresh `--cleanup` deletes only the new alert | plan creating an `ovh_cloud_project`; baseline written after the apply, or an apply after a failed baseline; a probe starting while another run directory exists under `state/probes/`; a sweep deleting a baseline id, deleting without a baseline, or passing with the alert still listed; recovery files removed while unreconciled; read-back mismatch reported `pass`, or a mismatch skipping the destroy or sweep | PR + `evidence/T010.md`–`T016.md` |
| V004 | FR-009 | the two-tenant fixture decodes, generates and orders; binds with distinct ids | the fixture with both tenant refs on one id admitted | PR + `evidence/T017.md` |
| V005 | FR-004, FR-010, FR-011, SC-002, SC-003 | budget-alert run `pass` with the read-back recorded and the alert gone after destroy and sweep; final chain `pass`; every run's leftover check clean or cleaned with its first record kept; `evidence/retained.md` lists only approved retained infrastructure, written before the final chain; spend within the ceiling | (procedure, checked by review of the evidence, not by a sensor) a run from an unreviewed SHA or by a subagent; a run started below 20 € of credit or outside FR-010's dates; a disposable resource on `retained.md`; a later run started while a disposable resource exists; a listing error described as "believed" | `evidence/T001.md`, `T019.md`, `retained.md`, `T022.md`, `T023.md` |
| V006 | FR-012 | `task check:specs` with no argument and `ci-source` read spec 007; `TestTaskfileTracesEverySpec` green | either list omitting spec 007 while the registry holds `007/` keys; `test:foundation-ci` inputs without `specs/007-cleanup-first-live-slice/tasks.md` | `evidence/T002.md`, `T025.md` |

## Security guards with mandatory mutation proof
Each guard: one behavioural mutant that removes or weakens the clause must turn its control red.

| Guard | Where | Mutant |
| --- | --- | --- |
| G24 listing retry classification (FR-001) | `tools/internal/live/leftovers.go`, `chain.go`, `runner.go` | retry every error; retry 4xx (a token 4xx included); ignore the budget, the deadline or cancellation; build a fresh budget for the listings; restart a paginated listing from the first page |
| G25 record kept (FR-002) | `tools/internal/live/sweep.go`, `leftovers.go`, `runner.go` | keep only `summary.json` and `leftovers.json`; write over `attempts/<n>/`; move `baseline.json` or `inventory.jsonl`; drop the attempt list |
| G26 alias refusal (FR-003) | `tools/internal/stacks/adapter.go`, `tools/internal/live/apply.go`, `bootstrap_state.go` | admit tenant/tenant aliasing under the flag; check after the first child; check the bootstrap only at its publish or verify binding; print the id |
| G27 baseline and sweep (FR-006, FR-007) | `tools/internal/live/runner.go`, `leftovers.go`, `tools/internal/probes/live/alerting` | write the baseline after the apply; apply after a failed baseline; let `Before` decide a named kind; start while another run is unreconciled; **delete a baseline id**; delete without a baseline; pass without listing again; remove recovery files before the sweep and leftover check pass |
| G28 read-back (FR-008) | `tools/internal/probes/live/alerting` | record the plan's values instead of the API's; return without error on a mismatch; end the run before the destroy on a mismatch |

## Live run contracts (coordinator, D118; FR-010 is procedure, not code)
| Run | Task | Command | Pass | Stop or cleanup |
| --- | --- | --- | --- | --- |
| budget alert | T019 | `mise exec -- task live:probe -- --reviewed-sha <sha> tests/live/probes/budget-alert` | baseline written; one alert created and read back (id, threshold 1, delay 3600, project); destroy and sweep removed it; the alert listing no longer holds it; leftover check `pass` | not started after 2026-10-21 23:00; leftovers: `task live:probe -- --reviewed-sha <sha> --cleanup <run-id>` until clean |
| final chain | T022 | `mise exec -- task live:chain -- --reviewed-sha <sha> all` | chain `pass` with the sandbox's KD-1 binding admitted (FR-003), retries recorded if any, leftover check `pass` | finishes with its recovery on 2026-10-22; leftovers removed by destroy-on-exit, then by the lane's destroy verb per ephemeral instance |
| cleanup | T023 | per run not clean: a probe with `mise exec -- task live:probe -- --reviewed-sha <sha> --cleanup <run-id>`; a chain's ephemeral instance with the lane's destroy verb | every run of the spec clean; nothing disposable left | 2026-10-23, nothing started whose recovery crosses 23:00 |
