# Implementation Plan: Cleanup-first live slice
Branch: worktree of `main` per task · Date: 2026-10-16 · Spec: [spec.md](spec.md) · Status: draft

## Summary
Make the live lane's cleanup survive transient OVHcloud listing errors and keep what failed;
refuse an account file that maps two tenant refs onto one project (A17); qualify the landing zone's
budget-alert path with one disposable probe run; narrow the chain's run locks and prove a second
tenant offline (A24, B5); then close the live window with a retained-resource list, one final chain
and a cleanup day. Live work is ordered by D119 and bounded by D117/D118. Decisions and evidence:
[research.md](research.md).

## Technical context
- **Language/version**: Go 1.27.1 (tools module, vendored), OpenTofu 1.13.0, Task 3.53.1
  (`mise.toml`, `mise.live.toml`); provider `ovh/ovh` 2.21.0 from the entry's mirror.
- **Runtime**: offline entry r4-e2 for every offline check (`task <target> [-- <path>]`). Live
  runs: `lz-live` on the maintainer's workstation, from the clean live clone at a reviewed SHA on
  `main`, run by the coordinating session under D118 (expires 2026-10-24 23:59 or below 20 € of
  credit).
- **Testing**: Go tests with spec 005's fakes (fake API with scripted answers per path and attempt,
  fake `tofu`, fake S3, `DirLocks` in a temporary directory, fresh-process lock holders); `tofu
  test` with a mocked provider for the probe root (`lz-check unit`).
- **Cost**: one probe run and one chain, cleanup runs as needed; ceiling about 5 € of voucher, 0 €
  card (R6). The AGENTS.md sandbox budget applies: offline layers on every change, live only where
  this spec names a live task.
- **Constraints**: no credential in output, argv, files or the repository; agents never read
  `~/.config/ovh-lz/`; subagents never run live targets.

## Constitution check
Before research and after design (unchanged by design).

| Principle | Status | Note |
| --- | --- | --- |
| I Evidence, UNVERIFIED | pass | P1–P5 named with their probes; P1 (believed) becomes an observation through FR-002's attempt records; OVHcloud claims cite `kb/` or are UNVERIFIED (alert cost, retained prices) |
| II Traceability | pass | `007/` registry keys; spec 007 in the `check:specs` default and in `ci-source` (this spec-set commit; T002 verifies) |
| III Tests first | pass | every behaviour story lists its test tasks first, with given/when/then cases and the defect each exposes, against the signatures in data-model.md |
| III Mutation proof | pass (scoped) | guards G24–G28 (`contracts/checks.md`) |
| IV One owner, explicit authority | pass | live runs by the coordinating session only (D118); `bootstrap:*` and `~/.config/ovh-lz/` edits stay the owner's (R7) |
| V Fail closed; cleanup | pass | permanent errors never retried; a failed baseline stops before any create; aliasing refused before any child; shared locks still exclude writers; retained resources listed before the final run |
| V Cost | pass | ceiling and stop rules in FR-010; cost recorded per run |
| VI Smallest increment; AgentEx | pass; trigger fired | this is the second vertical slice (D119 Q3); AgentEx scheduled as its own offline spec from 2026-10-24 |
| VII Planned vs implemented | pass | no new Task target; the probe root is planned until T012 closes |
| VIII Decisions | pass | D119 scope; four open owner points carry defaults (spec Clarifications); counterpoints in research R5 and D119 |

## Alternatives and decisions
- **Retry placement** (R1): per request in `lister` (chosen) vs whole check vs `apiLister.request`.
  The chosen one sees the attempt, the cursor and the shared budget.
- **Alias check placement** (R2): stacks adapter called where the account binds (chosen) vs the
  decoder, which never sees ids.
- **Nameless kinds** (R3): pre-apply baseline (chosen) vs a name or tag the API does not offer.
- **Budget alert** (R4): probe root through the module (chosen) vs enabling it in the retained
  `project` stack (orphan risk) vs the raw resource again (already observed).
- **Locks** (R5): reader/writer on the existing lock names (chosen) vs per-instance names vs no
  locks for reads (reopens T062).
- Cheapest discriminating experiment for A17's consequence: reading the decoder and the binding
  (T001), no run.

## Project structure
```
specs/007-cleanup-first-live-slice/
  spec.md  plan.md  research.md  data-model.md  quickstart.md  tasks.md
  contracts/checks.md  checklists/requirements.md  evidence/ (created by tasks)
tools/internal/live/   leftovers.go sweep.go runner.go apply.go bootstrap_state.go chain.go (+ tests)
tools/internal/stacks/ adapter.go lock.go (+ tests)
tests/live/probes/budget-alert/        main.tf probe.tf probe.tftest.hcl
tests/fixtures/manifests/sandbox-two-tenants/deployments.yaml
```

## Delivery phases
1. Setup: T001 (A17 consequence), T002 (trace check).
2. US1 retry and record: tests T003–T004, then T005–T006.
3. US2 alias refusal: test T007, then T008.
4. US3 budget alert offline: tests T009–T010, then T011–T012.
5. US4 locks and two tenants offline: tests T013–T015, then T016–T017.
6. Live window: T018 (budget alert), T019 (docs of what T018 showed), gated T020–T021, T022
   (retained list), T023 (final chain), T024 (verification and cleanup, 2026-10-23).
7. Polish: T025 (state of the spec), T026 (exit checks).

## Verification strategy
V001–V006 in spec.md and `contracts/checks.md`. Offline: `task test:live-lane`, `task test:stacks`,
`task check:specs`, `task test:traceability`, `task test:foundation-ci`, `task docs:check`, each
through the entry. Test tasks close on a verified red against a stub; implementation tasks close
when their test task's rows are green and, for G24–G28, a mutant is killed. Live: the commands of
`contracts/checks.md` *Live run contracts*, with the run id, SHA, outcome, retries and cost in the
task's evidence.

## Dependencies and live change ordering
- Spec 006's live-lane tasks T022–T026 land on `main` before T003 (shared `retryBudget`, bounded
  verbs, destroy budget): every live-lane file of US1–US4 changes there too.
- US1 → US2 → US3 → US4 on `test:live-lane` (red windows as spec 006 defines them); T001 before
  any US4 task.
- Live: T018 needs T005, T006, T008, T011 and T012 on `main` at a reviewed SHA, each reviewed by an
  independent leg (D118: code under `tests/live/` or `tools/internal/live/` runs live only after
  that). T022 before T023; T023 finishes with its recovery on 2026-10-22; T024 on 2026-10-23.
- Per-run timebox and cost: budget alert, one probe deadline (default), under 0.1 € expected
  (R4, UNVERIFIED); final chain, 45 min default deadline, under 1 € (R6). Cleanup authority: the
  probe core's destroy in `finally`, then `--cleanup <run-id>`; for the chain, destroy-on-exit,
  then `live:destroy` per ephemeral instance.
- Claim withdrawn on refutation: P2 refuted → FR-005's "through the module" claim is withdrawn and
  the run qualifies the inlined resource only; P3 refuted (baseline misses an alert) → FR-006 is
  `fail` and nameless kinds are removed by hand with the evidence naming them.

## Complexity tracking
No new tool and no new Task target. One new lock mode on the existing lock files; one new probe
root; one new manifest fixture. Deferred: per-instance locks, live concurrency, a second project,
KD-3 (spec Out of scope).
