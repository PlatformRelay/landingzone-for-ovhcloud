# Loop — 007-cleanup-first-live-slice

Repo: `PlatformRelay/landingzone-for-ovhcloud` · Branch: `lane-007` (worktree `003-probes`) · Base: `origin/main` · Started: 2026-10-16 · Reviewers: two-model (review leg 1 and, on triggers, the strong review leg); spec-loop-duo under D117
Scope: D119. The live slice runs in this order: leftover-listing retry, A17, a disposable budget-alert run, the retained-resource list, then the final chain by 2026-10-22. 2026-10-23 is for verification and cleanup only. The narrowed locks (A24), the second project, KD-3, B1–B3 and AgentEx are out of scope.
Next: L — US3 lands (PR); owner clears the 9 pre-baseline runs (terminal-gated); T019 live budget-alert run by 2026-10-21 23:00; then T017–T018, T020–T025
Budget (Claude review legs, D117 cap 120 across the goal): about 79 used before this loop; per task, 1 strong leg per triggered round (most of 007 is live lane), times about 2 rounds; stage B reserve 3. Spend: voucher ceiling about 5 €; 0 € card; no new live start below 20 € of credit or after 2026-10-23 23:00 (Europe/Berlin).

## Stages
- [x] 0 orient — spec set present, branch from main
- [x] P spec set — authored per D119 (2f8ec3e, 11797ee, 7006f80 on main, PR #125)
- [x] R spec-set review — round 1 on cf35c65: both legs BLOCK (orphaned alert had no delete path; a baseline hid an earlier orphan; retained.md could accept a disposable resource; narrowed locks unworkable). Round 2 on d0e3cc5: both legs BLOCK (a run interrupted before its baseline blocked every later probe; baseline authority too weak). All verified findings were fixed (56036a2); no third round, per the two-round limit.
- [ ] L task loop
- [ ] B branch review
- [ ] Hand-off

## Fitness functions
As spec 006's loop.md: test:dependencies, the default check:specs (001, 005, 006, 007), test:traceability, test:foundation-ci, docs:check, test:live-lane, test:bootstrap, and every CI target on the final code tree.

## Triage
| Stage | Finding (one line) | Sev | Models | Disposition | Where it went / why | Needs user? |
|---|---|---|---|---|---|---|
| R1 | Orphaned budget alert had no delete path | critical | both | fix | DeleteAlert + budget-alert ProbeCheck Sweep | no |
| R1 | A baseline could hide an earlier run's orphan | critical | strong | fix | Before for nameless kinds; refuse start while an unreconciled run exists | no |
| R1 | retained.md could accept a disposable resource | critical | leg 1 | fix | disposable resources block completion | no |
| R1 | Narrowed per-tenant locks cannot let two chains run together | critical | both | defer | out of scope; split-lock design recorded (A24) | no |
| R2 | A run interrupted before its baseline blocked every later probe | critical | both | fix | start-up phases; pre-create runs retire | no |
| R2 | A readable baseline was not enough authority to delete | critical | leg 1 | fix | strict validation; delete only plan-matching alerts; owner path for a lost baseline | no |
| R2 | Scalar alert output could not reach Observe; archiving broke storage-iam recovery; T006/T007 gate | major | both | fix | 56036a2 | no |
| L T003 | Backoff fixture values equal; no nil-Backoff (production default) case; no 428/430/600 rows; transportError.Error() drops the inner error | minor | strong | fix in T006 | T006 adds a {1ms,2ms} case, a nil case (5 s wait) and an empty non-nil case, the extra status rows, and pins the Error() choice | no |
| L T004 | Probe case lacks a retry-line assertion; the shared budget is indistinguishable from a copy made at check start | minor | strong | fix in T006 | T006 adds the assertion with a mutant dropping Term, and G24 mutants for the budget copy | no |
| L T006 | The chain's baseline listing before the first apply still retries nothing; a transient error stops the run before any create | — | task | decide no (for now) | Coordinator minor decision: fail-safe (nothing created, rerun); a follow-up for a later spec if transient pre-apply failures recur | no |
| L T006 | The element-decode row takes the last page's attempt number | minor | review | defer | stage B (record only) | no |
| L T009 | identify --fresh-account writes an aliased answer to account.env before state refuses it | minor | review | defer | stage B (an optional earlier check; FR-003 is met) | no |
| L T008/T009 | TestObserveChainReported/kd1-without-flag corrected (TEST class, FR-003): STATE gets its own project | — | coordinator | accept | decided 2026-10-16 | no |
| L T010 | G1: the line `budget_alert = local.budget_alert` itself is not testable (a test cannot see a child module's resources) | minor | both | decide accept | Coordinator minor decision 2026-10-18: T016's review reads it and T019's live read-back checks the real alert; no plan-JSON check on the time-critical path | no |
| L T010 | G2: a project declared outside module.project is not caught | minor | strong | defer | T016's review | no |
| L T013 | DR-T013-a: lost create with both baseline copies deleted classifies pre-create, so its alert is retired, not swept | minor | strong | decide accept | Coordinator 2026-10-19: deleting both copies by hand is outside the threat model; a budget alert costs nothing and the leftover check still lists it; no FR-006 change on the time-critical path. Known gap for the hand-off | no |
| L T013 | DR-T013-b: a baseline-backed cleanup with no state makes no tofu call | — | task | decide confirm | Coordinator 2026-10-19: nothing for tofu to destroy; the sweep works through the API | no |
| L T013 | G1: no mutation proof (no scratch reference) | minor | task | defer | T014/T015 Verify lines carry the mutants | no |
| L T014 | DR-T014-a: Before scoped to the baseline's own project (literal reading would block all probes) | major | task | decide confirm | Coordinator 2026-10-19: confirmed; data-model row amended in its own commit | no |
| L T014 | 9 pre-baseline run directories in the maintainer's state/probes (state + probe.env, no baseline) are untrusted and block every probe start | major | coordinator | carry | T015 must make --accept-lost-baseline work for state-backend, storage-iam, network and swift-reach roots; the coordinator clears the 9 through live:probe --cleanup --accept-lost-baseline before T019 | no |
| L T014 | Missing summary.json for a run ending at its baseline; a kill-left .json.tmp never removed | minor | strong | defer | stage B | no |
| L T015 | DR-T015-a: alert paths gate the project as one path segment, not 32 hex | minor | task | decide accept | Coordinator 2026-10-19: the segment rule blocks traversal; test ids stay valid | no |
| L T015 | data-model said "stdin is a terminal" and "deletes nothing"; code reads /dev/tty and only budget-alert deletes nothing | minor | strong | fix | Coordinator 2026-10-19: data-model row amended to match | no |
| L T015 | The 9 pre-baseline runs need the owner at a terminal (--accept-lost-baseline is terminal-gated by design) | major | coordinator | carry | owner sheet once US3 is on main; before T019 | no |
| L T015 | relist after delete has no retry; alertKind/stateFile duplicate names | minor | both | defer | stage B | no |
| L T016 | G2 (no ovh_cloud_project outside module.project) closed by grep and review, no machine guard | minor | strong | defer | stage B | no |

## Tasks
| Task | Verdict | Review (legs, rounds, strong leg?) | Gaps / decision request |
|---|---|---|---|
| T001 | CLOSED: A17 refuses; B5 offline, A24 out | leg 1, round 1; no strong leg (docs-only) | T008 lacks the row "second tenant owns `STATE`" (R2 *Consequence*): coordinator to add |
| T002 | CLOSED: trace already held (TRACE_OK 001, 005, 006, 007; ci-source and inputs name 007) | leg 1, round 1; no strong leg (evidence-only) | none |
| T003 | CLOSED: 16 tests (14 task cases, a bounds case, the classifier table) red on the stubs for their stated reasons; 3 pins pass | leg 1 and strong leg, rounds 1 and 2 | deferred to T006: distinct backoff values, nil/empty `Backoff`, 428/430/600 rows, `transportError` text |
| T004 | CLOSED: 3 tests (chain budget spent, chain budget left, probe at the destroy deadline: admitted/refused) red on today's code for their stated reasons | leg 1 and strong leg, round 1 | deferred to T006: probe retry-line assertion with a `Term` mutant; shared-versus-copied budget mutant |
| T005 | CLOSED: 5 tests (keepRecord: whole record, next slot, nothing to move; entry: cleanup keeps the record, second cleanup's undo still reads observations.json) red on the stub for their stated reasons; 1 entry pin (cleanup without a record) passes | leg 1 and strong leg, round 1 | deferred to T007: `TestKeepRecordNextSlot` move check via G25 "slot reuse"; spec.md "next free number" vs data-model "1 + highest": coordinator to align spec.md wording |
| T006 | CLOSED: retry and attempt records in leftovers.go, budget/clock/terminal from chain.go and runner.go; T003's 16 and T004's 3 known-red rows green; 8 cases for the T003/T004 deferrals and 9 for review findings added; 36 G24 mutants, 35 killed, 1 equivalent (chain-level copy where the check starts) | leg 1 and strong leg, rounds 1 and 2; round-2 behaviour fixes unreviewed by rule | deferred: an element-decode row takes the last page's attempt number; `attempts` checked before the context's end (record only); the chain's baseline listing retries nothing (nil budget, data-model wires only the final check) |
| T007 | CLOSED: `keepRecord` (sweep.go) with its writes in files.go, wired through `Runner.recordDir` on `--cleanup` only; T005's 5 known-red rows green; 2 tests added (archive on cleanup only; a failed archive fails the cleanup, record unchanged); 18 G25 mutants in 32 runs, each killed in its unit or entry scope (T005's slot-reuse deferral: copy-instead-of-move killed by the suite, not by `TestKeepRecordNextSlot` alone); every CI target green | leg 1 and strong leg, round 1 | rejected: leg 1's run-lock, symlink and mode findings (no new exposure; reasons in evidence). Nits accepted: a partial archive splits one attempt across two slots; an `observations.json`-only directory archives nothing |
| T008 | CLOSED: 8 unit cases (5 refusals, 3 pins red on the stub's "not implemented"), lane table (plan, apply, destroy, chain) and bootstrap case red on the stub for their stated reasons; lane and bootstrap KD-1 pins pass; scratch reference green, 11 of 11 mutants killed (4 G26, the case defects, 2 from review) | leg 1 and strong leg, rounds 1 and 2 (round 1: bootstrap stdout id scan, two-tenant KD-1 pin) | for T009: `TestObserveChainReported/kd1-without-flag` runs the KD-1 account with the flag cleared and is refused by a correct FR-003 check; T009 changes it as a TEST-class correction |
| T009 | CLOSED: `CheckProjectAliases` (state ref and tenant environments' refs, pairwise; KD-1 only state-beside-tenant and never when a tenant owns `STATE`; unresolved refs skipped) wired through `boundUnaliased` in the lane's `runLane` and the bootstrap's `(*stateRun).setup`; all 12 of T008's known-red rows green; 2 tests added (unresolved-refs pin, bootstrap malformed binding); 20 mutant runs (17 distinct), each killed, one of them by a crash rather than an assertion; every CI target green | leg 1 and strong leg, round 1 (no behaviour defect, so no round 2) | deferred to stage B: `identify --fresh-account` writes an aliased answer before `state` refuses it (optional earlier check). Rejected: `destroy` refused on an already-aliased account (FR-003 names destroy) |
| T010 | CLOSED-WITH-GAPS: budget-alert root (lock file and `probe.tf` copied, stub `main.tf` with the alert object in `local.budget_alert`), 8 `tofu test` runs: 2 red on the stub for their stated reasons, 6 pins pass; P2 observed (mirror-only init with the lock file passes); reference 8/8, 10 of 11 mutants red; every other CI target green | leg 1 and strong leg, rounds 1 and 2; round-2 test fix (type-stable key check) unreviewed by rule | G1: the wiring line `budget_alert = local.budget_alert` is not observable from `tofu test` (T016 review, T019 read-back; plan-JSON check proposed). G2: an `ovh_cloud_project` declared outside `module.project` not caught (T016 review). T016 must keep `budget_alert = local.budget_alert` |
| T011 | CLOSED: 12 tests (15 `ClassifyRun` rows) red on the stubs for their stated reasons; 1 pin and 2 `Before` regression guards pass (outcome already holds); stubs `Before`, `namelessKinds`, `probeBaseline`, `Baseline`, `LoadBaseline`, `RunClass`, `ClassifyRun`, `CondUnreconciledRun`, `unreconciledRuns`; scratch reference green, 35 of 35 mutants killed, 1 control survives; every other CI target green | leg 1 and strong leg, rounds 1 and 2; round-2 test fixes (post-apply listing, order-free ids) unreviewed by rule | for T014: earlier tests FR-006 retires (`sweep_test.go` order checks see the baseline listing before apply; `TestProbeRerun` run 2 now refused `unreconciled-run`; `TestProbeListingRetryBudget`/`Line` see the check's clock read before any child) and the baseline listing's retry, clock and budget. Deferred to the coordinator: data-model "record copy first, else recovery copy" when the record copy is unreadable; optionally "of this run and project" in the pre-create predicate |
| T012 | CLOSED: 12 tests in `alerting/check_test.go` (sweep: delete only the non-baseline plan-matching alert in the `Sweep` argument's project and list again; still listed fails; foreign alert reported and kept, beside B too; untrusted 7 rows; recovery copy; lost create; pre-create sends nothing; gate 6 rows with controls; read-back and mismatches) and `TestProbeCheckBudgetAlert`, all red on the stubs; stubs `alerting.Options`/`Check`, `APIListAlerts`/`APIGetAlert`/`APIDeleteAlert`; `Options` gains `StateDir` and `Project` (coordinator decisions 2026-10-18, data-model row updated); scratch reference green, 38 mutants killed, 1 control survives | leg 1 and strong leg, rounds 1 and 2; round-2 test fixes (B beside a foreign alert, `get-no-id`) unreviewed by rule | for T015: `probeCheck` needs the run paths (`RunDir`, `StateDir`) and must assert them (the registration test cannot see them unwired; T013's entry cases partly do); add `tools/internal/probes/live/alerting` to `test:bootstrap`/`bootstrap:account` inputs with the import. For the coordinator: data-model's registration row still names `RunDir: …` only (proposed: `…, StateDir: …, Project: project`) |
| T013 | CLOSED-WITH-GAPS: `probe_recovery_test.go`, `lz-live probe` as fresh processes of the test binary (`rcEntry`), fake API with alert routes and fake `tofu` (`rcTofu`: lost, create, block); 5 tests, 9 runs (lost create exit/kill, leftover failure, success path, 4 start-up kills, lost baseline with `--accept-lost-baseline` on a pty), all red on today's code for their stated reasons; harness probe on scratch reached the baseline-listing kill and the pty path; race 0; no mutation proof (G1) | leg 1 and strong leg, rounds 1 and 2; round-2 test fixes (failing-leftover pre-create cleanup, row 3 keeps its passphrase) unreviewed by rule | for the coordinator: DR-T013-a (task's "both copies deleted" is pre-create under FR-006; test uses unreadable copies; amend FR-006 or confirm), DR-T013-b (a stateless baseline-backed cleanup calls no `tofu`: confirm). For T014/T015: G27 mutants against these cases (G1); the record-copy-only entry path rests on T012's `TestSweepRecoveryCopy` |
| T014 | CLOSED-WITH-GAPS: phases (claim, refusal, `probe.env`, passphrase, baseline under the run's context before any child), write-once synced copies, `LoadBaseline` precedence, `ClassifyRun`, cleanup by class (untrusted refused; no state: no passphrase, no `tofu`; baseline as `Before`), failed leftover check keeps any probe's files, a failed cleanup never retires; T011's 12 and T013's start-up rows green; 45 mutants (all G27), 44 killed, 1 equivalent | leg 1 and strong leg, rounds 1 and 2; round-2 fixes (credential-file and directory fsync, exclusive run-directory claim) unreviewed by rule | DR-T014-a for the coordinator: `Before` judges only the baseline's project (data-model row to amend, or a one-line revert). Before T019: check the maintainer's `state/probes/` for earlier run directories (a run with state and no baseline needs T015's `--accept-lost-baseline`). Deferred to stage B: no `summary.json` for a run ended at its baseline; `.json.tmp` left by a kill. Rejected: `Check` complexity 26 (lowered from 44) |
| T015 | CLOSED-WITH-GAPS: `ListAlerts`/`GetAlert`/`DeleteAlert` and `GetMe`'s address (`apicheck.go`); `alerting.Check` (read-back, sweep by class against the baseline, plan-matching deletes, relist); registration with the run's paths; `--accept-lost-baseline` (terminal gate before credentials; untrusted runs only; destroy from state and the root's sweep, except `budget-alert`, which deletes nothing); T012's 12 + `TestProbeCheckBudgetAlert` and T013's 4 known-red rows green; 7 tests added; 36 mutants killed | leg 1 and strong leg, rounds 1 and 2; round-2 wording fix (owner prompt) unreviewed by rule | for the coordinator: DR-T015-a (alert ops gate the project as a path segment, not 32 hex, because the probe tests use `p1`); data-model "stdin is a terminal" vs the controlling terminal. G1: live recipient/id shapes UNVERIFIED until T019. Deferred to stage B: relist without retry; `stateFile`/`alertKind` duplicated. Owner command sheet for the 9 pre-baseline runs in evidence |
| T016 | CLOSED-WITH-GAPS: `main.tf` enables `local.budget_alert` (threshold 1, delay 3600, `ovh_me` address; `budget_alert = local.budget_alert` kept) and wires `alert_id` and `budget_alert_check` to `module.project.budget_alert_id`; run-sheet row 13 with the FR-007 owner recovery and the alert row's baseline note; how-to pin re-recorded; T010's 2 known-red rows green, `test:live-lane` fully green; 5 of 5 mutants red; every CI target green | leg 1 and strong leg, round 1 (no behaviour defect, so no round 2); wording fixes unreviewed by rule | G1 (T010) stays: the wiring line is review-checked only, T019's read-back is the live check; G2 closed by grep and review, no machine guard. Rejected: evidence not closed at review time (close-task order) |

## Known red
| Test (file:name) | Story | Written in | Cleared in |
|---|---|---|---|
| leftovers_retry_test.go:TestListingRetryTransient500 | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryClasses | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryBodyReadReset | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryToken (503-once, 401) | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryResumesPage | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryObjectRead | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryExhausted | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryStillReconciles | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryPermanent/*/record | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryRepeatingCursor/record | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryRecordedInLeftoversJSON | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryDeadline | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryBudget (refuses, nil) | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryCancelled | US1 | T003 | T006 |
| leftovers_retry_test.go:TestListingRetryRedacted | US1 | T003 | T006 |
| leftovers_retry_test.go:TestTransientListing | US1 | T003 | T006 |
| chain_retry_budget_test.go:TestChainListingRetryBudgetSpent | US1 | T004 | T006 |
| chain_retry_budget_test.go:TestChainListingRetryBudgetLeft | US1 | T004 | T006 |
| chain_retry_budget_test.go:TestProbeListingRetryBudget (admitted, refused) | US1 | T004 | T006 |
| sweep_record_test.go:TestKeepRecordArchivesWholeRecord | US1 | T005 | T007 |
| sweep_record_test.go:TestKeepRecordNextSlot (second-cleanup, after-a-gap) | US1 | T005 | T007 |
| sweep_record_test.go:TestKeepRecordNothingToMove | US1 | T005 | T007 |
| probe_cleanup_record_test.go:TestProbeCleanupKeepsRecord | US1 | T005 | T007 |
| probe_cleanup_record_test.go:TestProbeCleanupKeepsObservationsForUndo | US1 | T005 | T007 |
| alias_test.go:TestProjectAliasTenants | US2 | T008 | T009 |
| alias_test.go:TestProjectAliasEnvironments | US2 | T008 | T009 |
| alias_test.go:TestProjectAliasSandboxPin (pin; red on the stub's "not implemented" only) | US2 | T008 | T009 |
| alias_test.go:TestProjectAliasFlagTwoTenantsPin (pin; red on the stub's "not implemented" only) | US2 | T008 | T009 |
| alias_test.go:TestProjectAliasStateWithoutFlag | US2 | T008 | T009 |
| alias_test.go:TestProjectAliasFlagNotTenants | US2 | T008 | T009 |
| alias_test.go:TestProjectAliasTenantOwnsState | US2 | T008 | T009 |
| alias_test.go:TestProjectAliasUnusedRefPin (pin; red on the stub's "not implemented" only) | US2 | T008 | T009 |
| alias_lane_test.go:TestLaneProjectAlias (plan, apply, destroy, chain) | US2 | T008 | T009 |
| bootstrap_alias_test.go:TestBootstrapProjectAlias | US2 | T008 | T009 |
| tests/live/probes/budget-alert/probe.tftest.hcl:module_alert_on_run_project | US3 | T010 | T016 |
| tests/live/probes/budget-alert/probe.tftest.hcl:outputs_name_the_module_alert | US3 | T010 | T016 |
| probe_baseline_test.go:TestProbeBaselineBeforeApply | US3 | T011 | T014 |
| probe_baseline_test.go:TestProbeBaselineEmptyListing | US3 | T011 | T014 |
| probe_baseline_test.go:TestProbeBaselinePassingRunRetires | US3 | T011 | T014 |
| probe_baseline_test.go:TestProbeBaselineListingFails | US3 | T011 | T014 |
| probe_baseline_test.go:TestProbeBaselineDigestNotID | US3 | T011 | T014 |
| probe_baseline_test.go:TestLeftoverBefore (alert-absent-from-baseline, named-kind-in-before) | US3 | T011 | T014 |
| probe_baseline_test.go:TestClassifyRun (15 rows) | US3 | T011 | T014 |
| probe_baseline_test.go:TestLoadBaselineRecoveryCopy | US3 | T011 | T014 |
| probe_baseline_test.go:TestProbeStartUnreconciledRun | US3 | T011 | T014 |
| probe_baseline_test.go:TestUnreconciledRuns | US3 | T011 | T014 |
| alerting/check_test.go:TestSweepDeletesOnlyNewAlert | US3 | T012 | T015 |
| alerting/check_test.go:TestSweepFailsWhenStillListed | US3 | T012 | T015 |
| alerting/check_test.go:TestSweepBaselineOnly | US3 | T012 | T015 |
| alerting/check_test.go:TestSweepLeavesForeignAlert (4 rows) | US3 | T012 | T015 |
| alerting/check_test.go:TestSweepDeletesBesideForeign (2 rows) | US3 | T012 | T015 |
| alerting/check_test.go:TestSweepUntrusted (7 rows) | US3 | T012 | T015 |
| alerting/check_test.go:TestSweepRecoveryCopy (2 rows) | US3 | T012 | T015 |
| alerting/check_test.go:TestSweepLostCreate (3 rows) | US3 | T012 | T015 |
| alerting/check_test.go:TestSweepPreCreate (2 rows) | US3 | T012 | T015 |
| alerting/check_test.go:TestAlertCallsGated (6 rows, red on their controls) | US3 | T012 | T015 |
| alerting/check_test.go:TestObserveReadsBack | US3 | T012 | T015 |
| alerting/check_test.go:TestObserveMismatch (3 rows) | US3 | T012 | T015 |
| probe_check_test.go:TestProbeCheckBudgetAlert | US3 | T012 | T015 |
| probe_recovery_test.go:TestProbeRecoveryLostCreate (exit, kill) | US3 | T013 | T015 |
| probe_recovery_test.go:TestProbeRecoveryCleanupLeftoverFails | US3 | T013 | T015 |
| probe_recovery_test.go:TestProbeRecoveryBudgetAlertPasses | US3 | T013 | T015 |
| probe_recovery_test.go:TestProbeRecoveryStartUpKill (4 rows) | US3 | T013 | T014 |
| probe_recovery_test.go:TestProbeRecoveryLostBaseline | US3 | T013 | T015 |

## Test changes
| Test (file:name) | Written in | Changed in | Evidence it was wrong |
|---|---|---|---|
| observe_test.go:TestObserveChainReported/kd1-without-flag | spec 005/006 | T009 | FR-003 refuses its world (KD-1 account `STATE` = `DEMO_DEV` with the flag cleared) at setup; coordinator decision 2026-10-16. `STATE` now has its own project in account.env, the fake tofu and the fake API; assertions unchanged |
| sweep_test.go:swOrder (TestSweepWithUndo, TestSweepNone/full-run) | spec 003 | T014 | FR-006: a listing before the first apply is the baseline, named `baseline` |
| sweep_test.go:TestSweepCleanupNoState | spec 003 | T014 | data-model "destroy if state", DR-T013-b: a stateless cleanup runs no destroy |
| runner_test.go:TestProbeRerun, cmd/lz-live/main_test.go:TestProbeRerunEntry | spec 003 | T014 | FR-006: run 2 is refused beside unreconciled run 1; reordered to run 1, cleanup, run 2 |
| chain_retry_budget_test.go:destroyClock (TestProbeListingRetryBudget, TestProbeListingRetryLine) | T004 | T014 | FR-006: the baseline listing reads the check's clock before the first child; a wait before any child is the error instead |
| cmd/lz-live/probe_cleanup_record_test.go:TestProbeCleanupKeepsRecord | T005 | T014 | FR-006: the run writes its own `baseline.json`; the hand-written one (`listed: {}`) is invalid |
| probe_check_test.go:TestProbeCheckBudgetAlert and every other `probeCheck` call (spec 003's swift-reach, token-residual, iam-urn, locked-recovery) | T012, spec 003 | T015 | FR-007: the Sweep loads the run's baseline from the record and state directories, so `probeCheck` takes the run's paths (`runPaths{}` added; assertions unchanged; T012's test text allowed it) |

## Live runs
| Task | Run id | Reviewed SHA | Observation | Cost | Leftovers |
|---|---|---|---|---|---|

## Lessons
- See spec 006's and spec 003's loop.md *Lessons*; they apply here. Key ones: the entry command shape; whole-checkout scratch copies for live-package mutants; fakes must mock attributes the live API leaves empty (spec 003 T039); a create whose id is returned but whose wait fails leaves the resource outside state (2026-10-16 chain incident).
- An exemption keyed on a name (the state ref under KD-1) needs a case where that name is also used in the role the rule protects (a tenant environment owning `STATE`). (T001)
- A retry case counts requests per page, not per path (a multi-page fixture makes a correct retry send more than "attempts" requests); a backoff fixture needs distinct values, or the second wait is untested. (T003)
- A "same budget" wiring case needs state the fresh budget lacks (a reserve raised by a slow read after the spending wait); an edge against a code-computed deadline anchors the fake clock at a child's real deadline (`childStarted`) and asserts the anchor. (T004)
- A stub for a case whose answer is the empty value ("" when there is nothing to move) must return an error, or that case is a pin against the stub. (T005)
- When a test task's case only bites once a later task wires the code (observations.json hidden by the archive), add an assertion only the wiring satisfies (the archive copy) for the red, and prove the other half on a scratch implementation plus its mutant. (T005)
- `TestFilesOnlyWriter` confines file writers (`os.Rename`, `os.Mkdir*`, `os.WriteFile`) to `files.go`: T007's `keepRecord` writes through `files.go`. (T005)
- A "shared, not copied" requirement is testable only where something reads the shared object after the copy point: put the copy mutant there, and name a copy no later code reads equivalent. (T006)
- A suite red on another task's known red cannot judge a mutant by exit code: run each mutant's target tests only and read their failure lines (`t006-mutate.py` in the records directory). (T006)
- A request the check needs once (the token) is one request for the whole check: count its attempts and its give-up apart from the listing that asked for it. (T006)
- Judge an HTTP answer by its status before its body or headers: a 4xx with a broken body, or a 3xx whose Location Go cannot parse, is still that status, not a transport failure. (T006)
- A `select` on a timer and `ctx.Done()` may pick the timer when both are ready: re-check the context before the next attempt, and test with a clock that cancels as it fires. (T006)
- SonarCloud's complexity gate can be checked before the PR: host `~/go/bin/golangci-lint-v2.13.1 run` with a config enabling only `gocognit` (`min-complexity: 15`) over the package. (T006)
- A new branch in a function already over the complexity limit (`Execute`, 71) goes into a small helper that replaces an existing call (`recordDir` for `ensureDir`), so the old function's score does not move. (T007)
- A mutant's `old` text must be unique in its file or name its occurrence: a mode mutant on `os.O_EXCL, 0o600)` hit `writeSynced` first and "survived". (T007)
- A wiring mutant that no entry test reaches (an ignored error, a gate on the wrong flag) needs a unit case on the wiring function itself. (T007)
- A "zero children before the refusal" case needs a world in which the run would otherwise reach a child: with an unrelated earlier refusal (growth's two-environment tenants stop at `tenant-environments`) the count is zero for the wrong reason; check the stub run shows children. (T008)
- Before closing a test task, run a scratch implementation against the whole package, not only the new tests: it finds earlier tests whose premise the requirement retires (`kd1-without-flag` under FR-003). (T008)
- A chain test moved to a second project needs it in three fakes: account.env, the fake tofu's `state_project_id` (`laneWorld.State`) and the fake API's listings (regions with empty storage, empty lists, an untagged IAM resource), seeded from the first request (`early`). (T009)
- `gocognit` `min-complexity: 15` reports only above 15; to hold "under 15" run it at a lower setting (10 or 14). (T009)
- An OpenTofu test reaches a child module only through its outputs (`module.x.<resource>` is `Unsupported attribute`, and the error skips every later run): assert a module call's inputs through a root local, and record the wiring line as a gap. (T010)
- An HCL key check of an output that may be an object or a map is `sort(keys(x)) == tolist([...])`; `keys(x) == [...]` compares a list with a tuple and fails on a `tomap()` root. (T010)
- A known red in `test:live-lane` stops every later `LZ_TARGETS` target in CI (`set -e`): FINAL runs them all through the entry, and a PR opened over that range says so. (T010)
- A content check after the run cannot tell a record written before the apply from one rewritten after it: make the fake world change at the apply (a listing that gains the created resource). (T011)
- To read a file that is kept only until reconciliation, fail a later step (the destroy) so the run stays unreconciled. (T011)
- A refusal row that passes on a stub (the gate refuses the unknown op) is red only with its acceptance control in the same row: put the control call inside each refusal subtest. (T012)
- A sweep test whose deleting fixtures all carry state cannot tell a lost create (valid baseline, no state, no inventory) from pre-create; give the delete case a row without state. (T012)
- When a function gets the same value two ways (the `Sweep` argument and an `Options` field), set them apart in the fixture, or a mutant reading the wrong one survives. (T012)
- A "report the foreign one and fail" case needs the run's own resource beside it, listed before and after it, or a sweep that stops at the first mismatch passes. (T012)
- Test-package names (`digest`, `observation`) collide with the implementation the next task writes in the same package: prefix the test's package-level names. (T012)
- A case whose first process "ends by itself" unreconciled must make that process's own reconciliation fail (refuse its requests after the apply), or a correct implementation sweeps in the first process and the case cannot pass. (T013)
- The runner starts `tofu` in its own process group: a SIGKILL of the lz-live group leaves a blocked fake `tofu` running; have the fake exit when reparented. (T013)
- A task's given that contradicts a fixed predicate (both baseline copies deleted = pre-create) is written as the predicate's nearest untrusted case and raised as a decision request, not pinned as the task words it. (T013)
- A filter keyed on ids from one scope (`Before`: one project's alerts) must carry that scope, or every resource of another scope the check lists is judged against the wrong list. (T014)
- "Kept until reconciled" also means a cleanup that fails before its checks ran never retires the run: retire only on an affirmative pass. (T014)
- A durability change in a shared writer (`credentialDir`) reorders another feature's pinned fsyncs (`TestFilesCredentialSetDurableSwitch`): make it local to the new caller. (T014)
- A wiring case for a path pair needs one entry row per path where only that path holds the evidence (only the record copy, only the recovery copy): with both present a wrong path still reads the other and the mutant survives. (T015)
- An owner-recovery flag that bypasses a safety class must say per root what it deletes, in the prompt the owner reads, not only in the how-to; and it must refuse runs whose class does not need it. (T015)
- A value pair fixed in two languages (the HCL plan, the Go read-back) with no shared source needs at least a comment naming the other place, or a change passes offline and fails live. (T016)
