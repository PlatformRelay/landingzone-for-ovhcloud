# Loop — 007-cleanup-first-live-slice

Repo: `PlatformRelay/landingzone-for-ovhcloud` · Branch: `lane-007` (worktree `003-probes`) · Base: `origin/main` · Started: 2026-10-16 · Reviewers: two-model (review leg 1 and, on triggers, the strong review leg); spec-loop-duo under D117
Scope: D119. The live slice runs in this order: leftover-listing retry, A17, a disposable budget-alert run, the retained-resource list, then the final chain by 2026-10-22. 2026-10-23 is for verification and cleanup only. The narrowed locks (A24), the second project, KD-3, B1–B3 and AgentEx are out of scope.
Next: L — US3 (T010 budget-alert root tests, then T011–T013, T019 live by 2026-10-21 23:00)
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
| tests/live/probes/budget-alert/probe.tftest.hcl:module_alert_on_run_project | US3 | T010 | |
| tests/live/probes/budget-alert/probe.tftest.hcl:outputs_name_the_module_alert | US3 | T010 | |

## Test changes
| Test (file:name) | Written in | Changed in | Evidence it was wrong |
|---|---|---|---|
| observe_test.go:TestObserveChainReported/kd1-without-flag | spec 005/006 | T009 | FR-003 refuses its world (KD-1 account `STATE` = `DEMO_DEV` with the flag cleared) at setup; coordinator decision 2026-10-16. `STATE` now has its own project in account.env, the fake tofu and the fake API; assertions unchanged |

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
