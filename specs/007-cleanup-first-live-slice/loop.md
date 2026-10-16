# Loop — 007-cleanup-first-live-slice

Repo: `PlatformRelay/landingzone-for-ovhcloud` · Branch: `lane-007` (worktree `003-probes`) · Base: `origin/main` · Started: 2026-10-16 · Reviewers: two-model (review leg 1 and, on triggers, the strong review leg); spec-loop-duo under D117
Scope: D119. The live slice runs in this order: leftover-listing retry, A17, a disposable budget-alert run, the retained-resource list, then the final chain by 2026-10-22. 2026-10-23 is for verification and cleanup only. The narrowed locks (A24), the second project, KD-3, B1–B3 and AgentEx are out of scope.
Next: L — US2 (T008 alias tests, then T009)
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

## Test changes
| Test (file:name) | Written in | Changed in | Evidence it was wrong |
|---|---|---|---|

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
