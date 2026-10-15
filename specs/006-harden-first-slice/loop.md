# Loop — 006-harden-first-slice

Repo: `PlatformRelay/landingzone-for-ovhcloud` · Branch: `worktree-001-foundation-loop` · Base: `origin/main` · Started: 2026-10-15 · Reviewers: two-model (review leg 1 + the strong review leg), duo rules: one leg per task, a second only on the triggers
Next: L — dispatch the first open task (`task.sh next`)
Budget: strong review legs 1/12 (B reserve 3) · active hours — · default

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

## Tasks
| Task | Verdict | Review (legs, rounds, strong leg?) | Gaps / decision request |
|---|---|---|---|
| T001 | CLOSED; known red `TestTaskfileTracesEverySpec` (3 subtests) until T002 | leg 1, 2 rounds (REQUEST_CHANGES ×2, all verified findings fixed), no strong leg (no trigger) | none |
| T002 | CLOSED; known red `TestTaskfileTracesEverySpec` cleared | leg 1 APPROVE + strong leg CLEAN (trigger: checks.yaml), 1 round | `foundation-runs.json` stale: coordinator refresh |

## Known red
| Test (file:name) | Story | Written in | Cleared in |
|---|---|---|---|
| tools/cmd/lz-check/main_test.go:TestTaskfileTracesEverySpec (check:specs_default, ci-source_specs, foundation-ci_inputs); `test:traceability` red | Setup | T001 | |

## Test changes
| Test (file:name) | Written in | Changed in | Evidence it was wrong |
|---|---|---|---|

## Lessons
- The entry admits only `task <target> [-- <path>]`; run it from $HOME, because mise shims refuse the untrusted worktree mise.toml. (spec R1, 005)
- `test:live-lane` is not the whole CI: run `docs:check`, and re-pin pages whose `references:` name a file you changed. (005 T102)
- No review tool or model names in committed files (D111). (005)
- A test that parses config as text passes vacuously unless it has controls for a commented-out line, an inline comment and a matching line in another block; write them with the test, before review. (T001)
- Known red rows in this file are the coordinator's; report them, do not edit this table. (T001)
- Any change to checks.yaml, specs or targets makes `pipelines/github/foundation-runs.json` stale; that is the coordinator's refresh, not the task's. (005)

## Where a human should look first

## Proposed harness changes
| Lesson | Seen in | Proposed change |
|---|---|---|

## Owner tasks (skipped by the loop)
| Task | Command sheet |
|---|---|
