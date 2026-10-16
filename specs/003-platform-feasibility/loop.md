# Loop — 003-platform-feasibility (probe lane)

Repo: `PlatformRelay/landingzone-for-ovhcloud` · Branch: `lane-003-probes` (worktree `003-probes`) · Base: `origin/main` · Started: 2026-10-16 · Reviewers: two-model (review leg 1, plus the strong review leg on triggers); duo rules under D117
Scope: the four live probes only — test task, implementation task, then the live run: T029→T030→T025, T031→T032→T026, T033→T034→T027, T035→T036→T028. Tasks T001–T024 are out of this lane.
Next: R — review of the amendment commit 3f610df
Budget: strong review legs are counted in spec 006's loop.md against D117's cap of 120.

## Stages
- [x] 0 orient — worktree from origin/main; spec set present
- [x] P amendment — probe tasks made agent-runnable (3f610df)
- [ ] R spec-set review of the amendment — review leg 1 and the strong review leg
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

## Tasks
| Task | Verdict | Review (legs, rounds, strong leg?) | Gaps / decision request |
|---|---|---|---|

## Known red
| Test (file:name) | Story | Written in | Cleared in |
|---|---|---|---|

## Test changes
| Test (file:name) | Written in | Changed in | Evidence it was wrong |
|---|---|---|---|

## Live runs
| Task | Run id | Reviewed SHA | Observation | Cost | Leftovers |
|---|---|---|---|---|---|

## Lessons
- See spec 006's loop.md *Lessons*; they apply here, especially the entry command shape, whole-checkout scratch copies for live-package mutants, and vacuity checks on harness runs.
