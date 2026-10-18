# Tasks: 007-cleanup-first-live-slice
Status: planned · Created: 2026-10-16 · Revised: 2026-10-16 (spec-set review round 1: orphan-proof cleanup, A24 deferred, B5 offline; round 2: start-up phases, baseline authority, output shape, observations kept, record split) · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md

All boxes are open. This spec creates no Task target; the probe root `tests/live/probes/budget-alert`
and the package `tools/internal/probes/live/alerting` are **planned** until T016 and T015 close.

**Test tasks and implementation tasks.** Every behaviour story lists its test tasks before its
implementation tasks. A test task names its cases as given / when / then, each with the defect it
would expose; a fresh session writes the tests from spec.md, data-model.md's *Fixed signatures* and
the task text (not from plan.md's rationale), against a stub whose body returns "not implemented"
where the symbol is new, and closes on a verified red: each new test fails for its stated reason. A
compile error, a missing symbol, a missing binary or an initialisation failure is not that red.
Cases marked *pin* hold today (or hold by construction of the stub) and pass from the start; every
other case must fail for its stated reason. The tests a test task writes are fixed points for the
implementation task that follows; that task closes when they pass.

**Red windows.** A story's tests stay red on their target until the story's last implementation
task closes; nothing is pushed inside a red window. Stories sharing `test:live-lane` run in order
(US1 → US2 → US3).

**Commands.** The entry admits only `lz-offline --candidate <checkout> -- task <target> [-- <path>]`,
so every offline Verify names an existing Task target and runs it through the entry
(`<approved-absolute-path>/lz-offline --candidate <checkout> -- task <target>`, from `$HOME`).
**Live tasks (T019, T022, T023) are run by the coordinator under D118**: the coordinating session,
never a subagent, on the maintainer's workstation, from the clean live clone at a reviewed SHA on
`main`, after the code it runs has passed its independent review; never through the entry. FR-010's
limits are **coordinator procedure, not code**: before each live run the coordinator checks the
remaining credit by hand (Control Panel or `ovhcloud`) and stops below 20 €; T019 is dropped if not
started by 2026-10-21 23:00; T022 finishes with its recovery on 2026-10-22; on 2026-10-23 only
verification and cleanup start, none whose recovery allowance (deadline plus destroy budget) would
end after 23:00; the spec's live spend stays within about 5 € of voucher and 0 € card. Every resource
a live run creates carries the run id in its name or tag, or is found through the pre-apply baseline
(FR-006). Anything a disposable run created and did not remove blocks every later feature run and
this spec's completion until a clean leftover check shows it gone; it never goes on
`evidence/retained.md`.
Mutation proof is required where a Verify names a guard G24–G28 (`contracts/checks.md`).
Evidence: the Verify output is summarised in the PR description; `evidence/<TID>.md` in this
directory points at the PR and names the red and green commits, or, for a live task, the run id,
SHA, outcome, retries seen, approximate cost and the credit left.

## Phase 1: Setup

- [x] T001 Establish whether A17 refuses two tenants on one project and record the rule and its consequence in specs/007-cleanup-first-live-slice/research.md (R2) — closed 2026-10-16, evidence: evidence/T001.md
  - Requirements: FR-004; ADRs: 0009, 0021. Depends on: none. First task.
  - Scope: read `tools/internal/stacks/manifest.go` (project-ref ownership, `DUPLICATE_NAME`; the `shared_state_project` rule), `tools/internal/live/publish.go` (`BoundAccountFromEnv`) and its callers, `tools/internal/live/bootstrap_state.go` (`setup`), T098's decision request, inventory Q8 and D119's conditions; decide whether a second tenant on the sandbox's one project is possible under FR-003 as written. Fill R2's *Outcome* with `refuses` or `permits` and the lines that decide it. On `permits`, file the owner decision FR-004 names; do not change FR-003 to reach `permits`.
  - Verify: `task check:specs -- specs/007-cleanup-first-live-slice`: `TRACE_OK`; R2 has an outcome with cited lines.
  - Evidence: `evidence/T001.md`; initial status `not-run`.

- [x] T002 Verify the trace of spec 007 in Taskfile.yml and tools/harness/checks.yaml (written in the spec-set commit) — closed 2026-10-16, evidence: evidence/T002.md
  - Requirements: FR-012; ADRs: 0008, 0019. Depends on: none.
  - Verify: `task test:traceability; task test:foundation-ci; task check:specs`: `TestTaskfileTracesEverySpec` green; `task check:specs` with no argument prints `TRACE_OK` for 001, 005, 006 and 007; `ci-source` reads spec 007; `test:foundation-ci`'s inputs name `specs/007-cleanup-first-live-slice/tasks.md`.
  - Evidence: `evidence/T002.md`; initial status `not-run`.

## Phase 2: User Story 1 — A transient listing error does not fail the cleanup (P1)
Independent test: V001. Test tasks T003–T005, then implementation tasks T006–T007. Spec 006's T022–T026
(PR #123) are on `main` (`15bc579`); the signatures of data-model.md are those of `main`.

- [x] T003 [US1] Write listing-retry tests in tools/internal/live/leftovers_retry_test.go — closed 2026-10-16, evidence: evidence/T003.md
  - Requirements: FR-001, SC-001; ADRs: 0008, 0024. Depends on: T002.
  - Cases (given / when / then → defect exposed), fake `Lister` or fake HTTP server scripted per path and attempt, `Backoff` of 1 ms, a fake `waitClock`, a generous `retryBudget`:
    - given `/cloud/project/<p>/network/private` answering 500 then 200 / when `Check` runs / then `pass`, one line `LZ-LIVE retry listing … 500 (attempt 2 of 3)` on `Term`, and one `ListingAttempt{Status: 500, Retried: true}` → exposes today's single attempt (T027's `fail`).
    - given 503, 429 and a reset while sending on three listings, each once / when `Check` runs / then `pass` with three retries → exposes a classification that retries only 500.
    - given an HTTP server that sends headers and resets the connection mid-body once / when `Check` runs / then the failure is `transport` and retried → exposes a body-read reset classed `decode`.
    - given the token endpoint answering 503 once / when the first listing needs the token / then the token request is retried and the check passes; given it answering 401 / then no retry, and every listing fails with the cached refusal → exposes a token 5xx cached for the whole check, or a token 4xx retried.
    - given page 2 of a paginated listing answering 500 once / when `Check` runs / then the retry sends page 2's cursor and the listing holds each item once → exposes a restart from page 1 or a dropped cursor.
    - given a single-object read (a region detail) answering 502 once / when `Check` runs / then `pass` → exposes retries on array listings only.
    - given 500 on every attempt / when `Check` runs / then `fail`, the error names the path, 500 and "attempt 3 of 3", and three attempts are recorded, the last with `reason: attempts` → exposes an unbounded retry or a `pass` on exhaustion.
    - given a 500 once and then a 200 that lists a network named with the run id / when `Check` runs / then `fail` with that leftover → exposes a retry path that skips reconciliation.
    - given 403, given 404 where absence is not admitted, given a 200 whose body is not JSON / when `Check` runs / then one attempt each and `fail` → *pin*; the same case's `reason: permanent` record row is red until T006 → exposes a refusal recorded as retryable.
    - given a check with two failed attempts / when `leftovers.json` is written / then it holds an `attempts` array with path, attempt, status, class, retried and reason, and no token → exposes attempts kept only in memory.
    - given a context deadline 3 ms away and `Backoff` of 10 ms / when a 500 arrives / then no wait starts and `reason: deadline` → exposes a wait past the deadline.
    - given a `retryBudget` that refuses the wait, and given `Budget` nil / when a 500 arrives / then no wait starts, `reason: budget` → exposes listing waits outside the shared budget, or nil read as "deadline only".
    - given a wait in progress / when the context is cancelled / then the wait ends at once with the context's cause, `reason: cancelled`, and no further request is sent → exposes a sleep that ignores cancellation.
    - given `Red` holding the bearer token and a transport error quoting it / when the retry line and the record are written / then neither contains it → exposes an unredacted retry line.
  - Verify: `task test:live-lane`: every case except the *pin* fails against the stub `transientListing` and the unchanged lister, for its stated reason.
  - Evidence: `evidence/T003.md`; initial status `not-run`.

- [x] T004 [US1] Write the shared-budget wiring tests in tools/internal/live/chain_retry_budget_test.go — closed 2026-10-16, evidence: evidence/T004.md
  - Requirements: FR-001, SC-001; ADRs: 0008, 0024. Depends on: T002.
  - Cases (given / when / then → defect exposed), the chain through `ChainOptions` with fake children, fake API and a fake `waitClock`:
    - given a destroy deadline whose budget the retained-state reads spend on their retries (spec 006's case) / when the next listing answers 500 once / then it is not retried (`reason: budget`) and the report is not `pass` → exposes the listing check built with a fresh or nil-as-unbounded budget.
    - given the same chain with a budget left after the retained reads / when a listing answers 500 once / then it is retried and the report is `pass` → exposes a listing check that never receives the budget.
    - given a probe run through `Probe.Start` with fakes / when its leftover listing answers 500 once / then it is retried within a budget ending at the destroy context's deadline → exposes a probe core that passes no budget.
  - Verify: `task test:live-lane`: the three cases fail on today's code.
  - Evidence: `evidence/T004.md`; initial status `not-run`.

- [x] T005 [US1] Write record-keeping tests in tools/internal/live/sweep_record_test.go and tools/cmd/lz-live/probe_cleanup_record_test.go — closed 2026-10-16, evidence: evidence/T005.md
  - Requirements: FR-002; ADRs: 0008, 0024. Depends on: T002.
  - Cases (given / when / then → defect exposed):
    - given a probe run directory with `summary.json`, `leftovers.json`, `errors.txt`, `listings/network.json`, `inventory.jsonl` and `baseline.json` / when `--cleanup <run-id>` runs against fakes / then every file but `inventory.jsonl`, `baseline.json` and `observations.json` is under `attempts/1/` unchanged, a copy of `observations.json` is there too, those three stay in place, and new records are written beside → exposes the overwrite that lost T027/T028's error text, including `errors.txt` and the listings.
    - given `attempts/1/` already present / when a second cleanup runs / then the old record goes to `attempts/2/` and `attempts/1/` is unchanged → exposes reuse of an archive slot.
    - given a run directory with only `inventory.jsonl` / when cleanup runs / then it proceeds and `keepRecord` returns "" → exposes a cleanup that fails on absence.
    - given a `storage-iam` probe run whose `observations.json` records a policy id that the policy listing omits (a lagging listing) / when two `--cleanup <run-id>` runs follow in fresh processes, the first failing its leftover check / then the second cleanup's undo still receives the recorded policy id through `Recorded` and deletes that policy by id → exposes archiving that hides `observations.json` from `session.undo` (`runner.go:786-794`; `storageiam.go:181,532`).
  - Verify: `task test:live-lane`: all four cases fail against the stub `keepRecord`.
  - Evidence: `evidence/T005.md`; initial status `not-run`.

- [x] T006 [US1] Retry transient listing errors and record each failed attempt in tools/internal/live/leftovers.go and pass the shared budget from tools/internal/live/chain.go and tools/internal/live/runner.go — closed 2026-10-16, evidence: evidence/T006.md
  - Requirements: FR-001, SC-001; ADRs: 0008, 0024. Depends on: T003, T004.
  - Scope: `transientListing`, `transportError` (send, body read, token request), `defaultListingBackoff`, the `LeftoverCheck` fields and `waitClock` of data-model.md; `ListingAttempt` and `Report.Attempts`, written into `leftovers.json`; the retry in `lister.pages`, `lister.objectRaw` and the token request; the chain hands its reconciliation budget and clock to the check; the probe core builds its budget from the destroy deadline.
  - Verify: `task test:live-lane`: T003's cases (record rows included) and T004's cases green; G24 mutants (retry every error; retry 4xx; ignore budget, deadline or cancellation; fresh budget for listings; restart from page 1) each turn a case red.
  - Evidence: `evidence/T006.md`; initial status `not-run`.

- [x] T007 [US1] Archive earlier records across cleanups in tools/internal/live/sweep.go and tools/internal/live/runner.go — closed 2026-10-16, evidence: evidence/T007.md
  - Requirements: FR-002; ADRs: 0008, 0024. Depends on: T005, T006.
  - Verify: `task test:live-lane`: T005's cases green; G25 mutants (two files only, slot reuse, baseline, inventory or observations moved, attempts dropped) each turn a case red.
  - Evidence: `evidence/T007.md`; initial status `not-run`.

## Phase 3: User Story 2 — Two refs on one project are refused before anything runs (P1)
Independent test: V002. Test task T008, then implementation task T009.

- [x] T008 [US2] Write project-alias tests in tools/internal/stacks/alias_test.go, tools/internal/live/alias_lane_test.go and tools/internal/live/bootstrap_alias_test.go — closed 2026-10-16, evidence: evidence/T008.md
  - Requirements: FR-003, SC-004; ADRs: 0009, 0021. Depends on: T007.
  - Scope: a lane test that reads a manifest fixture adds that fixture to `test:live-lane`'s registry inputs in tools/harness/checks.yaml.
  - Cases (given / when / then → defect exposed), ids built in the test, never read from disk:
    - given the `growth` manifest and `DEMO_DEV` = `SHOP_DEV` / when `CheckProjectAliases` runs / then `*RefusalError{Reason: "project-alias"}` naming both refs and not the id → exposes T098's hole (two tenants on one project pass).
    - given `DEMO_DEV` = `DEMO_PROD` / when it runs / then refused → exposes a check limited to tenants (VLAN collisions across one tenant's environments, T098).
    - given the sandbox manifest (flag true) with `STATE` = `DEMO_DEV` / when it runs / then nil → *pin* for the KD-1 binding the sandbox runs on.
    - given the flag false and different refs on one id, one of them `STATE` / when it runs / then refused naming `STATE` → exposes id-level sharing the decoder's ref check (`manifest.go:583-590`) cannot see.
    - given the flag true and `STATE` = `DEMO_DEV` = `SHOP_DEV` / when it runs / then refused naming `DEMO_DEV` and `SHOP_DEV` → exposes the flag widening to tenant/tenant sharing (D119: never weaken A17).
    - given the flag true, a manifest where a second tenant environment (`SHOP_DEV`) itself owns the `STATE` ref, and that ref equal to `DEMO_DEV` / when it runs / then refused naming the two tenant environments → exposes an exemption keyed on the `STATE` name that lets a second tenant onto the one project (T001, research R2 *Consequence*).
    - given an `LZ_PROJECT_ID_SPARE` the manifest never uses, equal to `DEMO_DEV` / when it runs / then nil → *pin* against judging refs outside the manifest.
    - table over `plan`, `apply`, `destroy`, `chain` (lane, fake children counting every process) and the bootstrap's `Rest` (fake children and fake API): given an aliased account / when each runs / then it ends with `project-alias`, the fake recorded **zero** child processes, and stderr holds no 32-hex id → exposes a check placed after init, at the bootstrap's publish or verify binding (after `state` ran `tofu`), or a refusal printing the id.
    - the same table with the sandbox's KD-1 account / then no `project-alias` refusal → *pin* that the check does not block the sandbox.
  - Verify: `task test:stacks; task test:live-lane; task test:bootstrap`: every case except the *pins* fails against the stub `CheckProjectAliases`.
  - Evidence: `evidence/T008.md`; initial status `not-run`.

- [x] T009 [US2] Refuse aliased project refs in tools/internal/stacks/adapter.go, in the lane's setup in tools/internal/live/apply.go and in `(*stateRun).setup` in tools/internal/live/bootstrap_state.go — closed 2026-10-16, evidence: evidence/T009.md
  - Requirements: FR-003, SC-004; ADRs: 0009, 0021. Depends on: T008.
  - Verify: `task test:stacks; task test:live-lane; task test:bootstrap`: T008's cases green; G26 mutants (tenant/tenant under the flag, check after the first child, bootstrap checked only at publish or verify, id printed) each turn a case red.
  - Evidence: `evidence/T009.md`; initial status `not-run`.

## Phase 4: User Story 3 — The landing zone's budget alert is shown live and leaves nothing (P2)
Independent test: V003 offline, V005 live (T019). Test tasks T010–T013, then implementation tasks
T014–T016.

- [x] T010 [US3] Write the budget-alert root's scaffolding and tests in tests/live/probes/budget-alert/ (`.terraform.lock.hcl`, `probe.tf`, a stub `main.tf`, `probe.tftest.hcl`) with its `lz-check unit` line in Taskfile.yml — closed 2026-10-18, evidence: evidence/T010.md
  - Requirements: FR-005; ADRs: 0006, 0024. Depends on: T009.
  - Scope: copy the `alerting` root's `.terraform.lock.hcl` (the module uses only `ovh/ovh` 2.21.0) and `probe.tf` (state path pattern ending `/budget-alert.tfstate`); the stub `main.tf` holds `data "ovh_me" "account"` and `module "project"` in `reference` mode with `budget_alert = { enabled = false }` and outputs `alert_id` (null), `budget_alert_check` (`{ alert_id = "" }`) and `run_id`, so init and plan succeed offline (premise P2, observed through the entry and recorded); add `go -C tools run ./cmd/lz-check -root .. unit tests/live/probes/budget-alert` to `test:live-lane`, and `tests/live/probes/budget-alert` and `modules/cloud-project` to `test:live-lane`'s registry inputs in tools/harness/checks.yaml.
  - Cases (given / when / then → defect exposed), mocked provider as the `alerting` root's tests:
    - given the run's `project_id` / when planned / then exactly one `module.project.ovh_cloud_project_alerting.this[0]` with that `service_name`, `monthly_threshold` 1, `delay` 3600 and the mocked `ovh_me` address → exposes a root that does not reach the module's alert path (red on the stub).
    - given the plan / when outputs are read / then `alert_id` is the module's `budget_alert_id` (scalar) and `budget_alert_check` is a map of strings whose `alert_id` is the same value → exposes a check output the runner cannot decode (`runner.go:721-741` refuses a scalar) or that names another id (red on the stub).
    - given the plan / when its resources are listed / then no `ovh_cloud_project` is planned → *pin* (reference mode).
    - given a `state_path` outside `…/state/probes/<run-id>/budget-alert.tfstate` / when planned / then validation fails → *pin* (copied boilerplate).
    - given two mocked projects / when planned / then the module reads `project_id`'s project → *pin*.
  - Verify: `task test:live-lane`: the root initialises from the entry's mirror with its lock file (no initialisation failure); the first two cases fail on the stub for the stated reason; the three *pins* pass.
  - Evidence: `evidence/T010.md`; initial status `not-run`.

- [ ] T011 [US3] Write probe-baseline, start-up-phase and unreconciled-run tests in tools/internal/live/probe_baseline_test.go
  - Requirements: FR-006; ADRs: 0008, 0024. Depends on: T009.
  - Cases (given / when / then → defect exposed), fake API and fake `tofu`, `LoadBaseline` and `ClassifyRun` as data-model.md fixes them:
    - given the fake `tofu` checking, when `apply` starts, for `.local/live/<run-id>/baseline.json` and `state/baselines/<run-id>.json` / when a probe runs / then both exist, are synced (written through a temporary file and renamed), name the run and the project digest, and list the alerts with `complete: true` → exposes a baseline written after the first create, in one place only, or under `state/probes/<run-id>/`.
    - given a probe run that passes / when it ends / then `state/probes/` holds no directory for it and the recovery copy is gone, and a second `Start` is not refused → exposes a run that blocks the next one (an extra file making `os.Remove` of its directory fail).
    - given the alert listing answering 500 on every attempt / when a probe runs / then no `tofu apply` is called, the baseline is written with `complete: false` and its errors, the run reports the baseline error and retires its own files, and the next `Start` is not refused → exposes creating without a baseline, or a failed baseline that blocks every later probe.
    - given alert A in the baseline and alert B listed after the run with no inventory line / when the leftover check runs / then B is the run's leftover and A is not → exposes a nameless alert lost when its creation outlives the apply's error.
    - given a bucket named `lzprobe-<run-id>-x` that is also in `Before` / when the leftover check runs / then it is still a leftover → exposes `Before` deciding a named kind.
    - given a chain's leftover check with `Before` nil and a pre-existing alert C that is in no inventory and holds no run id / when it runs / then C is not a leftover, and an alert in the chain's inventory still is → exposes a nil `Before` that makes every alert a candidate.
    - table over `ClassifyRun`: no state, no inventory, no baseline → pre-create; no state, no inventory, a baseline with `complete: false` → pre-create; no state, no inventory, a valid baseline → baseline-backed; state present, no baseline → untrusted; an inventory entry, no baseline → untrusted; a baseline present but unreadable → untrusted; a baseline with another `run_id`, another project digest, no alert key, `complete: false` beside state, or non-empty errors beside state → untrusted → exposes missing state alone taken as proof that nothing was created, or a baseline accepted on readability alone.
    - given the record copy missing and the recovery copy valid / when `LoadBaseline` runs / then the recovery copy is used; both missing → `BASELINE_MISSING` → exposes a single point of loss.
    - given another run's directory under `state/probes/` / when a probe starts / then it is refused `unreconciled-run` naming that run, before any file of the new run is written → exposes an earlier orphan entering a later baseline unseen.
    - given no other directory / when a probe starts / then it proceeds → *pin* against a refusal of the run's own directory.
    - given a baseline / when it is written / then it holds the project's SHA-256 digest, never the 32-hex id → exposes an id in the run record.
  - Verify: `task test:live-lane`: every case except the *pin* fails against the stubs `probeBaseline`, `LoadBaseline`, `ClassifyRun` and `unreconciledRuns`.
  - Evidence: `evidence/T011.md`; initial status `not-run`.

- [ ] T012 [US3] Write the budget-alert probe check's tests in tools/internal/probes/live/alerting/check_test.go, with the stub package and its line in Taskfile.yml's `test:live-lane`
  - Requirements: FR-007, FR-008; ADRs: 0006, 0008. Depends on: T009.
  - Scope: stub package `tools/internal/probes/live/alerting` with `Check` and `Options` as data-model.md fixes them; stub `APIListAlerts`, `APIGetAlert`, `APIDeleteAlert` in tools/internal/live/apicheck.go; add `./internal/probes/live/alerting` to both `go test` commands of `test:live-lane`, and `tools/internal/probes/live/alerting` to `live:probe`'s registry inputs.
  - Cases (given / when / then → defect exposed), fake API recording every call:
    - given a baseline holding A and the API listing A and B / when `Sweep` runs / then it sends `DELETE …/alerting/B` only, lists again, and passes when B is gone → exposes no way to delete an alert (`apicheck.go` has none).
    - given the same / when the second listing still shows B / then `Sweep` fails naming B → exposes a sweep that trusts the DELETE's answer.
    - given a baseline holding A / when the API lists only A / then no DELETE is sent and `Sweep` passes → exposes a sweep that **deletes a baseline id** (G27).
    - given a baseline holding A and the API listing A and C, where C reads threshold 50, delay 86400 or another address (an alert the owner set later) / when `Sweep` runs / then no DELETE is sent, C is reported and `Sweep` fails → exposes a late cleanup removing the owner's real alert.
    - given an untrusted run (a baseline that is unreadable, of another run or project, without the alert key, or a missing baseline beside state) / when `Sweep` runs / then it fails with `BASELINE_INVALID` or `BASELINE_MISSING` and sends no DELETE → exposes deleting on a baseline that proves nothing.
    - given a pre-create run / when `Sweep` runs / then it passes and sends no request → exposes a pre-create run that can never be retired.
    - given an alert id that is not one path segment (`../x`) / when a call is built / then it is refused before any request → exposes path injection through a listed id.
    - given the check output `budget_alert_check` holding `alert_id`, and the API reading back threshold 1, delay 3600 on the run's project / when `Observe` runs / then it records id, threshold, delay and the project digest, `matches_plan` true → exposes an observer that records the plan instead of the API (G28).
    - given a read-back with threshold 2, given delay 7200, given a 404 / when `Observe` runs / then it returns an error naming the mismatch → exposes a mismatch reported `pass`.
    - given `probeCheck("budget-alert", …)` in tools/cmd/lz-live / when called / then it returns a check with a `Sweep` and `Output` `budget_alert_check` → exposes the root running without a sweep (`probe.go:33-47` returns nil today) or selecting the scalar output.
  - Verify: `task test:live-lane`: every case fails against the stubs for its stated reason.
  - Evidence: `evidence/T012.md`; initial status `not-run`.

- [ ] T013 [US3] Write the production-entry recovery and start-up tests in tools/cmd/lz-live/probe_recovery_test.go
  - Requirements: FR-007, SC-005; ADRs: 0008, 0024. Depends on: T011, T012.
  - Cases (given / when / then → defect exposed), `lz-live probe` run as a fresh process of the test binary (as spec 003 T037), fake API and fake `tofu`:
    - given a fake `tofu apply` that makes the fake API create alert B and then exits non-zero before writing any state, so no state and no inventory exist / when the first process ends (its exit, then a kill of a second variant before its own cleanup) / then `probe.env`, the passphrase, the root record and `baseline.json` are still on disk → exposes `runner.go:1295-1304` removing the recovery files after an error without state.
    - given that disk / when a fresh process runs `lz-live probe --reviewed-sha <sha> --cleanup <run-id>` / then it reloads the original baseline, deletes only B, lists again, finds B gone, leaves alert A (in the baseline) untouched, passes its leftover check, and only then removes the run's files → exposes a cleanup that cannot reach a nameless orphan, or deletes a baseline alert.
    - given the cleanup's leftover check failing / when it ends / then the files stay and the error names `--cleanup <run-id>` again → exposes files removed before reconciliation succeeds.
    - given a fake whose create works and a root publishing the real output shape (`alert_id` scalar and `budget_alert_check` map, as the fake `tofu output -json` prints it) / when a probe runs to the end / then `Observe` records the read-back, destroy and sweep pass, and the run passes → exposes a check output the runner cannot decode on the success path.
    - table: a kill (SIGKILL of the fresh process) after `probe.env`, after the passphrase, after the record copy of the baseline, and after both copies / when the next `Start` runs / then it is refused `unreconciled-run`; when `--cleanup <run-id>` then runs / then the first three are retired as pre-create (no passphrase read, no `tofu` call), the fourth is swept as baseline-backed with nothing to delete, and a following `Start` is not refused → exposes a start-up window that blocks every later probe, or a cleanup that needs the passphrase of a run that never applied.
    - given a passing `--cleanup` / when the next `Start` runs / then it is not refused → exposes a cleanup that leaves its directory or the recovery copy.
    - given both baseline copies deleted after a lost create (state absent, inventory absent) / when `--cleanup <run-id>` runs / then it is refused as untrusted and deletes nothing; when `--cleanup <run-id> --accept-lost-baseline` runs without a terminal / then it is refused; with a pseudo-terminal and the run id typed back / then it deletes nothing, runs the leftover check and retires the files only when it passes → exposes a baseline rebuilt from a later listing, or an owner-only retirement an automated session can run.
  - Verify: `task test:live-lane`: the cases fail on today's runner (no sweep, no phases, files removed).
  - Evidence: `evidence/T013.md`; initial status `not-run`.

- [ ] T014 [US3] Write the start-up phases, both baseline copies, run classes and the unreconciled-run refusal in tools/internal/live/runner.go and tools/internal/live/leftovers.go, keeping recovery files until reconciliation
  - Requirements: FR-006, FR-007; ADRs: 0008, 0024. Depends on: T011, T013.
  - Verify: `task test:live-lane`: T011's cases and T013's recovery-file and start-up cases green; G27 mutants (baseline after apply, apply after a failed baseline, `Before` deciding a named kind, nil `Before` making every alert a candidate, start beside an unreconciled run, missing state alone classed pre-create, files removed before reconciliation) each turn a case red.
  - Evidence: `evidence/T014.md`; initial status `not-run`.

- [ ] T015 [US3] Add the alert API operations to tools/internal/live/apicheck.go, the budget-alert probe check to tools/internal/probes/live/alerting/check.go, and its registration and `--accept-lost-baseline` to tools/cmd/lz-live/probe.go
  - Requirements: FR-007, FR-008, SC-005; ADRs: 0006, 0024. Depends on: T012, T014.
  - Verify: `task test:live-lane`: T012's and T013's cases green; the G27 mutants deleting a baseline id, deleting a non-matching alert and accepting another run's baseline, and the G28 mutants, each turn a case red.
  - Evidence: `evidence/T015.md`; initial status `not-run`.

- [ ] T016 [US3] Write the budget-alert root in tests/live/probes/budget-alert/main.tf and its run-sheet row in tests/live/probes/README.md
  - Requirements: FR-005; ADRs: 0006, 0024. Depends on: T010, T015.
  - Scope: `budget_alert` enabled as data-model.md fixes it; `alert_id` from `module.project.budget_alert_id` and `budget_alert_check = { alert_id = module.project.budget_alert_id }`; the run-sheet row also gives the owner recovery of FR-007; the run-sheet row (command, expected, refuted-if, cleanup) and the nameless-kind note pointing at the baseline and the sweep. No inline-resource variant.
  - Verify: `task test:live-lane; task docs:check`: T010's cases green; the run sheet passes the docs rules.
  - Evidence: `evidence/T016.md`; initial status `not-run`.

## Phase 5: User Story 4 — A second tenant, proven offline (P3)
Independent test: V004.

- [ ] T017 [US4] Write the two-tenant sandbox fixture tests/fixtures/manifests/sandbox-two-tenants/deployments.yaml and its tests in tools/internal/stacks/two_tenant_test.go
  - Requirements: FR-009; ADRs: 0004, 0005. Depends on: T001, T009.
  - Cases (given / when / then → defect exposed), fixture as data-model.md fixes it:
    - given the fixture / when decoded, reconciled and generated into a temporary tree / then `lab`'s four stacks exist beside `demo`'s, each with its own state bucket and deployer authority → exposes a generator that assumes one tenant (*pin* if it already holds; recorded either way).
    - given the fixture / when `Order` runs / then each tenant's stacks follow its own `tenant-state` and `account-governance` → exposes cross-tenant ordering (*pin* if it holds).
    - given distinct ids for `DEMO_DEV` and `LAB_DEV` / when `CheckProjectAliases` runs / then nil; given `LAB_DEV` = `DEMO_DEV` (the one sandbox project) / then `project-alias` → *pin* after T009: B5 on one project is refused.
  - Verify: `task test:stacks`: results recorded per case; any red case is a defect and gets its own task before this spec closes.
  - Evidence: `evidence/T017.md`; initial status `not-run`.

## Phase 6: Live window (US3 live, US5)
Independent test: V005. Every live task here is run by the coordinator under D118 (see *Commands*).

- [ ] T018 Record the live-window plan in specs/007-cleanup-first-live-slice/evidence/window.md before the first live run
  - Requirements: FR-010; ADRs: 0019, 0024. Depends on: T001.
  - Scope: the planned runs, their order, the cut-offs (T019 not after 2026-10-21 23:00; T022 finished on 2026-10-22; 2026-10-23 verification and cleanup only), the credit floor and the ceiling, each marked "coordinator procedure, no sensor".
  - Verify: exempt — docs-only; the file states every limit of FR-010 and names who checks it.
  - Evidence: `evidence/window.md`; initial status `not-run`.

- [ ] T019 [US3] Live: the budget-alert probe with tests/live/probes/budget-alert/ — run by the coordinator under D118
  - Requirements: FR-005, FR-006, FR-007, FR-008, FR-010, SC-002, SC-003; ADRs: 0006, 0024. Depends on: T006, T007, T009, T014, T015, T016 on `main` at a reviewed SHA, each reviewed by an independent leg; T018. Not started after 2026-10-21 23:00; then it is recorded as dropped and FR-005 as not qualified.
  - Verify: `mise exec -- task live:probe -- --reviewed-sha <sha> tests/live/probes/budget-alert` from the clean live clone: no unreconciled run refused it; `baseline.json` written before the apply; one alert created and read back (id, threshold 1, delay 3600, project); destroy and sweep removed it and the alert listing no longer holds it; leftover check `pass`, or `fail` followed by `--cleanup <run-id>` until clean, with the first record kept under `attempts/1/`; every retry line quoted; the run's cost and the credit left. P2 and P3 recorded as observed or refuted.
  - Evidence: `evidence/T019.md`; initial status `not-run`.

- [ ] T020 [US3] Record the retry, the alias refusal and the budget-alert run in docs/how-to/run-the-first-slice.md and docs/reference/verification-status.md
  - Requirements: FR-001, FR-003, FR-005; ADRs: 0009, 0024. Depends on: T019, or T019 recorded as dropped.
  - Verify: `task docs:check`: green; the how-to shows the retry line, the `project-alias` and `unreconciled-run` refusals and their next actions; the status page records P10 through the module with T019's run id, or says it was not run, without a 32-hex id.
  - Evidence: `evidence/T020.md`; initial status `not-run`.

- [ ] T021 [US5] Write the retained-infrastructure list specs/007-cleanup-first-live-slice/evidence/retained.md before the final chain
  - Requirements: FR-011; ADRs: 0024. Depends on: T019 if it ran (else T018); T006, T007, T009 on `main`.
  - Scope: written by the coordinator from the latest run record's listings and retained states in the live clone, with no new API call: the approved retained infrastructure that stays after 2026-10-24 12:42, as data-model.md fixes it. A resource a probe or an ephemeral stack created is never a row: if one exists, the file names it at the top as a blocker, and T022 does not start until a clean leftover check shows it gone. A listing error in the record is written as "not clean", not as an empty kind.
  - Verify: `task check:specs -- specs/007-cleanup-first-live-slice`: `TRACE_OK`; every row is approved retained infrastructure with a removal path and a monthly cost with a source or UNVERIFIED; no blocker is open.
  - Evidence: `evidence/retained.md`; initial status `not-run`.

- [ ] T022 [US5] Live: the final chain over stacks/deployments.yaml — run by the coordinator under D118
  - Requirements: FR-010, SC-002, SC-003; ADRs: 0024. Depends on: T021 with no open blocker; T006, T007 and T009 on `main` at a reviewed SHA. Finishes with its recovery on 2026-10-22.
  - Verify: `mise exec -- task live:chain -- --reviewed-sha <sha> all` from the clean live clone: the sandbox's KD-1 binding admitted by FR-003; chain `pass`; leftover check `pass` with any retries quoted from the attempt record; the run's cost and the credit left.
  - Evidence: `evidence/T022.md`; initial status `not-run`.

- [ ] T023 [US5] Verification and cleanup on 2026-10-23, against specs/007-cleanup-first-live-slice/evidence/retained.md — run by the coordinator under D118
  - Requirements: FR-010, FR-011, SC-003; ADRs: 0024. Depends on: T022.
  - Verify: for each run of this spec whose leftover check is not clean, `mise exec -- task live:probe -- --reviewed-sha <sha> --cleanup <run-id>` (probe), or the lane's destroy verb (task live:destroy with the reviewed SHA and the instance) for a chain's ephemeral instance, until clean; no run started whose recovery would cross 23:00; `retained.md` re-checked against the last record and the Control Panel, with nothing disposable left; the spec's total spend recorded (voucher and card).
  - Evidence: `evidence/T023.md`; initial status `not-run`.

## Final phase: Polish

- [ ] T024 Record spec 007's state in specs/README.md and specs/007-cleanup-first-live-slice/spec.md
  - Requirements: FR-004, FR-011; ADRs: 0021, 0024. Depends on: T023.
  - Verify: exempt — docs-only; the specs table row states what ran live, T001's outcome and what approved infrastructure stays; `task docs:check` green.
  - Evidence: `evidence/T024.md`; initial status `not-run`.

- [ ] T025 Run the exit checks of tools/harness/checks.yaml for spec 007
  - Requirements: FR-012; ADRs: 0008, 0019. Depends on: T024.
  - Verify: `task check:specs; task test:traceability; task test:foundation-ci`: green, with `TRACE_OK` for 007; V001–V005 evidence linked from their tasks.
  - Evidence: `evidence/T025.md`; initial status `not-run`.

## Dependencies & execution order
- Spec 006 T022–T026 (PR #123) on `main` → T003, T004. T001 → T017, T018.
- US1: T003, T004 → T006; T005, T006 → T007. US2: T007 → T008 → T009.
  US3: T009 → T010, T011, T012; T011, T012 → T013; T011, T013 → T014; T012, T014 → T015;
  T010, T015 → T016. US4: T001, T009 → T017.
- Live: T006, T007, T009, T014, T015, T016 reviewed on `main`, and T018 → T019 (not after
  2026-10-21 23:00) → T020. T019 if it ran (else T018) → T021 → T022 (by 2026-10-22) → T023
  (2026-10-23) → T024 → T025.
- External gates: the coordinating session's D118 grant (to 2026-10-24 23:59, or below 20 € of
  credit).
- MVP boundary: US1 + US2 + T021–T023 (cleanup reliable, alias refused, window closed clean). US3
  live (T019) is dropped by its cut-off if late; US4 is offline proof.

## Parallel opportunities
- T003, T004 and T005 (different files). T010, T011 and T012 (different packages).
- No live task runs in parallel with another: one workstation, one credit.

## Completion
All V001–V006 evidence green, or T019 recorded as dropped by its cut-off; every live run of this spec
ends clean; nothing disposable remains; `evidence/retained.md` lists only approved retained
infrastructure; the total spend is recorded.
