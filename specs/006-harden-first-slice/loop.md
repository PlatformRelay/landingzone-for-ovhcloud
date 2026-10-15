# Loop — 006-harden-first-slice

Repo: `PlatformRelay/landingzone-for-ovhcloud` · Branch: `worktree-001-foundation-loop` · Base: `origin/main` · Started: 2026-10-15 · Reviewers: two-model (review leg 1 + the strong review leg), duo rules: one leg per task, a second only on the triggers
Next: L — dispatch the first open task (`task.sh next`)
Budget: strong review legs 8/20 (B reserve 3; 9 left for L) · active hours — · owner D116 (2026-10-15): cap 20; duo triggers only (secrets or credentials, CI, infrastructure or manifests, security posture, leg 1 BLOCK); a second round re-runs the strong leg only when it raised the defect. T002, T004 and T047 ran it on wider triggers carried over from spec 005.

## Stages
- [x] 0 orient — feature dir created by hand from the templates; spec, plan, research, data model, contracts, quickstart and tasks present; base origin/main
- [x] P plan + tasks — generated with the repository's Spec Kit commands · spec set committed: the spec commit ":memo: docs(specs): specify spec 006"
- [x] R spec-set review — round 1 on c604b97: review leg 1 REQUEST_CHANGES (10 major), the strong review leg CONCERNS (5 warnings, 4 notes); no CRITICAL. 13 verified findings fixed (462374a). Owner decisions D115 (Q4, Q5, Q7) applied (cb3a47e). A stage-R warning followed: the combined test-and-implementation tasks were split into test tasks ordered first (the spec commit ":memo: docs(specs): specify spec 006"). No second round: there was no accepted CRITICAL.
- [ ] L task loop — see *Tasks*
- [ ] B branch review — not started
- [ ] Hand-off — not started

## Fitness functions
| Characteristic | Command (through the entry unless host-side) | Kind | Baseline | Per task / at B |
|---|---|---|---|---|
| ADR-0002 layering and purity | `task test:dependencies` | triggered | DEPENDENCIES_OK dirs=37 | every task touching tools/, modules, stages or stacks |
| Traceability | `task check:specs` (001, 005, then 006 after T001) | triggered | TRACE_OK 005 r=19 t=112 c=21 | every tasks.md, spec or harness change |
| Registry freshness (inputs cover reads) | `task test:traceability` | triggered | green | every checks.yaml change, and every new read by a check |
| Foundation CI judge | `task test:foundation-ci` | triggered | FOUNDATION_CI_OK targets=18 deferred=1 | .github/, pipelines/, Taskfile or harness changes |
| Docs gates | `task docs:check` | triggered | DOCS_OK pages=39 | every docs change, and every change to a file a page pins |
| Generated stacks fresh | `task stacks:check` | triggered | STACKS_CHECK_OK stacks=6 | manifest or stage changes |
| Offline boundary | `task test:offline-boundary` | triggered | BOUNDARY_QUALIFIED | offline-entry changes |
| Mutation (security guards) | `~/.local/share/ovh-lz/review/mutate.py` / `mutate-entry.py` | sample | mutants named per guard in research G-rows | guard tasks |
| Whole CI | all 18 `LZ_TARGETS` | holistic | all green at 234a88d (local r4-e2 run) | final tree of each code task; at B |

Holistic run at B: not run yet.

## Triage
| Stage | Finding (one line) | Sev | Models | Disposition | Where it went / why | Needs user? |
|---|---|---|---|---|---|---|
| R1 | FR-002 claimed the destroy budget ends with the run; it is already detached, and the gap is a budget below DefaultDeadline | major | both | fix | FR-002, T010 reworded | no |
| R1 | FR-005 per-file rename loses the old set on a crash | major | leg 1 | fix | generation directories plus a `current` pointer | no |
| R1 | Verify steps ran `go test` through the entry, which refuses it | major | leg 1 | fix | every Verify names a Task target | no |
| R1 | Ordering of T021/T022 and T025/T027 | major | leg 1 | fix | reordered and merged | no |
| R1 | Release manifest needs .git inside the entry | major | leg 1 | fix | fixture repository; real HEAD is a host step | no |
| R1 | The repository map judges paths the entry cannot see | major | leg 1 | fix | narrowed, with MAP_UNJUDGED | no |
| R1 | Owner choices treated as approved (constitution VIII) | major | leg 1 | fix | owner decided, D115 | done |
| R1 | Renovate manager names need external docs | major | leg 1 | fix | recorded UNVERIFIED; the check judges the repository's own rules | no |
| R1 | Registry inputs miss reads (foundation-ci tasks, docs:check pins) | major | both | fix | T001, T025, T026 | no |
| R1 | Plan/destroy deadline controls missing; T003 named the wrong unsafe case | major | both | fix | T008, T003 | no |
| R1 | The bundle/checks import cycle for pins | warning | strong | fix | single table in `checks` | no |
| R1 | The TF_VAR allow-list admitted reserved names; the neighbours subtest must invert | warning | strong | fix | FR-006, T007 | no |
| R1 | "Two minors behind" contradicted ADR-0013 | warning | both | fix | more than two, everywhere | no |
| R1 | T001's control could not fail | note | strong | fix | TestTaskfileTracesEverySpec | no |
| R1 | Repository-map committed path inventory (proposed fix) | — | leg 1 | reject | the author: a generator by another name; a stale inventory hides drift | no |
| R | Combined test-and-implementation tasks (spec-loop P rule) | warning | coordinator | fix | split, test tasks first | no |
| L T004 | `test:traceability`'s inputs miss the task lists its judge reads | minor | review leg 1 | fix | T047 | no |
| L T007 | A writer that unlinks `current` then renames `current.tmp` passes every step-boundary check; real `fsync` is invisible to crash injection | minor | review leg 1 | defer | T013's review must check both (G18) | no |
| L T008 | No case pins "resolve `current` once per load" | minor | review leg 1 | defer | T014's review checks one `current` read per load | no |
| L T011 | A timeout while reading a 200 body is reported as "does not decode" | minor | review leg 1 | defer | stage B (no leak) | no |
| L T011 | The leftover lister reports a hostile 3xx as "request failed" | minor | review leg 1 | defer | stage B (outside FR-003) | no |
| L T011 | Account-mismatch refusal no longer shows the answered account id (a hostile answer could carry secret text) | — | task | open | owner asked 2026-10-15 | yes |
| L T047 | `test:traceability`'s tests also read files not in its inputs (`pipelines/github/foundation-source.json`, `.github/workflows/foundation.yml`, READMEs read by `docs_test.go`); stale `tasks.md` inputs are not flagged | minor | strong | defer | stage B: one sweep of non-Go reads for every check, instead of a task per discovery | no |

## Tasks
| Task | Verdict | Review (legs, rounds, strong leg?) | Gaps / decision request |
|---|---|---|---|
| T001 | CLOSED; known red `TestTaskfileTracesEverySpec` (3 subtests) until T002 | leg 1, 2 rounds (REQUEST_CHANGES ×2, all verified findings fixed), no strong leg (no trigger) | none |
| T002 | CLOSED; known red `TestTaskfileTracesEverySpec` cleared | leg 1 APPROVE + strong leg CLEAN (trigger: checks.yaml), 1 round | `foundation-runs.json` stale: coordinator refresh |
| T003 | CLOSED; known red `TestRegistryInputsCoverGoDeps` (go_deps_covered: 57 rows; traceability_reads_the_registry) until T004 | leg 1, 2 rounds (REQUEST_CHANGES ×2, all verified findings fixed), no strong leg (no trigger) | none |
| T004 | CLOSED; known red `TestRegistryInputsCoverGoDeps` cleared | leg 1 APPROVE + strong leg CONCERNS (trigger: checks.yaml; no finding against the diff, one verified out-of-scope gap deferred), 1 round | `test:traceability` inputs omit the `specs/*/tasks.md` its judge reads: proposed task; `foundation-runs.json` stale: coordinator refresh |
| T047 | CLOSED; own red recorded and cleared in-task, no known red | leg 1 APPROVE + strong leg CLEAN (trigger: checks.yaml; notes only), 1 round | other non-Go reads of `test:traceability` unpinned: proposed task; `foundation-runs.json` stale: coordinator refresh |
| T005 | CLOSED; known red `TestBindingErrorsQuiet` (4 subtests), `TestProbeBindingErrorQuiet` (2 subtests); `test:live-lane` red until US1's FR-003 implementation | leg 1 + strong leg (trigger: live lane), 2 rounds (round 1 REQUEST_CHANGES + CONCERNS, round 2 REQUEST_CHANGES + CLEAN; all verified findings fixed) | FR-003 "next action" has no case: proposed for the implementation task |
| T006 | CLOSED; known red `TestChainLeftoverTokenRedacted` (4 subtests) until T012 | leg 1 + strong leg (trigger: live lane), 2 rounds (round 1 REQUEST_CHANGES + CONCERNS: one verified behaviour finding fixed; round 2 APPROVE + CLEAN) | token-name prefix collision at 10+ admin tokens: deferred to T012 |
| T007 | CLOSED; known red `TestFilesCredentialSetWriter` (7 subtests) until T013 | leg 1 only (no trigger: tests plus a no-behaviour stub), 2 rounds (round 1 REQUEST_CHANGES: two verified behaviour findings fixed; round 2 APPROVE, two evidence minors fixed) | fsync and single-rename atomicity are invisible to step-boundary crash injection: for T013 review |
| T008 | CLOSED; known red `TestCredentialSetPathResolves` (3), `TestCredentialSetPathPointerInvalid` (8), `TestCredentialsFromSets` (2 of 3) until T014, `TestCredentialSetWriteKeepsBinding` until T013 | leg 1 only (no trigger: tests, a no-behaviour stub and a docs pin), 1 round (APPROVE, one wording minor fixed) | "resolve `current` once per load" has no case: for T014 review |
| T009 | CLOSED; known red `TestEnvCommandTakesHome` until T015; pins `TestEnvChildHomeEveryPath` (5) and `TestBootstrapStateChildHome` green | leg 1 only (no trigger: tests and a no-behaviour stub), 2 rounds (round 1 REQUEST_CHANGES: one verified behaviour finding, a vacuous probe pin, fixed; round 2 APPROVE) | none |
| T010 | CLOSED; known red `TestAdmitCompanionName`, `TestProbeIdentityReservedNames` (`unlisted/*` ×17, `cleanup/refused-unlisted`), `TestCompanionPairs` (`refused/s3-access-alone`, `refused/s3-secret-alone`) until T016; `passed/neighbours` inverted | leg 1 only (no trigger: tests and a no-behaviour stub), 1 round (APPROVE, no findings) | S3 pair with an empty value and an absent OAuth2 pair not pinned (outside FR-006's text) |
| T011 | CLOSED; known red `TestBindingErrorsQuiet` (4), `TestProbeBindingErrorQuiet` (2) cleared; new `TestBindingErrorsActionable` red before the fix, green after | leg 1 + strong leg (trigger: secret handling in error paths), 2 rounds (round 1 REQUEST_CHANGES + CLEAN: three verified behaviour findings fixed; round 2 leg 1 only, REQUEST_CHANGES: two verified findings fixed without review) | mismatch refusal no longer quotes the answered account id; body-read timeout class and leftover lister 3xx class deferred to B |
| T012 | CLOSED; known red `TestChainLeftoverTokenRedacted` (4) cleared; new `TestRunnerLeftoverTokenRedacted` (2) red before the fix, green after | leg 1 + strong leg (trigger: secret handling), 2 rounds (round 1 APPROVE + CONCERNS: the probe Runner's check unregistered, fixed in `runner.go`; round 2 REQUEST_CHANGES + CLEAN: caller-set `Red` bypassed the run's redactor, fixed without review) | none; token-suffix prefix collision not touched (no admin token added) |

## Known red
| Test (file:name) | Story | Written in | Cleared in |
|---|---|---|---|
| tools/cmd/lz-check/main_test.go:TestTaskfileTracesEverySpec (check:specs_default, ci-source_specs, foundation-ci_inputs); `test:traceability` red | Setup | T001 | T002 |
| tools/cmd/lz-check/main_test.go:TestRegistryInputsCoverGoDeps (go_deps_covered: 57 rows; traceability_reads_the_registry); `test:traceability` red | Setup | T003 | T004 |
| tools/internal/live/hardening_test.go:TestBindingErrorsQuiet (token-redirect, auth-details-redirect, token-undecodable, auth-details-undecodable); tools/cmd/lz-live/probe_test.go:TestProbeBindingErrorQuiet (token, auth-details); `test:live-lane` red | US1 | T005 | T011 |
| tools/internal/live/chain_test.go:TestChainLeftoverTokenRedacted (token-as-id, token-inside-longer-id, token-split-across-writes, registered-before-first-listing); `test:live-lane` red | US1 | T006 | |
| tools/internal/live/files_test.go:TestFilesCredentialSetWriter (fresh-write, crash-after-first-file, crash-after-files, crash-after-dir-sync, crash-after-pointer-tmp, crash-after-switch, keeps-current-and-previous); `test:live-lane` red | US1 | T007 | |
| tools/internal/live/credentials_test.go:TestCredentialSetPathResolves (3), TestCredentialSetPathPointerInvalid (8), TestCredentialsFromSets (generation-after-switch, each-writer-its-own-pointer) — cleared by T014; TestCredentialSetWriteKeepsBinding — cleared by T013; `test:live-lane` red | US1 | T008 | |
| tools/internal/live/env_test.go:TestEnvCommandTakesHome; `test:live-lane` red | US1 | T009 | |
| tools/internal/live/probe_identity_test.go:TestAdmitCompanionName; TestProbeIdentityReservedNames (unlisted/* ×17, cleanup/refused-unlisted); TestCompanionPairs (refused/s3-access-alone, refused/s3-secret-alone); `test:live-lane` red | US1 | T010 | |

## Test changes
| Test (file:name) | Written in | Changed in | Evidence it was wrong |
|---|---|---|---|
| tools/internal/live/probe_identity_test.go:TestProbeIdentityReservedNames/passed/neighbours | before spec 006 | T010 | FR-006 (D115 allow-list): names outside `TF_VAR_` were asserted admitted; now refused `unlisted/*` rows |
| tools/internal/live/hardening_test.go:TestEnvCredsCannotReplacePathOrHome; tools/internal/live/env_test.go:TestEnvChildFromScratch | before spec 006 | T009 | not wrong: call sites follow the `Command(a, creds, home, name, arg...)` signature data-model fixes; no claim changed |

## Lessons
- The entry admits only `task <target> [-- <path>]`; run it from $HOME, because mise shims refuse the untrusted worktree mise.toml. (spec R1, 005)
- `test:live-lane` is not the whole CI: run `docs:check`, and re-pin pages whose `references:` name a file you changed. (005 T102)
- No review tool or model names in committed files (D111). (005)
- A test that parses config as text passes vacuously unless it has controls for a commented-out line, an inline comment and a matching line in another block; write them with the test, before review. (T001)
- A test that parses shell commands out of YAML must read every command form (block and quoted scalars, import-path packages, redirections), or count the `go` words against the invocations it read and fail on a gap. (T003)
- When waiting for a background review leg, wait on its output file, not a pgrep match: other sessions run review processes too. (T001)
- A partial-leak check needs a test secret with no English words in it; Go's JSON decode errors quote at most 4 characters of the body, so check fragments, not the whole secret. (T005)
- `mutate.py` copies only `tools/`; the live and lz-live tests read `../../../stacks` and `mise.toml`, so mutate them in a whole-checkout scratch copy. (T005)
- A redaction test needs the token to differ from every other token of the same credential; a fake issuing one token per credential lets a wrong-token fix pass. A split-write test through a child process cannot force two reads: pair it with an in-process hold-back test. (T006)
- A crash-window test must compare the pointer file's identity across the switch (`os.SameFile`), or a rewrite in place passes; a retention test must read the kept generation, not list it; crash injection proves ordering, not durability. (T007)
- A reader crash-window fixture lays a partial newer generation and a stale `current.tmp` beside a valid `current`; give each set a stale generation under the other writer's name so a shared-pointer bug reads wrong values instead of failing. A stub in a file a docs page pins is also a docs edit (re-pin it). (T008)
- A pin over a harness run checks that the run succeeded and the verb did its work (never `_, _ = execute(...)`); a caller-HOME leak check searches every variable's value for the sentinel; `plan -- all` on a fresh lane harness exits 2 by design. (T009)
- An allow-list test needs, per reserved name, a row with the exact prefix and the rest in another case; refusal rows run through the same closures as existing refused rows and check the error names the refused name. (T010)
- A field the answer chose is a body quote too: stop echoing it instead of bounding it. Before changing an error's type, grep for `errors.As` checks and text matches on it. (T011)
- Known red rows in this file are the coordinator's; report them, do not edit this table. (T001)
- Any change to checks.yaml, specs or targets makes `pipelines/github/foundation-runs.json` stale; that is the coordinator's refresh, not the task's. (005)

## Where a human should look first

## Proposed harness changes
| Lesson | Seen in | Proposed change |
|---|---|---|

## Owner tasks (skipped by the loop)
| Task | Command sheet |
|---|---|
