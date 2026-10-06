# Loop — 005-first-landing-zone-slice

Repo: `landingzone-for-ovhcloud` · Branch: `worktree-001-foundation-loop` · Base: `main` (origin/main 363b65a) · Started: 2026-10-06 · Reviewers: no opencode on this host → per review `codex exec --sandbox read-only` (astra, D62) plus `fanout.sh --legs "claude/sonnet:diff"` (Opus on the claude-review triggers)
Task prompt: `~/.local/share/ovh-lz/review/loop-005/task-prompt.md` · Records: `~/.local/share/ovh-lz/review/loop-005/` (never committed)
Next: L / T053 (pairs with T052), then `task.sh next`. Out-of-order items the coordinator dispatches explicitly: 001/T024 + 001/T025 (entry admits `stacks/`, runtime revision published) before 005/T038; re-record `pipelines/github/foundation-runs.json` so `ci:foundation` turns green (T023 owner sheet) at the next landing.

## Stages
- [x] 0 orient — spec 005 created under D85/D87 (first real OpenTofu slice; spec 001 loop paused after T023, its remaining tasks postponed).
- [x] P plan + tasks — drafted and committed: 0d06c4a, revised c016a7d, 29edc69; ADR notes 8157e46, dfd14bd.
- [x] R spec-set review — round 1 on bfb4840: codex REQUEST_CHANGES, Opus BLOCK (3 CRITICAL; operator decisions D88); round 2 on dfd14bd: codex REQUEST_CHANGES (1 CRITICAL, ordering: bootstrap before the retained-resource guard — verified, fixed mechanically in 29edc69 under FR-011, no third round), Opus CONCERNS. 44 findings, none rejected; triage in `reviews/R-spec-set/triage.md`.
- [ ] L task loop — see *Tasks*
- [ ] B branch review — not run
- [ ] Hand-off — not reached

## Fitness functions
| Characteristic | Command (through the entry unless host-side) | Kind | Baseline | Per task / at B |
|---|---|---|---|---|
| ADR-0002 layering + purity (T005/T006 extend) | `task test:dependencies` | triggered | DEPENDENCIES_OK | every task touching modules/components/stages/stacks/tools |
| Traceability (per spec after T002) | `task check:specs` | triggered | TRACE_OK 001 r=15 t=23 c=22 | every tasks.md/harness change |
| Static HCL | `task lint -- <dir>`, `task test:static` | triggered | green on fixtures | every HCL task |
| Unit/slice tests (after T004) | `task test:unit`, `task test:slice` | triggered | — (created by T004) | every module/stage task |
| Generation freshness (after T038) | `task stacks:generate` + clean `git status` | triggered | — | every manifest/stage change |
| Foundation CI judge | `task test:foundation-ci` | triggered | green | .github/pipelines/harness changes |
| Mutation (security guards only, D85) | `~/.local/share/ovh-lz/review/mutate.py` / `mutate-entry.py` | sample | G1–G15 mutants killed | guard tasks |

## Triage (spec set)
See `~/.local/share/ovh-lz/review/loop-005/reviews/R-spec-set/triage.md`. Coordinator choices in round 2: project state in the tenant bucket recorded as KD-3 (adapter refuses a project artefact for another account); account binding via `GET /auth/details` (new premise P26, fallback grants `account:apiovh:me/get`).

## Tasks
| Task | Verdict | Review (legs, rounds, Claude?) | Gaps / decision request |
|---|---|---|---|
| T001 | CLOSED | codex leg not run (usage limit) → general-purpose subagent APPROVE; Sonnet CONCERNS; Opus CONCERNS (codex-no-verdict trigger); 1 round; review fixes added 4 controls | none; T002 must also edit tools/cmd/lz-check/main.go (not in its file list) |
| T002 | CLOSED | codex not run (limit) → subagent APPROVE; Opus CONCERNS (codex-unavailable); 1 round; lint/SC-001 dropped, ITEM quoted, spec-set edits split into e91c86e | owner validates tasks.md form edits (e91c86e) and `verify:toolchain`→`005/FR-011`; astra owed |
| T003 | CLOSED | codex not run (limit) → subagent REQUEST_CHANGES (lint-ignoring slice mutant survived); Opus CONCERNS (codex-unavailable); 1 round, coverage fixes, mutant killed | T004 must put `TestUnit*` in a Task target; astra owed |
| T004 | CLOSED-WITH-GAPS | codex not run (limit) → subagent APPROVE (P2: per-directory `blocked` unpinned, fixed); Opus CLEAN (code diff, codex-unavailable); 1 round | `test:slice` red by design (NO_DISCOVERY) until T012; tagged controls not in CI until then + CI source extension (owner); `ci:foundation` stale since T001 (re-record after push); astra owed |
| T005 | CLOSED | codex not run (limit) → subagent REQUEST_CHANGES (`.tofu` bypass); Opus CONCERNS (code diff, codex-unavailable; `stacks/**/tests` escape, Subject unchecked); 1 round, coverage fixes, 9+6 mutants killed | `test:dependencies`/`test:traceability` red until T006; T006 chooses `.tofu` → UNSUPPORTED_CONFIG or parse; astra owed |
| T006 | CLOSED-WITH-GAPS | codex not run (limit) → subagent APPROVE (surviving `check`-overwrite mutant, row added); Opus CONCERNS (code diff, codex-unavailable; header-forged instance outside `stacks/`, pre-existing `classify`); 1 round, 16/16 mutants killed | `.tofu` → UNSUPPORTED_CONFIG; foundation CI targets green again (push point); decision request: pre-existing purity escapes (`fixtures` anywhere, package named `tests`/`examples`), Rec. A follow-up task; astra owed |
| T066 | CLOSED | codex not run (limit) → subagent APPROVE (surviving `tools`-segment mutant, row added); Opus CONCERNS (test-code diff, codex-unavailable; refuse-only `.tofutest.*` made explicit, hidden-dir skip); 1 round, 22 red rows, 14/14 mutants of throw-away impl killed | `test:dependencies`/`test:traceability` red until T067 (no push); T067 may start from `t066-impl-dependencies.go`; hidden dirs skipped anywhere = possible route 4 (coordinator); astra owed |
| T067 | CLOSED | codex not run (limit) → subagent REQUEST_CHANGES r1 (hidden-ancestor symlink + coverage), APPROVE r2; Opus CONCERNS both rounds (code diff, codex-unavailable; r1 tests/examples outside package roots, r2 root-tree helpers demoted — both verified, fixed); 2 rounds, 23/23 mutants killed | push point (all CI targets green); route 4: called hidden dirs judged, unreferenced skipped (counterpoint in evidence); "root-module tests only" premise UNVERIFIED; astra owed |
| T052 | CLOSED-WITH-GAPS | codex not run (limit) → subagent REQUEST_CHANGES (G12 prefix-filter env survives; git-failure admit; `/me` fallback); Opus CONCERNS (test-code diff, codex-unavailable; `/me` fallback, `showUntrackedFiles=no` config) — all verified, fixed as coverage rows; 1 round, 47 red rows, 35/35 mutants of throw-away impl killed | no Task target runs `./internal/live` until T053/T055 (`test:live-lane`), lz-offline exec-from-tmp/HOME unverified; platform-deployer policy unspecified in R6; guard's git env (`GIT_*`) scrub proposed for T053; T053 may start from `t052-impl/`; astra owed |
| T053 | CLOSED-WITH-GAPS | codex not run (limit) → subagent REQUEST_CHANGES r1 (clean tree passes with index bits / `core.worktree`, verified with real git), APPROVE r2; Opus CONCERNS both rounds (security code, codex-unavailable; r1 `live.env` mode, open dirs; r2 `PATH=x` key, token redirect) — all verified, fixed; 2 rounds, 79/80 mutants killed (1 equivalent) | push point (all CI targets green); `test:live-lane` defined, runs through the entry, CI enforcement needs creator move (decision 1, Rec. T053) or T055; child `HOME` (decision 2); shared `.git`/same-uid boundary (decision 3); astra owed |
| T054 | CLOSED-WITH-GAPS | codex not run (limit) → subagent REQUEST_CHANGES r1 (5 own mutants survived: non-apply stream redaction, runner-level incremental inventory, HOME across runs, passphrase argv, cleanup re-apply), APPROVE r2; Opus CONCERNS both rounds (test code for security guards, codex-unavailable) — all verified, fixed; 2 rounds, 83 red controls on stubs, throw-away green (host + entry), 74/74 mutants killed | NOT a push point: `test:live-lane` red until T055; T055 may start from `t054-impl/`; decisions: R12 tag path (`GET /iam/resource/{urn}`), graceful SIGINT to tofu (Rec. T055); gaps: P24 formal capture (T007), fallback ids, redaction scope; astra owed |
| T063 | CLOSED-WITH-GAPS | codex not run (limit) → subagent APPROVE r1 (minors incl. `plan -replace`, keyed module, sidecar name — verified behaviour gaps) and APPROVE r2 (keyed module address as entry, fixed); Opus CONCERNS r1 (keyed module, `errored`), CLEAN r2 (security-guard test code, codex-unavailable); 2 rounds, 24 red rows on the stub, 27/27 mutants of throw-away killed | T055 BLOCKED on T064 (dispatch); NOT a push point (`test:live-lane` red until T064/T055); fixtures captured with pinned tofu outside capture admission (T007 owes); T064 may start from `t063-impl/`; empty retained entry covers nothing (unpinned); astra owed |

## Owner tasks (skipped by the loop)
| Task | Command sheet |
|---|---|
| T009 | read-only/plan-only probes + `ovhcloud` listings — run early; T065 and T046 need its captures |
| T010 | create→destroy probes through `lz-live probe` (after T055, T065) |
| T044, T045 | bootstrap current account twice; fresh account |
| T049 | `task live:chain -- all` |

## Lessons
- Run Task targets through the entry: `~/.local/share/ovh-lz/003-runtime-r3-e2/lz-offline --candidate <absolute worktree> -- task <target>`; `check:specs` takes no args and traces spec 001 until T002 lands. Host-side targets (`stacks:*`, `generate:*`) run directly.
- The worktree guard refuses compound commands, `$(…)`, heredocs mentioning git, paths containing `github`, and `-run 'A|B'` regexes: put them in a script under `~/.local/share/ovh-lz/review/` written with the Write tool (Read the file first if it exists).
- Red controls must show failed assertions (`--- FAIL:`), not just a non-zero exit (T023).
- Owner/SUPERSEDED markers only count in the task TITLE line; never strike through task ids.
- Agents never call the OVHcloud API or read `~/.config/ovh-lz/`; live behaviour is tested with fakes and T009's captured listings.
- Mutation proof only for security guards (D85); plain red/green TDD elsewhere.
- `lz-offline` accepts only `task <target>`: run `go -C tools test …` Verify lines on the host and also run the task target that covers them through the entry (T001).
- Codex is at its usage limit until 2026-10-11: review leg 1 is a general-purpose subagent, leg 2 Opus; the astra review is owed per task and the D62 merge bar is not met until it runs (T001).
- Do not push between a test task and its implementation: the red tests make `test:traceability`/CI red (T001→T002).
- T002 must also change `tools/cmd/lz-check/main.go` (set `Trace.Spec` from the directory) and fix `ParseSpec` for spec 005's `- **FR-0NN Name** —` headings, otherwise the 14 FRs are dropped silently (T001). Done in T002.
- `task check:specs` now traces 001 and 005 by default (005: r=19 t=65 c=20); every tasks.md edit must keep it green (T002).
- A check's creator is the task that creates it if its Verify runs it, else the first task whose Verify runs it (T002).
- `mutate.py` counts occurrences from 0: all-NOT_FOUND means an off-by-one, not a pass (T002).
- The fanout Opus leg runs with Bash denied: put sensor results in the review brief (T002).
- A test task proves its red can be satisfied with a throw-away implementation in a scratch copy (not committed); reviewers mutate it, and the implementation task may start from it (T003: `~/.local/share/ovh-lz/review/t003-impl-unit.go`).
- An aggregate's control needs a member that fails exactly one aggregated clause, or an implementation ignoring that clause passes (T003).
- Every new Go test must run under some Task target, or CI never enforces it; the implementation task wires it (T003→T004 `test:unit`).
- Until T012 turns `test:slice` green, its exit code says nothing about the controls: read step 1's `ok` lines (the `TestUnit` controls run first) (T004).
- `task test:unit -- <fixture dir>` through the entry is a cheap positive control (T004).
- Every nested library/stage directory with `*.tf` is a slice member and needs its own `.tftest.hcl` (T004, applies from T012).
- Queued for after T012: add `test:slice` to `pipelines/github/foundation-source.json` once it is green (changes the recorded runs; coordinator task line then).
- OpenTofu also reads `*.tofu`: any check routing files by extension needs a `.tofu` control (T005).
- `classify` lets `tests`/`examples` segments win anywhere in a path: a rule scoped to `stacks/` needs controls with those segments inside `stacks/` (T005).
- `mutate.py` copies only `tools/`; tests reading `../../../tests/check/fixtures` then find nothing and every mutant falsely dies — mutate a scratch copy holding `tools/` and `tests/` (`~/.local/share/ovh-lz/review/t005-mutants.py`) (T005).
- Landing (D89/D90): the coordinator pushes green points, opens a PR, rebase-merges on all-green checks (SonarCloud "analysis failed" excepted until fixed), then rebases this branch onto origin/main. PR #22 merged 2026-10-06 (main 49094ef). Queued: re-record `pipelines/github/foundation-runs.json` from the PR #22 / main runs so `ci:foundation` turns green (T023 owner sheet).
- Coordinator: run `task.sh next` before every dispatch — T055 was dispatched while its dependency T063/T064 was open (caught, redirected, nothing committed).
- Write mutants per task-text clause, not per implementation line; a "per run" guard needs two runs in the control; fakes for stateful CLIs trap signals and log how they were stopped (T054).
- An address guard needs a prefix-sibling row and a keyed-instance row at every level (resource, module, module key); judge plan changes by `actions`, never `action_reason`; bind a digest sidecar to its file name; capture scripts never `rm -rf` a caller-supplied path (T063).
- Coordinator validations after T002 (2026-10-06): e91c86e (form-only spec-set edits) accepted; `verify:toolchain` stays mapped to 005/FR-011 so T047's "toolchain unchanged" gate keeps tracing.
