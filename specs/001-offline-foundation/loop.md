# Loop — 001-offline-foundation

Repo: `landingzone-for-ovhcloud` (local dir `ovh-landing-zone-accelerator`) · Branch: `001-foundation-loop` · Base: `main` (origin/main 363b65a) · Resumed: 2026-10-06 · Reviewers: no opencode on this host → per review leg 1 `codex exec --sandbox read-only` (astra, as D62 requires), leg 2 `fanout.sh --legs "claude/sonnet:<lens>" --no-unify`; Opus leg on the claude-review triggers
Next: L / T023 — remaining `task ci:foundation` (judge the recorded GitHub run evidence) and evidence/T023.md; then `task.sh next`.

Earlier history (2026-10-02 to 10-05: T001–T009 and the T023 implementation half, merged through PRs #10–#20) lives in git, the evidence files and `agent-context/decisions.md` (D58–D66); this file restarts from the merged state.

## Stages
- [x] 0 orient — feature from the handover (feature.json on the stale `review` checkout points at 004; 001 is the active feature, T023 in flight). Spec, plan, tasks present. Worktree `worktrees/ovh-landing-zone-accelerator/001-foundation-loop` from origin/main.
- [x] P plan + tasks — skipped: present.
- [x] R spec-set review — stands in: the spec-set amendments since 2026-10-02 were each reviewed with their PRs (astra codex rounds, PRs #10–#20); the only change since is 53b3d24 (ADR-0003 accepted, other-cloud references removed, operator instruction D66), docs-only; `task check:specs` TRACE_OK requirements=15 tasks=23 checks=22 on 53b3d24 (2026-10-06, through the r3-e2 entry). No fresh fan-out, to spare credit.
- [ ] L task loop — see *Tasks*
- [ ] B branch review — not run
- [ ] Hand-off — not reached

## Fitness functions
| Characteristic | Command (through the entry) | Kind | Baseline | Per task / at B |
|---|---|---|---|---|
| ADR-0002 layering, reference and changed-closure rules | `task test:dependencies` | triggered | no violations on this repo's own graph | per task when `.tf`/module/tools change; B |
| Requirement/task/check traceability | `task check:specs` | triggered | TRACE_OK r=15 t=23 c=22 | per task (every tasks.md/harness change); B |
| Definition of done per path | `task dod -- <path>` | triggered | — | per task on touched paths |
| Static HCL (fmt/init/validate/TFLint 0.64.0) | `task test:static`, `task lint -- <module>` | triggered | green on fixtures; `lint -- modules/naming` not-run until T014 | per task touching HCL; B |
| Foundation CI source/rendered workflow | `task test:foundation-ci` | triggered | green on main | per task touching .github/pipelines/harness; B |
| Mutation evidence (D62) | `~/.local/share/ovh-lz/review/mutate.py` / `mutate-entry.py` | holistic sample | every guard has a killed mutant | per code task, representative sample |

No golangci/arch-lint config exists; `go vet`/`gofmt` are the only Go linters available. Holistic run at B: not run yet.

## Triage
| Stage | Finding (one line) | Sev | Models | Decision | Where it went / why | Needs user? |
|---|---|---|---|---|---|---|

## Tasks
| Task | Verdict | Review (legs, rounds, Claude?) | Gaps / decision request |
|---|---|---|---|
| T001–T009 | CLOSED (merged) | per-PR astra rounds | see evidence/T00*.md |
| T023 | CLOSED-WITH-GAPS | codex + Opus, 2 rounds (Opus: CI/harness diff); r1 REQUEST_CHANGES fixed, r2 REQUEST_CHANGES on registry digest coverage → decision | owner: GitHub run of the final head + record-only commit; decision: registry scope/inputs (evidence/T023.md) |
| T024 | CLOSED-WITH-GAPS (test task: 5 tests red until T025) | codex + Opus, 2 rounds (Opus: offline entry tests); r1 REQUEST_CHANGES (3 test-isolation/coverage findings) fixed, r2 codex APPROVE / Opus CONCERNS | no target runs `./cmd/lz-offline`; decisions: T025 Verify + CI wiring, land T024+T025 together (evidence/T024.md) |

## Owner tasks (skipped by the loop)
| Task | Command sheet |
|---|---|
| T012 | needs disposable GitHub and GitLab repos, protected base workflow, scoped publisher, runners — external |
| T021 (and T022 behind it) | needs an exclusive self-hosted runner with fixed CPU/memory — external |

## Lessons
- Run every Task target through the installed entry: `~/.local/share/ovh-lz/003-runtime-r4/lz-offline --candidate <absolute worktree> -- task <target>`; redirect output to a file under the job tmp dir (/tmp tmpfs quota fills; Bash output can be lost).
- If the entry/runtime must change, rebuild per `evidence/T003.md` and the memory note: `go -C tools build -trimpath -ldflags '-X main.manifestSHA=<sha>' -o <runtime>/lz-offline ./cmd/lz-offline`; never edit the published r3-e2 bundle in place.
- D62: a passing result needs mutation evidence (neutralise the guard → suite red). Runners: `~/.local/share/ovh-lz/review/mutate.py` (host) and `mutate-entry.py` (through the entry).
- Keep helper scripts and review briefs in `~/.local/share/ovh-lz/review/` (session scratch dirs vanish); write them with the Write tool, not heredocs (the worktree guard refuses heredocs mentioning git).
- Coordination files (`agent-context/`) are gitignored and live in the main checkout; product files only in the worktree.
- No cloud resources, no credential use, no global host security change (D62 preserved bounds).
