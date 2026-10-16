# Loop — 003-platform-feasibility (probe lane)

Repo: `PlatformRelay/landingzone-for-ovhcloud` · Branch: `lane-003-probes` (worktree `003-probes`) · Base: `origin/main` · Started: 2026-10-16 · Reviewers: two-model (review leg 1, plus the strong review leg on triggers); duo rules under D117
Scope: the four live probes only — test task, implementation task, then the live run: T029→T030→T025, T031→T032→T026, T033→T034→T027, T035→T036→T028. Tasks T001–T024 are out of this lane.
Next: L — T037 (Sweep tests), then T038; then T030, T032, T034, T036; then the live runs T025–T028
Budget: strong review legs are counted in spec 006's loop.md against D117's cap of 120.

## Stages
- [x] 0 orient — worktree from origin/main; spec set present
- [x] P amendment — probe tasks made agent-runnable (3f610df)
- [x] R spec-set review of the amendment — round 1 on 3f610df: both legs BLOCK (CRITICAL: probe cleanup could not find its own resources without outputs); fixed in ffe2a21. Round 2 on ffe2a21: leg 1 BLOCK (CRITICAL: a lost create response leaves root resources outside cleanup), strong leg CONCERNS; all verified findings fixed in bd23d8d with no further round (two-round limit). Implementation is checked by each task's own review.
- [ ] L task loop — offline tasks, then live runs by the coordinator (D117)
- [ ] B branch review
- [ ] Hand-off

## Known lane conditions
- `check:specs -- specs/003-platform-feasibility` is red today: spec 003 has no `003/` registry keys, and T001–T024 carry trace findings (UNRELATED_ADR, UNRELATED_CHECK, NO_APPLICABLE_CHECK). The default `check:specs` (001, 005, 006) and every CI target stay green. Registering spec 003 and repairing T001–T024 is a later task (goal phase 4). Coordinator decision, 2026-10-16. Task subagents do not try to fix it.
- Live runs (T025–T028) are run by the coordinator from the clean live clone at a reviewed main SHA: `mise exec -- task live:probe -- --reviewed-sha <sha> tests/live/probes/<root>`. Each records its run id, cost (UNVERIFIED unless read) and a clean leftover check.

## Fitness functions
As spec 006's loop.md: test:dependencies, the default check:specs, test:traceability, test:foundation-ci, docs:check, test:live-lane, and every CI target on the final code tree.

## Triage
| Stage | Finding (one line) | Sev | Models | Disposition | Where it went / why | Needs user? |
|---|---|---|---|---|---|---|
| R1 | Probe outputs lacked run_id; cleanup could not rebuild names | critical | both | fix | ffe2a21 | no |
| R1 | Swift cleanup lacked authority over bucket B; one direction only | critical | leg 1 | fix | ffe2a21 | no |
| R1 | Token residual: deadline too short; expiry read as revocation | major | both | fix | ffe2a21 | no |
| R1 | IAM URN verdict could be a propagation artefact | major | both | fix | ffe2a21 (positive control) | no |
| R1 | Recovery faked `tofu force-unlock` over HTTP | major | both | fix | ffe2a21 (pinned tofu seam) | no |
| R1 | Stubs could pass cases vacuously; bucket names lacked lower() | major/note | both | fix | ffe2a21 | no |
| R1 | Tracked files forbid agent-run live targets | warning | strong | fix | D118, chore PR | no |
| R2 | Lost create response leaves root resources outside cleanup (runner.go:694,743,1241) | critical | leg 1 + strong | fix | T037/T038 Sweep from run id; one owner per resource (bd23d8d) | no |
| R2 | Swift: token expiry read as isolation; no seeded object to read | major | both | fix | bd23d8d | no |
| R2 | force-unlock: any non-zero exit read as refused; no owner-death proof; lock not proven held | major | both | fix | bd23d8d | no |
| R2 | Tofu seam puts the state credential in a child env against the ProbeCheck contract | warning | strong | fix | bd23d8d (state keys only; T036 amends the contract) | no |
| R2 | IAM URN could tag the retained project; one action generalised to all | warning | strong | fix | bd23d8d | no |
| L T029 | Sweep may delete a bucket still holding objects (fake allows it) | minor | review | defer | T030 decides, T025 observes (UNVERIFIED) | no |
| L T029 | Foreign-host `auth_url` row | minor | review | defer | T030 | no |
| L T029 | `ProbeCheck.Sweep` lands in T038; the stub has a `sweep` method with its signature | — | task | accept | whichever of T030/T038 lands second wires `Sweep: o.sweep` | no |
| L T031 | Read allowed again after a refusal; an allowed poll exactly at expiry | minor | review | defer | T032 decides, T026 observes | no |
| L T031 | data-model.md `Upper` comment says "zero when none observed" | minor | review | defer | T032 aligns data-model with the stub | no |
| L T033 | Bucket URN form, whether an S3 bucket is an IAM resource, whether a tag key may contain `:` | minor | review | defer | T034 decides, T027 observes (UNVERIFIED) | no |
| L T033 | Validate rule on `bucket_urn` type/project | minor | review | reject | URN form UNVERIFIED; the admin's resource listing is the guard | no |
| L T035 | tasks.md says "two state versions", but the tests forbid writing the backend's live state key | minor | task | defer | T036 aligns the T036/T028 wording with the tests (restore a version of a probe key, not the live state key) | no |
| L T035 | The fake's unlock ignores an overwritten lock object in the condition-ignored case | minor | review | defer | T036/T028 | no |
| L T035 | The tofu diagnostic fixtures are composed from OpenTofu 1.13.0 source, not captured | — | task | accept | T028 records whether live output matches | no |
| R2 | The lost-create-response gap also exists in spec 005's probes and chains | — | coordinator | defer | hand-off: what no sensor covers; the leftover check and the stop rule are the guard | no |

## Tasks
| Task | Verdict | Review (legs, rounds, strong leg?) | Gaps / decision request |
|---|---|---|---|
| T029 | CLOSED-WITH-GAPS | leg 1 + strong leg, 2 rounds (r1 REQUEST_CHANGES/CONCERNS, r2 REQUEST_CHANGES/CONCERNS; verified findings fixed) | round-2 fixes unreviewed (last round); Sweep on a non-empty bucket UNVERIFIED (T030/T025) |
| T031 | CLOSED-WITH-GAPS | leg 1 + strong leg, 2 rounds (r1 REQUEST_CHANGES/CONCERNS, r2 REQUEST_CHANGES/CONCERNS; verified findings fixed) | round-2 fixes unreviewed (last round); flapping after a refusal and an allowed poll at the expiry left to T032/T026; known red residual_test.go (6 tests) for the coordinator's table |
| T033 | CLOSED-WITH-GAPS | leg 1 + strong leg, 2 rounds (r1 REQUEST_CHANGES/CONCERNS, r2 REQUEST_CHANGES/CONCERNS; verified findings fixed) | round-2 fixes unreviewed (last round); bucket URN form, S3 bucket as IAM resource and `:` in a tag key UNVERIFIED (T034/T027); known red urn_test.go (6 tests) for the coordinator's table |
| T035 | CLOSED-WITH-GAPS | leg 1 + strong leg, 2 rounds (r1 REQUEST_CHANGES/CONCERNS, r2 REQUEST_CHANGES/CONCERNS; verified findings fixed) | round-2 fix unreviewed (last round); fixtures composed from OpenTofu 1.13.0 source, live match T028's; test:live-lane registry input added; known red recovery_test.go (11 tests) for the coordinator's table |
| T037 | CLOSED-WITH-GAPS | leg 1 + strong leg, 2 rounds (r1 REQUEST_CHANGES/CONCERNS, r2 APPROVE/CONCERNS; verified r1 findings fixed, r2 deferred) | decision request for T038: Sweep when the root is kept (Undo or companion), recommended B (sweep in the final `--cleanup`); `--cleanup` of a run without state untested (fake destroy needs a state file); known red sweep_test.go (6 tests) for the coordinator's table |
| T038 | CLOSED-WITH-GAPS | leg 1 + strong leg, 2 rounds (r1 REQUEST_CHANGES/CONCERNS, r2 APPROVE/CONCERNS; verified findings fixed) | round-2 fix unreviewed (last round); tofu on a missing encrypted state UNVERIFIED (a no-state `--cleanup` that fails its destroy keeps its files); decision request: a root whose Undo passed and destroy failed is swept, recommended A (each probe's Undo tolerates swept resources, T030/T032/T034/T036); clears T037's sweep_test.go red |
| T030 | CLOSED-WITH-GAPS | leg 1 + strong leg, 2 rounds (r1 REQUEST_CHANGES/CONCERNS, r2 REQUEST_CHANGES/CONCERNS; verified findings fixed) | round-2 fixes unreviewed (last round); the root's `lz-check unit` line waits for T031/T033/T035 (ran via `test:unit`); Swift/S3 listing sharing, seed propagation, API object shapes UNVERIFIED (T025); clears T029's swiftreach_test.go red |

## Known red
| Test (file:name) | Story | Written in | Cleared in |
|---|---|---|---|
| tools/internal/probes/live/storageiam/swiftreach_test.go: TestJudgeReach, TestSwiftObserve, TestSwiftUndo, TestSwiftSweep, TestSwiftValidate, TestSwiftLeak; `test:live-lane` red | US2 swift-reach | T029 | T030 |
| tools/internal/probes/live/identity/residual_test.go: TestResidualWindow, TestResidualObserve, TestResidualUndo, TestResidualSweep, TestResidualValidate, TestResidualLeak; `test:live-lane` red | US3 token-residual | T031 | |
| tools/internal/probes/live/identity/urn_test.go: TestJudgeURN, TestURNObserve, TestURNUndo, TestURNSweep, TestURNValidate, TestURNLeak; `test:live-lane` red | US3 iam-urn | T033 | |
| tools/internal/probes/live/state/recovery_test.go: TestRecoveryLock, UnlockRefused, Blocked, TofuEnv, ConditionalPut, Restore, NoAnswer, Undo, Sweep, Validate, Leak; `test:live-lane` red | US2 locked-recovery | T035 | |
| tools/internal/live/sweep_test.go: TestSweepLostCreateAnswer, TestSweepAfterCrash, TestSweepKeepsRunFiles, TestSweepRemovesRunFiles, TestSweepWithUndo, TestSweepRedacted | US1 runner sweep | T037 | T038 |

## Test changes
| Test (file:name) | Written in | Changed in | Evidence it was wrong |
|---|---|---|---|
| tools/internal/probes/live/storageiam/swiftreach_test.go: swWorld (fake S3 and API) | T029 | T030 | not wrong, extended: the seed write the provider cannot do (tasks.md T030), 404/InvalidAccessKeyId after a Sweep and the API's object listing and bulk delete (coordinator decision 2026-10-16); no assertion changed |

## Live runs
| Task | Run id | Reviewed SHA | Observation | Cost | Leftovers |
|---|---|---|---|---|---|

## Lessons
- `test:live-lane` stops at its first red Go line: in a red window, later lines do not run; each task's final run must say which lines ran. (T029)
- A fake's hangup on a GET is undone by net/http's silent re-send: disable keep-alive and assert the request count. "Both directions" is scripted per direction; a Sweep case needs a resource outside the default region and a partial set. (T029)
- A case that ends in a stop asserts the stop was reached (Settle used up, deadline or expiry reached), not only an upper bound; compare the record with every fake answer one to one; with a fake clock, script one refused control so issue time and delete time differ. (T031)
- A fake's "acknowledged but not effective" mode covers every route a cleanup may use; blocked or no-target cases still require the controls and reads that should happen; one `t.Run` per variant; an `ALT-` row proves a correct alternative passes. (T033)
- An "acknowledged but ineffective" fake keeps plausible metadata and needs a matching-metadata, wrong-content variant; diagnostics composed from a tool's source follow its renderer (word wrap); a test that reads outside tools/ declares and pins its registry input in the same task. (T035)
- A "keep the files until X passes" rule needs a case where the run fails before X runs; a fake's tolerance added for one case is opt-in per stack; a mutant that deletes a condition keeps its variables used. (T038)
- Server text reaches errors through listings too: validate every listed name (regions, keys) before printing it, check an XML answer's root, and filter fields from a successful answer (role names) against every secret of the run, not only the caller's. A mutant that deletes a condition keeps its variables used. (T030)
- See spec 006's loop.md *Lessons*; they apply here, especially the entry command shape, whole-checkout scratch copies for live-package mutants, and vacuity checks on harness runs.
