# Loop — 003-platform-feasibility (probe lane)

Repo: `PlatformRelay/landingzone-for-ovhcloud` · Branch: `lane-003-probes` (worktree `003-probes`) · Base: `origin/main` · Started: 2026-10-16 · Reviewers: two-model (review leg 1, plus the strong review leg on triggers); duo rules under D117
Scope: the four live probes only — test task, implementation task, then the live run: T029→T030→T025, T031→T032→T026, T033→T034→T027, T035→T036→T028. Tasks T001–T024 are out of this lane.
Next: L — T029 (probe-package test tasks first: T029, T031, T033, T035; T037/T038 touch tools/internal/live/runner.go and wait until spec 006 US1 is on main)
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
| R2 | The lost-create-response gap also exists in spec 005's probes and chains | — | coordinator | defer | hand-off: what no sensor covers; the leftover check and the stop rule are the guard | no |

## Tasks
| Task | Verdict | Review (legs, rounds, strong leg?) | Gaps / decision request |
|---|---|---|---|
| T029 | CLOSED-WITH-GAPS | leg 1 + strong leg, 2 rounds (r1 REQUEST_CHANGES/CONCERNS, r2 REQUEST_CHANGES/CONCERNS; verified findings fixed) | round-2 fixes unreviewed (last round); Sweep on a non-empty bucket UNVERIFIED (T030/T025) |

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
