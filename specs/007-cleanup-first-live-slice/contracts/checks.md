# Checks and command contracts: Cleanup-first live slice
This spec creates no Task target; it adds test files, one probe root (with its `lz-check unit` line
in `test:live-lane`) and registry keys. The entry admits exactly `lz-offline --candidate <checkout>
-- task <target> [-- <path>]` (`tools/cmd/lz-offline/main.go`), so every offline control runs as a
Task target through it. Live controls are host commands run by the coordinating session under D118
from the clean live clone at a reviewed SHA on `main`, never through the entry and never by a
subagent.

Outcome words: `pass`, `fail`, `blocked`, `not-run`, `review-required` (constitution V). A refused
precondition, empty discovery or a listing error is never `pass`. "Red" is a behavioural failure on
the unfixed tree; a compile error or a missing symbol is not red.

## V-checks

| Check | Requirements | Positive control | Negative controls (each must fail) | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | fake API answering 500, 503, 429 or a reset once, then 200: check `pass`, one retry line each, attempts recorded; second-page 500 resumes at its cursor with no duplicate; `--cleanup` keeps the first record under `attempts/1/` | 500 on every attempt reported `pass` or retried more than twice; 403, 404 (absence not admitted) or an undecodable body retried; a wait started past the deadline or refused by the budget; retry line or attempt record holding the bearer token; `--cleanup` overwriting `leftovers.json` or an earlier `attempts/<n>/` | PR + `evidence/T003.md`–`T006.md` |
| V002 | FR-003, SC-004 | sandbox manifest with `STATE` = `DEMO_DEV` under the KD-1 flag binds; an unused `LZ_PROJECT_ID_X` with a used id binds | `DEMO_DEV` = `SHOP_DEV`, `DEMO_DEV` = `DEMO_PROD`, `STATE` = a tenant ref without the flag, three refs on one id with the flag: admitted, or refused after a `tofu` call, or the refusal printing the id; the bootstrap state phase binding an aliased account | PR + `evidence/T007.md`, `T008.md` |
| V003 | FR-005, FR-006, SC-001 | mocked plan of `budget-alert`: one `module.project.ovh_cloud_project_alerting.this[0]` on the run's project, threshold 1, delay 3600; baseline written before the fake's first apply; an alert the fake creates without an inventory line found as the run's leftover | plan creating an `ovh_cloud_project`; state path outside `state/probes/<run-id>/budget-alert.tfstate` accepted; baseline written after the apply, or a failed baseline followed by an apply; a pre-existing alert reported or removed | PR + `evidence/T009.md`–`T012.md` |
| V004 | FR-007, FR-008, SC-005 | two shared holders of `account`; two partial chains over `demo` and `lab` ephemeral stacks both hold their locks; the two-tenant fixture decodes, generates and orders | an exclusive holder admitted beside a shared one, or the reverse; a chain taking `account` or another tenant's lock exclusively for a read; an `apply lab-state` admitted while a `demo` chain holds its shared locks; a killed shared holder leaving a lock; the fixture with both tenant refs on one id admitted | PR + `evidence/T013.md`–`T017.md` |
| V005 | FR-004, FR-009, FR-010, FR-011, SC-002, SC-003 | budget-alert run `pass` with the alert read back and gone after destroy; final chain `pass`; every run's leftover check clean or cleaned with its first record kept; `evidence/retained.md` written before the final chain; spend within the ceiling | a run started from a non-`main` or unreviewed SHA, by a subagent, below 20 € of credit, on 2026-10-23 for anything but verification or cleanup, or with a recovery allowance past 23:00; a leftover left without a row in `retained.md`; a listing error described as "believed" | `evidence/T001.md`, `T018.md`, `T020.md`–`T024.md` |
| V006 | FR-012 | `task check:specs` with no argument and `ci-source` read spec 007; `TestTaskfileTracesEverySpec` green | either list omitting spec 007 while the registry holds `007/` keys; `test:foundation-ci` inputs without `specs/007-cleanup-first-live-slice/tasks.md` | `evidence/T002.md`, `T026.md` |

## Security guards with mandatory mutation proof
Each guard: one behavioural mutant that removes or weakens the clause must turn its control red.

| Guard | Where | Mutant |
| --- | --- | --- |
| G24 listing retry classification (FR-001) | `tools/internal/live/leftovers.go` | retry every error; retry 4xx; ignore the budget or the deadline; restart a paginated listing from the first page |
| G25 record kept (FR-002) | `tools/internal/live/sweep.go`, `leftovers.go` | write `leftovers.json` over the old one; reuse `attempts/<n>/`; drop the attempt list |
| G26 alias refusal (FR-003) | `tools/internal/stacks/adapter.go`, `tools/internal/live/apply.go`, `bootstrap_state.go` | admit tenant/tenant aliasing under the flag; call the check after the first child; skip it on the bootstrap path; print the id |
| G27 probe baseline (FR-006) | `tools/internal/live/runner.go`, `leftovers.go` | write the baseline after the apply; apply after a failed baseline; treat a baseline id as a candidate |
| G28 shared locks (FR-007) | `tools/internal/stacks/lock.go`, `tools/internal/live/chain.go` | take `LOCK_SH` for a written instance; admit `LOCK_EX` beside a shared holder; drop the retained instances from the read set |

## Live run contracts (coordinator, D118)
| Run | Task | Command | Pass | Stop or cleanup |
| --- | --- | --- | --- | --- |
| budget alert | T018 | `mise exec -- task live:probe -- --reviewed-sha <sha> tests/live/probes/budget-alert` | apply created one alert (id recorded, threshold and delay read back); destroy removed it; the alert listing no longer holds it; leftover check `pass` | destroy in `finally`; leftovers: `task live:probe -- --reviewed-sha <sha> --cleanup <run-id>` |
| two tenants (gated) | T021 | `mise exec -- task live:chain -- --reviewed-sha <sha> all` with the two-tenant manifest | both tenants' ephemeral stacks applied and destroyed; leftover check `pass`; KD-1 reported `known-deviation` | only on T001 "permits"; else not run |
| final chain | T023 | `mise exec -- task live:chain -- --reviewed-sha <sha> all` | chain `pass` with the sandbox's KD-1 binding admitted (FR-003), retries recorded if any, leftover check `pass` | finishes with its recovery on 2026-10-22 |
| cleanup | T024 | per run not clean: a probe with `mise exec -- task live:probe -- --reviewed-sha <sha> --cleanup <run-id>`; a chain's ephemeral instance with `mise exec -- task live:destroy -- --reviewed-sha <sha> <instance>` | every run of the spec clean or listed in `retained.md` | 2026-10-23, nothing started whose recovery crosses 23:00 |
