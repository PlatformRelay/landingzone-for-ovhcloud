# Tasks: 007-cleanup-first-live-slice
Status: planned · Created: 2026-10-16 · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md

All boxes are open. This spec creates no Task target; the probe root `tests/live/probes/budget-alert`
is **planned** until T012 closes.

**Test tasks and implementation tasks.** Every behaviour story lists its test tasks before its
implementation tasks. A test task names its cases as given / when / then, each with the defect it
would expose; a fresh session writes the tests from spec.md, data-model.md's *Fixed signatures* and
the task text (not from plan.md's rationale), against a stub whose body returns "not implemented"
where the symbol is new, and closes on a verified red: each new test fails for its stated reason. A
compile error, a missing symbol or a missing binary is not that red. Cases marked *pin* hold today
and pass from the start; every other case must fail for its stated reason. The tests a test task
writes are fixed points for the implementation task that follows; that task closes when they pass.

**Red windows.** A story's tests stay red on their target until the story's last implementation
task closes; nothing is pushed inside a red window. Stories sharing `test:live-lane` run in order
(US1 → US2 → US3 → US4); US2 and US4 also share `test:stacks`.

**Commands.** The entry admits only `lz-offline --candidate <checkout> -- task <target> [-- <path>]`,
so every offline Verify names an existing Task target and runs it through the entry
(`<approved-absolute-path>/lz-offline --candidate <checkout> -- task <target>`, from `$HOME`).
**Live tasks (T018, T021, T023, T024) are run by the coordinator under D118**: the coordinating
session, never a subagent, on the maintainer's workstation, from the clean live clone at a reviewed
SHA on `main`, after the code it runs has passed its independent review; never through the entry.
Before each live run the coordinator checks the remaining credit (Control Panel or `ovhcloud`) and
stops below 20 €; no live run starts on or after 2026-10-23 except verification and cleanup, or
when its recovery allowance (deadline plus destroy budget) would end after 2026-10-23 23:00; the
spec's live spend stays within about 5 € of voucher and 0 € card (FR-010). Every resource a live run
creates carries the run id in its name or tag, or is found through the pre-apply baseline (FR-006).
Mutation proof is required where a Verify names a guard G24–G28 (`contracts/checks.md`).
Evidence: the Verify output is summarised in the PR description; `evidence/<TID>.md` in this
directory points at the PR and names the red and green commits, or, for a live task, the run id,
SHA, outcome, retries seen and approximate cost.

## Phase 1: Setup

- [ ] T001 Establish whether A17 refuses two tenants on one project and record the rule and its consequence in specs/007-cleanup-first-live-slice/research.md (R2)
  - Requirements: FR-004; ADRs: 0009, 0021. Depends on: none. First task: every US4 task waits for it.
  - Scope: read `tools/internal/stacks/manifest.go` (project-ref ownership, `DUPLICATE_NAME`; the `shared_state_project` rule), `tools/internal/live/publish.go` (`BoundAccountFromEnv`) and its callers, T098's decision request, inventory Q8 and D119's conditions; decide whether a second tenant on the sandbox's one project is possible under FR-003 as written. Fill R2's *Outcome* with `refuses` or `permits`, the lines that decide it, and the consequence: on `refuses`, mark T020 and T021 "not run: A17 refuses (T001)" in this file; on `permits`, say which flag or rule admits it and why that does not weaken FR-003. Do not change FR-003 to reach `permits`.
  - Verify: `task check:specs -- specs/007-cleanup-first-live-slice`: `TRACE_OK`; R2 has an outcome with cited lines; T020/T021 are marked accordingly.
  - Evidence: `evidence/T001.md`; initial status `not-run`.

- [ ] T002 Verify the trace of spec 007 in Taskfile.yml and tools/harness/checks.yaml (written in the spec-set commit)
  - Requirements: FR-012; ADRs: 0008, 0019. Depends on: none.
  - Verify: `task test:traceability; task test:foundation-ci; task check:specs`: `TestTaskfileTracesEverySpec` green; `task check:specs` with no argument prints `TRACE_OK` for 001, 005, 006 and 007; `ci-source` reads spec 007; `test:foundation-ci`'s inputs name `specs/007-cleanup-first-live-slice/tasks.md`.
  - Evidence: `evidence/T002.md`; initial status `not-run`.

## Phase 2: User Story 1 — A transient listing error does not fail the cleanup (P1)
Independent test: V001. Test tasks T003–T004, then implementation tasks T005–T006. Starts after spec
006's T022–T026 are on `main`.

- [ ] T003 [US1] Write listing-retry tests in tools/internal/live/leftovers_retry_test.go
  - Requirements: FR-001, SC-001; ADRs: 0008, 0024. Depends on: T002; spec 006 T024 on `main`.
  - Cases (given / when / then → defect exposed), fake `Lister` scripted per path and attempt, `Backoff` of 1 ms, a fake `waitClock`:
    - given `/cloud/project/<p>/network/private` answering 500 then 200 / when `Check` runs / then `pass`, no error, one line `LZ-LIVE retry listing … 500 (attempt 2 of 3)` on `Term`, and one `ListingAttempt{Status: 500, Retried: true}` → exposes today's single attempt (T027's `fail`).
    - given 503, then 429, then a `*transportError` before a 200 on three different listings / when `Check` runs / then `pass` with three retries → exposes a classification that retries only 500, or not 429 or resets.
    - given page 2 of a paginated listing answering 500 once / when `Check` runs / then the retry sends page 2's cursor and the listing holds each item once → exposes a restart from page 1 (duplicates) or a dropped cursor.
    - given a single-object read (a region detail) answering 502 once / when `Check` runs / then `pass` → exposes retries on array listings only.
    - given 500 on every attempt / when `Check` runs / then `fail`, the error names the path, 500 and "attempt 3 of 3", and three attempts are recorded, the last with `reason: attempts` → exposes an unbounded retry or a `pass` on exhaustion.
    - given 403, given 404 where absence is not admitted, given a 200 whose body is not JSON / when `Check` runs / then one attempt each, `fail`, `reason: permanent` → exposes retrying a refusal or a decode failure. (*pin* for the single attempt; the attempt record makes it red until T006.)
    - given a context deadline 3 ms away and `Backoff` of 10 ms / when a 500 arrives / then no wait starts and the error says "no retry" with `reason: deadline` → exposes a wait past the deadline.
    - given a `retryBudget` that refuses the wait / when a 500 arrives / then no wait starts, `reason: budget` → exposes listing waits outside spec 006's shared budget.
    - given `Red` holding the bearer token and a transport error quoting it / when the retry line and the record are written / then neither contains it → exposes an unredacted retry line.
  - Verify: `task test:live-lane`: every case except the *pin* fails against the stub `transientListing` and the unchanged lister, for its stated reason.
  - Evidence: `evidence/T003.md`; initial status `not-run`.

- [ ] T004 [US1] Write record-keeping tests in tools/internal/live/sweep_record_test.go
  - Requirements: FR-002; ADRs: 0008, 0024. Depends on: T002.
  - Cases (given / when / then → defect exposed):
    - given a probe run directory with `summary.json` and `leftovers.json` holding errors / when `--cleanup <run-id>` runs against fakes / then both files are under `attempts/1/` unchanged and new ones are written beside them → exposes the overwrite that lost T027/T028's error text.
    - given `attempts/1/` already present / when a second cleanup runs / then the old files go to `attempts/2/` and `attempts/1/` is unchanged → exposes reuse of an archive slot.
    - given a run directory with neither file (destroy failed before the check) / when cleanup runs / then it proceeds and `keepRecord` returns "" → exposes a cleanup that fails on absence.
    - given a check with two failed attempts / when `leftovers.json` is written / then it holds an `attempts` array with path, attempt, status, class and retried, and no token → exposes attempts kept only in memory.
  - Verify: `task test:live-lane`: all four cases fail against the stub `keepRecord`.
  - Evidence: `evidence/T004.md`; initial status `not-run`.

- [ ] T005 [US1] Retry transient listing errors in tools/internal/live/leftovers.go
  - Requirements: FR-001, SC-001; ADRs: 0008, 0024. Depends on: T003.
  - Scope: `transientListing`, `transportError`, `defaultListingBackoff`, the `LeftoverCheck` fields of data-model.md; the retry in `lister.pages` and `lister.objectRaw`; the chain's and the probe core's check pass their `retryBudget`, clock and terminal.
  - Verify: `task test:live-lane`: T003's cases green; G24 mutants (retry every error; retry 4xx; ignore budget or deadline; restart from page 1) each turn a case red.
  - Evidence: `evidence/T005.md`; initial status `not-run`.

- [ ] T006 [US1] Keep failed attempts and earlier records in tools/internal/live/leftovers.go and tools/internal/live/sweep.go
  - Requirements: FR-002; ADRs: 0008, 0024. Depends on: T004, T005.
  - Verify: `task test:live-lane`: T004's cases and T003's record rows green; G25 mutants (overwrite, slot reuse, attempts dropped) each turn a case red.
  - Evidence: `evidence/T006.md`; initial status `not-run`.

## Phase 3: User Story 2 — Two refs on one project are refused before anything runs (P1)
Independent test: V002. Test task T007, then implementation task T008.

- [ ] T007 [US2] Write project-alias tests in tools/internal/stacks/alias_test.go and tools/internal/live/alias_lane_test.go
  - Requirements: FR-003, SC-004; ADRs: 0009, 0021. Depends on: T006.
  - Scope: a lane test that reads a manifest fixture adds that fixture to `test:live-lane`'s registry inputs in tools/harness/checks.yaml.
  - Cases (given / when / then → defect exposed), ids built in the test, never read from disk:
    - given the `growth` manifest and `DEMO_DEV` = `SHOP_DEV` / when `CheckProjectAliases` runs / then `*RefusalError{Reason: "project-alias"}` naming both refs and not the id → exposes T098's hole (two tenants on one project pass).
    - given `DEMO_DEV` = `DEMO_PROD` / when it runs / then refused → exposes a check limited to tenants (VLAN collisions across one tenant's environments, T098).
    - given the sandbox manifest (flag true) with `STATE` = `DEMO_DEV` / when it runs / then nil → *pin* against a fix that refuses the KD-1 binding the sandbox runs on.
    - given the same manifest with the flag false and different refs on one id / when it runs / then refused naming `STATE` → exposes id-level sharing the decoder's ref check (`manifest.go:583-590`) cannot see.
    - given the flag true and `STATE` = `DEMO_DEV` = `SHOP_DEV` (`growth` with the flag) / when it runs / then refused naming `DEMO_DEV` and `SHOP_DEV` → exposes the flag widening to tenant/tenant sharing (D119: never weaken A17).
    - given an `LZ_PROJECT_ID_SPARE` the manifest never uses, equal to `DEMO_DEV` / when it runs / then nil → *pin* against judging refs outside the manifest.
    - given `lz-live plan` with an aliased account against fake children / when it runs / then it exits with `project-alias` before any `tofu` call (the fake records none) and stderr holds no 32-hex id → exposes a check placed after init or a refusal printing the id.
    - given the bootstrap's state phase with an aliased account / when it binds / then the same refusal → exposes the second binding path (`bootstrap_state.go`) left unchecked.
  - Verify: `task test:stacks; task test:live-lane`: every case except the two *pins* fails against the stub `CheckProjectAliases`.
  - Evidence: `evidence/T007.md`; initial status `not-run`.

- [ ] T008 [US2] Refuse aliased project refs in tools/internal/stacks/adapter.go, tools/internal/live/apply.go and tools/internal/live/bootstrap_state.go
  - Requirements: FR-003, SC-004; ADRs: 0009, 0021. Depends on: T007.
  - Verify: `task test:stacks; task test:live-lane`: T007's cases green; G26 mutants (tenant/tenant under the flag, check after the first child, bootstrap path skipped, id printed) each turn a case red.
  - Evidence: `evidence/T008.md`; initial status `not-run`.

## Phase 4: User Story 3 — The landing zone's budget alert is shown live and leaves nothing (P2)
Independent test: V003 offline, V005 live (T018). Test tasks T009–T010, then T011–T012.

- [ ] T009 [US3] Write probe-baseline tests in tools/internal/live/probe_baseline_test.go
  - Requirements: FR-006; ADRs: 0008, 0024. Depends on: T008.
  - Cases (given / when / then → defect exposed), fake API and fake `tofu`:
    - given alert A listed before the run / when the fake apply creates alert B and then answers an error, so the inventory has no B / then the leftover check reports B as the run's leftover and not A → exposes a nameless alert lost when its creation outlives the apply's error.
    - given that run / when `--cleanup <run-id>` runs / then B is removed and A is untouched → exposes a cleanup that removes alerts the run did not create.
    - given the fake `tofu` checking for `baseline.json` when `apply` starts / when a probe runs / then the file exists and lists A → exposes a baseline written after the first create.
    - given the alert listing answering 500 on every attempt / when a probe runs / then no `tofu apply` is called and the run reports the baseline error → exposes creating without a baseline.
    - given a baseline holding the run's project / when it is written / then it names the project by ref, never by the 32-hex id → exposes an id in the run record.
  - Verify: `task test:live-lane`: all five cases fail against the stub `probeBaseline`.
  - Evidence: `evidence/T009.md`; initial status `not-run`.

- [ ] T010 [US3] Write the budget-alert root's tests in tests/live/probes/budget-alert/probe.tftest.hcl with a stub root and its `lz-check unit` line in Taskfile.yml
  - Requirements: FR-005; ADRs: 0006, 0024. Depends on: T008.
  - Scope: the stub root is `probe.tf` (the `alerting` root's boilerplate, state path ending `/budget-alert.tfstate`) and an empty `main.tf`; add `go -C tools run ./cmd/lz-check -root .. unit tests/live/probes/budget-alert` to `test:live-lane`, and `tests/live/probes/budget-alert` and `modules/cloud-project` to `test:live-lane`'s registry inputs in tools/harness/checks.yaml (every read is an input).
  - Cases (given / when / then → defect exposed), mocked provider as the `alerting` root's tests:
    - given the run's `project_id` / when planned / then exactly one `module.project.ovh_cloud_project_alerting.this[0]` with that `service_name`, `monthly_threshold` 1, `delay` 3600 and the mocked `ovh_me` address → exposes a root that does not reach the module's alert path.
    - given the same plan / when its resources are listed / then no `ovh_cloud_project` is planned → exposes a root that adopts (and could order or terminate) a project.
    - given a `state_path` outside `…/state/probes/<run-id>/budget-alert.tfstate` / when planned / then validation fails → exposes a root runnable outside the probe core, with state in the checkout.
    - given two mocked projects / when planned / then the alert targets `project_id`'s project → exposes picking the account's first project.
    - given the plan / when outputs are read / then `alert_id` and `run_id` exist → exposes a root whose run the leftover check cannot match.
  - Verify: `task test:live-lane`: the five cases fail against the stub root (premise P2: the module call initialises from the entry's mirror; recorded).
  - Evidence: `evidence/T010.md`; initial status `not-run`.

- [ ] T011 [US3] Record the probe baseline in tools/internal/live/runner.go and honour it in tools/internal/live/leftovers.go and tools/internal/live/sweep.go
  - Requirements: FR-006; ADRs: 0008, 0024. Depends on: T009.
  - Verify: `task test:live-lane`: T009's cases green; G27 mutants (baseline after apply, apply after a failed baseline, baseline id as candidate) each turn a case red.
  - Evidence: `evidence/T011.md`; initial status `not-run`.

- [ ] T012 [US3] Write the budget-alert root in tests/live/probes/budget-alert/main.tf and its run-sheet row in tests/live/probes/README.md
  - Requirements: FR-005; ADRs: 0006, 0024. Depends on: T010, T011.
  - Scope: `module "project"` as data-model.md fixes it; the run-sheet row (command, expected, refuted-if, cleanup) and the nameless-kind note pointing at the baseline. If P2 is refuted offline, inline the module's alert resource with its validation copied, and say in the row that the module path is not what ran.
  - Verify: `task test:live-lane; task docs:check`: T010's cases green; the run sheet passes the docs rules.
  - Evidence: `evidence/T012.md`; initial status `not-run`.

## Phase 5: User Story 4 — Two tenants' partial chains do not exclude each other (P3)
Independent test: V004. Starts after T001. Test tasks T013–T015, then T016–T017.

- [ ] T013 [US4] Write shared-lock tests in tools/internal/stacks/lock_test.go
  - Requirements: FR-007, SC-005; ADRs: 0004, 0024. Depends on: T001, T012.
  - Cases (given / when / then → defect exposed), `DirLocks` in a temporary directory, second holders in a fresh process where named:
    - given a shared holder of `account` / when a second process asks shared / then both hold it → exposes an exclusive-only store.
    - given a shared holder / when another process asks exclusive / then `ErrLocked`; and given an exclusive holder, a shared request is `ErrLocked` → exposes readers admitted beside a writer (T062's race).
    - given a fresh process holding a shared lock and killed / when another asks exclusive / then it is granted → exposes a stale lock after a crash (premise P5).
    - given the `growth` manifest, `write` = demo's dev network and runtime, `read` = every retained instance / when `RunLockModes` runs / then `account` shared, `tenant-demo` exclusive, `tenant-shop` shared, in the fixed order → exposes today's all-exclusive set.
    - given `write` holding `demo-state` (account-tenant) / when `RunLockModes` runs / then `account` and `tenant-demo` exclusive → exposes narrowing that drops a writer's `account` lock.
    - given `HoldRun(store, m, ids)` / when called / then it equals `HoldRunModes(store, m, ids, nil)` → *pin* for the lane's and the bootstrap's existing callers.
  - Verify: `task test:stacks`: every case except the *pin* fails against the stub `TryLockShared`/`RunLockModes`.
  - Evidence: `evidence/T013.md`; initial status `not-run`.

- [ ] T014 [US4] Write narrowed chain-lock tests in tools/internal/live/chain_locks_test.go
  - Requirements: FR-007, SC-005; ADRs: 0004, 0024. Depends on: T013, T015.
  - Scope: add `tests/fixtures/manifests/sandbox-two-tenants` to `test:live-lane`'s registry inputs in tools/harness/checks.yaml.
  - Cases (given / when / then → defect exposed), the two-tenant fixture, fake runner whose `apply` blocks until released:
    - given a partial chain over `demo`'s ephemeral stacks blocked in apply / when a partial chain over `lab`'s starts / then it is not refused `locked` and both reach their apply → exposes D100's serialisation.
    - given the `demo` chain blocked / when `lz-live apply lab-state` starts / then it is refused `locked` → exposes the `demo` chain's leftover check reading `lab-state`'s state while it is written (T062).
    - given the selection changing while the locks are taken / when the chain re-selects / then it is refused as today → *pin*.
  - Verify: `task test:live-lane`: the first two cases fail on today's `chain.go`; the third passes.
  - Evidence: `evidence/T014.md`; initial status `not-run`.

- [ ] T015 [US4] Write the two-tenant sandbox fixture tests/fixtures/manifests/sandbox-two-tenants/deployments.yaml and its tests in tools/internal/stacks/two_tenant_test.go
  - Requirements: FR-008; ADRs: 0004, 0005. Depends on: T001, T008.
  - Cases (given / when / then → defect exposed), fixture as data-model.md fixes it:
    - given the fixture / when decoded, reconciled and generated into a temporary tree / then `lab`'s four stacks exist beside `demo`'s, each with its own state bucket and deployer authority → exposes a generator that assumes one tenant (*pin* if it already holds; recorded either way).
    - given the fixture / when `Order` runs for `all` / then each tenant's stacks follow its own `tenant-state` and `account-governance` → exposes cross-tenant ordering.
    - given ids with `LAB_DEV` ≠ `DEMO_DEV` / when `CheckProjectAliases` runs / then nil; given `LAB_DEV` = `DEMO_DEV` (the one sandbox project) / then `project-alias` → exposes B5-on-one-project admitted (FR-003 after T008: *pin*).
  - Verify: `task test:stacks`: results recorded per case; any red case is a defect for T016/T017 or a new task.
  - Evidence: `evidence/T015.md`; initial status `not-run`.

- [ ] T016 [US4] Add shared locks to tools/internal/stacks/lock.go
  - Requirements: FR-007, SC-005; ADRs: 0004, 0024. Depends on: T013.
  - Verify: `task test:stacks`: T013's cases green; G28 mutants on `lock.go` (`LOCK_EX` admitted beside `LOCK_SH`; a written name taken shared) each turn a case red.
  - Evidence: `evidence/T016.md`; initial status `not-run`.

- [ ] T017 [US4] Take write locks exclusive and read locks shared in tools/internal/live/chain.go
  - Requirements: FR-007, SC-005; ADRs: 0004, 0024. Depends on: T014, T016.
  - Verify: `task test:live-lane`: T014's cases green; the G28 mutant dropping retained instances from the read set turns T014's second case red.
  - Evidence: `evidence/T017.md`; initial status `not-run`.

## Phase 6: Live window (US3 live, US4 live gated, US5)
Independent test: V005. Every task here is run by the coordinator under D118 (see *Commands*).

- [ ] T018 [US3] Live: the budget-alert probe with tests/live/probes/budget-alert/ — run by the coordinator under D118
  - Requirements: FR-005, FR-006, FR-010, SC-002, SC-003; ADRs: 0006, 0024. Depends on: T005, T006, T008, T011, T012 on `main` at a reviewed SHA, each reviewed by an independent leg.
  - Verify: `mise exec -- task live:probe -- --reviewed-sha <sha> tests/live/probes/budget-alert` from the clean live clone: `baseline.json` written before the apply; one alert created on the sandbox project (id, threshold 1 and delay 3600 read back); destroy removed it and the alert listing no longer holds it; leftover check `pass`, or `fail` followed by `--cleanup <run-id>` with the first record kept under `attempts/1/`; every retry line quoted; the run's cost. P2 and P3 recorded as observed or refuted.
  - Evidence: `evidence/T018.md`; initial status `not-run`.

- [ ] T019 [US3] Record the retry, the alias refusal and the budget-alert run in docs/how-to/run-the-first-slice.md and docs/reference/verification-status.md
  - Requirements: FR-001, FR-003, FR-005; ADRs: 0009, 0024. Depends on: T018.
  - Verify: `task docs:check`: green; the how-to shows the retry line, the `project-alias` refusal and its next action; the status page records P10 through the module with T018's run id, without a 32-hex id.
  - Evidence: `evidence/T019.md`; initial status `not-run`.

- [ ] T020 [US4] Gated on T001 = `permits`: add tenant `lab` to stacks/deployments.yaml and generate its stacks
  - Requirements: FR-009; ADRs: 0005, 0024. Depends on: T001 (`permits`), T017. On `refuses` (the default): closed as not run, with T001's reason.
  - Verify: `task test:stacks`: the sandbox manifest with `lab` decodes, reconciles and generates; `stacks/` holds `lab`'s stacks; the owner has added `LZ_PROJECT_ID_LAB_DEV` to the account file (the coordinator does not edit `~/.config/ovh-lz/`).
  - Evidence: `evidence/T020.md`; initial status `not-run`.

- [ ] T021 [US4] Gated on T001 = `permits`: live two-tenant chain over stacks/deployments.yaml — run by the coordinator under D118
  - Requirements: FR-009, FR-010; ADRs: 0005, 0024. Depends on: T020 on `main` at a reviewed SHA. On `refuses` (the default): closed as not run.
  - Verify: `mise exec -- task live:chain -- --reviewed-sha <sha> all` from the clean live clone: both tenants' ephemeral stacks applied and destroyed; KD-1 reported `known-deviation`; leftover check `pass`; cost recorded; no isolation claimed.
  - Evidence: `evidence/T021.md`; initial status `not-run`.

- [ ] T022 [US5] Write the retained-resource list specs/007-cleanup-first-live-slice/evidence/retained.md before the final chain
  - Requirements: FR-011; ADRs: 0024. Depends on: T018 (and T021 if it ran).
  - Scope: written by the coordinator from the latest run record's listings and retained states in the live clone, with no new API call: every resource that stays after 2026-10-24 12:42, as data-model.md *Retained-resource list* fixes it. A listing error in that record is written as "not clean", not as an empty kind.
  - Verify: `task check:specs -- specs/007-cleanup-first-live-slice`: `TRACE_OK`; every row has a removal path and a monthly cost with a source or UNVERIFIED.
  - Evidence: `evidence/retained.md`; initial status `not-run`.

- [ ] T023 [US5] Live: the final chain over stacks/deployments.yaml — run by the coordinator under D118
  - Requirements: FR-010, SC-002, SC-003; ADRs: 0024. Depends on: T022; T005, T006 and T008 on `main` at a reviewed SHA. Finishes with its recovery on 2026-10-22.
  - Verify: `mise exec -- task live:chain -- --reviewed-sha <sha> all` from the clean live clone: the sandbox's KD-1 binding admitted by FR-003; chain `pass`; leftover check `pass` with any retries quoted from the attempt record; the run's cost and the credit left.
  - Evidence: `evidence/T023.md`; initial status `not-run`.

- [ ] T024 [US5] Verification and cleanup on 2026-10-23, against specs/007-cleanup-first-live-slice/evidence/retained.md — run by the coordinator under D118
  - Requirements: FR-010, FR-011, SC-003; ADRs: 0024. Depends on: T023.
  - Verify: for each run of this spec whose leftover check is not clean, `mise exec -- task live:probe -- --reviewed-sha <sha> --cleanup <run-id>` (probe), or the lane's destroy verb (task live:destroy with the reviewed SHA and the instance) for a chain's ephemeral instance; no run started whose recovery would cross 23:00; `retained.md` re-checked against the last record and the Control Panel; the spec's total spend recorded (voucher and card).
  - Evidence: `evidence/T024.md`; initial status `not-run`.

## Final phase: Polish

- [ ] T025 Record spec 007's state in specs/README.md and specs/007-cleanup-first-live-slice/spec.md
  - Requirements: FR-004, FR-011; ADRs: 0021, 0024. Depends on: T024.
  - Verify: exempt — docs-only; the specs table row states what ran live, T001's outcome and what stays retained; `task docs:check` green.
  - Evidence: `evidence/T025.md`; initial status `not-run`.

- [ ] T026 Run the exit checks of tools/harness/checks.yaml for spec 007
  - Requirements: FR-012; ADRs: 0008, 0019. Depends on: T025.
  - Verify: `task check:specs; task test:traceability; task test:foundation-ci`: green, with `TRACE_OK` for 007; V001–V005 evidence linked from their tasks.
  - Evidence: `evidence/T026.md`; initial status `not-run`.

## Dependencies & execution order
- Spec 006 T022–T026 on `main` → T003. T001 → T013, T015, T020. T002 first offline check.
- US1: T003, T004 → T005 → T006. US2: T006 → T007 → T008. US3: T008 → T009, T010 → T011 → T012.
  US4: T001, T012 → T013; T008 → T015; T013, T015 → T014; T013 → T016; T014, T016 → T017.
- Live: T005, T006, T008, T011, T012 reviewed on `main` → T018 → T019. Gated: T017 → T020 → T021.
  T018 (and T021) → T022 → T023 (by 2026-10-22) → T024 (2026-10-23) → T025 → T026.
- External gates: the coordinating session's D118 grant (to 2026-10-24 23:59, or below 20 € of
  credit); the owner's account-file entry for T020 only.
- MVP boundary: US1 + US2 + T018 + T022–T024 (cleanup reliable, alias refused, P10 qualified,
  window closed clean). US4 is offline proof by default.

## Parallel opportunities
- T003 and T004 (different files). T009 and T010. T013 and T015 (different packages; both after
  T001).
- No live task runs in parallel with another: one workstation, one credit.

## Completion
All V001–V006 evidence green or, for T020/T021, recorded as not run with T001's reason; every live
run of this spec ends clean or listed in `evidence/retained.md`; the total spend is recorded.
