# Implementation Plan: Cleanup-first live slice
Branch: worktree of `main` per task · Date: 2026-10-16 · Revised: 2026-10-16 (spec-set review round 1) · Spec: [spec.md](spec.md) · Status: draft

## Summary
Make the live lane's cleanup survive transient OVHcloud listing errors inside one shared retry
allowance and keep every failed attempt; refuse an account file that maps two refs onto one project
(A17) before any child process; qualify the landing zone's budget-alert path with one disposable
probe run whose sweep can remove an alert even when its creation was never recorded, and whose
read-back checks what the API holds; prove a second tenant offline (B5); then close the live window
with a list of approved retained infrastructure, one final chain and a cleanup day. Narrowed locks
(A24) are deferred with their split design. Live work is ordered by D119 and bounded by D117/D118.
Decisions and evidence: [research.md](research.md).

## Technical context
- **Language/version**: Go 1.27.1 (tools module, vendored), OpenTofu 1.13.0, Task 3.53.1
  (`mise.toml`, `mise.live.toml`); provider `ovh/ovh` 2.21.0 from the entry's mirror.
- **Runtime**: offline entry r4-e2 for every offline check (`task <target> [-- <path>]`). Live
  runs: `lz-live` on the maintainer's workstation, from the clean live clone at a reviewed SHA on
  `main`, run by the coordinating session under D118 (expires 2026-10-24 23:59 or below 20 € of
  credit).
- **Testing**: Go tests with spec 005's fakes (fake API scripted per path and attempt, fake HTTP
  server for body resets, fake `tofu`, fake children counting processes, fresh-process `lz-live`
  runs as spec 003 T037); `tofu test` with a mocked provider for the probe root (`lz-check unit`,
  with the root's pinned lock file).
- **Cost**: one probe run and one chain, cleanup runs as needed; ceiling about 5 € of voucher, 0 €
  card (R6), held by coordinator procedure, not by code.
- **Constraints**: no credential in output, argv, files or the repository; agents never read
  `~/.config/ovh-lz/`; subagents never run live targets.

## Constitution check
Before research and after design (re-checked after review round 1).

| Principle | Status | Note |
| --- | --- | --- |
| I Evidence, UNVERIFIED | pass | P1–P4 named with their probes; P1 (believed) becomes an observation through FR-002's attempt records; OVHcloud claims cite `kb/` or are UNVERIFIED (alert cost, retained prices) |
| II Traceability | pass | `007/` registry keys; spec 007 in the `check:specs` default and in `ci-source` (spec-set commit; T002 verifies) |
| III Tests first | pass | every behaviour story lists its test tasks first, with given/when/then cases and the defect each exposes, against the signatures in data-model.md; assertions that already hold are marked *pin* |
| III Mutation proof | pass (scoped) | guards G24–G28 (`contracts/checks.md`), including a mutant that deletes a baseline id |
| IV One owner, explicit authority | pass | live runs by the coordinating session only (D118); `bootstrap:*` and `~/.config/ovh-lz/` stay the owner's |
| V Fail closed; cleanup | pass | permanent errors never retried; nil budget admits no wait; a failed baseline stops before any create; a probe never starts beside an unreconciled run; recovery files stay until reconciliation; aliasing refused with zero child processes; a disposable resource never becomes "retained" |
| V Cost | pass (procedure) | ceiling, credit floor and dates are coordinator procedure (FR-010); no sensor claims to enforce them |
| VI Smallest increment; AgentEx | pass; trigger fired | second vertical slice (D119 Q3); AgentEx its own offline spec from 2026-10-24; A24 deferred rather than half-built |
| VII Planned vs implemented | pass | no new Task target; the probe root and the probe-check package are planned until T016 and T015 close |
| VIII Decisions | pass | D119 scope; the coordinator's round-1 decisions (A24 deferred, B5 offline, defaults confirmed); counterpoints in research R5 and D119 |

## Alternatives and decisions
- **Retry placement** (R1): per request in `lister` (chosen) vs whole check vs `apiLister.request`.
  The chosen one sees the attempt, the cursor and the shared budget.
- **Alias check placement** (R2): the lane's setup and the bootstrap's `setup` (chosen) vs the
  decoder (never sees ids) vs the bootstrap's publish and verify bindings (after `tofu` ran).
- **Earlier orphans** (R3): refuse to start beside an unreconciled probe run (chosen) vs matching
  baseline ids against earlier runs' records (depends on records that may be overwritten or in
  another checkout).
- **Budget alert** (R4): probe root through the module with its lock file (chosen) vs enabling it
  in the retained `project` stack (orphan risk) vs an inlined resource (does not qualify the module).
- **Locks** (R5): deferred; the split `tenant-<t>`/`tenant-<t>-eph` design is recorded for the spec
  that needs concurrency.
- Cheapest discriminating experiment for A17's consequence: reading the decoder and the binding
  (T001), no run.

## Project structure
```
specs/007-cleanup-first-live-slice/
  spec.md  plan.md  research.md  data-model.md  quickstart.md  tasks.md
  contracts/checks.md  checklists/requirements.md  evidence/ (created by tasks)
tools/internal/live/   leftovers.go sweep.go runner.go apicheck.go apply.go bootstrap_state.go chain.go (+ tests)
tools/internal/probes/live/alerting/   check.go check_test.go (new)
tools/cmd/lz-live/     probe.go probe_recovery_test.go
tools/internal/stacks/ adapter.go (+ tests)
tests/live/probes/budget-alert/        .terraform.lock.hcl main.tf probe.tf probe.tftest.hcl
tests/fixtures/manifests/sandbox-two-tenants/deployments.yaml
```

## Delivery phases
1. Setup: T001 (A17 consequence), T002 (trace check).
2. US1 retry and record: tests T003–T005, then T006–T007.
3. US2 alias refusal: test T008, then T009.
4. US3 budget alert offline: tests T010–T013, then T014–T016.
5. US4 two tenants offline: T017.
6. Live window: T018 (window plan), T019 (budget alert, cut-off 2026-10-21 23:00), T020 (docs),
   T021 (retained list), T022 (final chain, by 2026-10-22), T023 (verification and cleanup,
   2026-10-23).
7. Polish: T024 (state of the spec), T025 (exit checks).

## Verification strategy
V001–V006 in spec.md and `contracts/checks.md`. Offline: `task test:live-lane`, `task test:stacks`,
`task test:bootstrap`, `task check:specs`, `task test:traceability`, `task test:foundation-ci`,
`task docs:check`, each through the entry. Test tasks close on a verified behavioural red against a
stub (an initialisation failure is not red: the root carries its lock file from T010);
implementation tasks close when their test task's rows are green and, for G24–G28, a mutant is
killed. Live: the commands of `contracts/checks.md` *Live run contracts*, with the run id, SHA,
outcome, retries, cost and credit left in the task's evidence.

## Dependencies and live change ordering
- Spec 006's live-lane tasks T022–T026 (PR #123) land on `main` before T003 and T004: the shared
  `retryBudget` and every live-lane file of US1–US3 change there. Its symbols are re-read on `main`
  first; a renamed one is followed and recorded.
- US1 → US2 → US3 on `test:live-lane`; T001 before T017 and T018.
- Live: T019 needs T006, T007, T009, T014, T015 and T016 on `main` at a reviewed SHA, each reviewed
  by an independent leg (D118), and is dropped if not started by 2026-10-21 23:00. T021 needs T019
  only if it ran; T022 needs T021 with no open blocker and finishes with its recovery on
  2026-10-22; T023 on 2026-10-23.
- Per-run timebox and cost: budget alert, one probe deadline (default), well under 1 € expected
  (R4, UNVERIFIED); final chain, 45 min default deadline, under 1 € (R6). Cleanup authority: the
  probe core's destroy and sweep in `finally`, then `--cleanup <run-id>`; for the chain,
  destroy-on-exit, then the lane's destroy verb per ephemeral instance.
- Claim withdrawn on refutation: P2 refuted → FR-005 not qualified (no fallback); P3 refuted (the
  baseline or the sweep misses an alert) → FR-006/FR-007 `fail`, and every later feature run waits
  until the alert is removed by hand and the leftover check is clean.

## Complexity tracking
No new tool and no new Task target. One new probe-check package (the pattern of
`tools/internal/probes/live/{state,storageiam,identity}`), three API operations, one probe root, one
manifest fixture, one start refusal. Deferred: A24's split locks, live concurrency, a second
tenant live, a second project, KD-3 (spec Out of scope).
