# Loop — 007-cleanup-first-live-slice

Repo: `PlatformRelay/landingzone-for-ovhcloud` · Branch: `lane-007` (worktree `003-probes`) · Base: `origin/main` · Started: 2026-10-16 · Reviewers: two-model (review leg 1 and, on triggers, the strong review leg); spec-loop-duo under D117
Scope: D119. The live slice runs in this order: leftover-listing retry, A17, a disposable budget-alert run, the retained-resource list, then the final chain by 2026-10-22. 2026-10-23 is for verification and cleanup only. The narrowed locks (A24), the second project, KD-3, B1–B3 and AgentEx are out of scope.
Next: L — T001 (settles A17)
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

## Tasks
| Task | Verdict | Review (legs, rounds, strong leg?) | Gaps / decision request |
|---|---|---|---|
| T001 | CLOSED: A17 refuses; B5 offline, A24 out | leg 1, round 1; no strong leg (docs-only) | T008 lacks the row "second tenant owns `STATE`" (R2 *Consequence*): coordinator to add |
| T002 | CLOSED: trace already held (TRACE_OK 001, 005, 006, 007; ci-source and inputs name 007) | leg 1, round 1; no strong leg (evidence-only) | none |
| T003 | CLOSED: 16 tests (14 task cases, a bounds case, the classifier table) red on the stubs for their stated reasons; 3 pins pass | leg 1 and strong leg, rounds 1 and 2 | deferred to T006: distinct backoff values, nil/empty `Backoff`, 428/430/600 rows, `transportError` text |

## Known red
| Test (file:name) | Story | Written in | Cleared in |
|---|---|---|---|
| leftovers_retry_test.go:TestListingRetryTransient500 | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryClasses | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryBodyReadReset | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryToken (503-once, 401) | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryResumesPage | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryObjectRead | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryExhausted | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryStillReconciles | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryPermanent/*/record | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryRepeatingCursor/record | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryRecordedInLeftoversJSON | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryDeadline | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryBudget (refuses, nil) | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryCancelled | US1 | T003 | |
| leftovers_retry_test.go:TestListingRetryRedacted | US1 | T003 | |
| leftovers_retry_test.go:TestTransientListing | US1 | T003 | |

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
