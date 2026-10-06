# Loop — 005-first-landing-zone-slice

Repo: `landingzone-for-ovhcloud` · Branch: `worktree-001-foundation-loop` · Base: `main` (origin/main 363b65a) · Started: 2026-10-06 · Reviewers: no opencode on this host → per review `codex exec --sandbox read-only` (astra, D62) plus `fanout.sh --legs "claude/sonnet:diff"` (Opus on the claude-review triggers)
Task prompt: `~/.local/share/ovh-lz/review/loop-005/task-prompt.md` · Records: `~/.local/share/ovh-lz/review/loop-005/` (never committed)
Next: L / `task.sh next` (expected T001).

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
