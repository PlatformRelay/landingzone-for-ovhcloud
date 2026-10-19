# Live-window plan — spec 007

- Written: 2026-10-19, before the first live run of this spec (T018). Requirements: FR-010 (FR-011 for
  the retained list). Status of every run below: `not-run`.
- Times are Europe/Berlin, CEST (+02:00) throughout: the switch to CET is on 2026-10-25, after the
  window.
- Every limit marked **coordinator procedure, no sensor** is checked by hand by the coordinating
  session before each run; nothing in code enforces it (spec.md Q "Are FR-010's guards enforced in
  code?"). Limits marked **lz-live host guard** are enforced by the live lane's existing guard.
- Each run's own evidence (`evidence/T019.md`, `T022.md`, `T023.md`) and the *Live runs* table of
  `loop.md` record its run id, SHA, outcome, retries seen, approximate cost and the credit left.

## Credit and bounds

| Item | Value | Source | Status |
|---|---|---|---|
| Trial voucher | 200 €; expires 2026-10-24 12:42 +02:00 | coordinator's Control Panel reading, 2026-10-19 | believed |
| Voucher used at plan time | 0.17 € | same reading | believed |
| Separate credit | 10 €, valid to 2027-10-01 | same reading | believed |
| Spec 007 spend ceiling | about 5 € of the voucher; 0 € of card | D119; FR-010 | decided |
| Credit floor | no new live start below 20 € of remaining credit | D118 (AGENTS.md *Bounded extension*); FR-010 | decided |
| D118 bound (this account) | live targets for the coordinating session until 2026-10-24 23:59, or below 20 € | AGENTS.md *Bounded extension* | decided |
| Goal stop | no new live start after 2026-10-23 23:00 | D117; spec.md Q on the cleanup day | decided |

The floor is read against the voucher's available credit alone, not voucher plus the 10 € credit:
the stricter reading, so the floor trips first if the two disagree (coordinator procedure, no
sensor). The 10 € credit is not planned spend.

## Limits of FR-010 and who checks them

| # | Limit | Who checks | How |
|---|---|---|---|
| L1 | Every live task (T019, T022, T023) is run by the coordinating session under D118, never by a subagent | coordinator | coordinator procedure, no sensor (subagent briefs forbid live targets) |
| L2 | Run from the owner's clean live clone at a reviewed SHA on `main`; credentials only in the child process; output redacted | lz-live host guard | `--reviewed-sha`, `LZ_OWNER_CHECKOUT` in `live.env`; enforced by `lz-live` |
| L3 | Code under `tests/live/` or `tools/internal/live/` runs live only after its independent review, never after an admin-merge | coordinator | coordinator procedure, no sensor: check the PR's review record before choosing the SHA |
| L4 | Spec 007's live spend stays within about 5 € of voucher; 0 € card | coordinator | coordinator procedure, no sensor: sum of the per-run cost deltas below |
| L5 | No run starts below 20 € of remaining credit | coordinator | coordinator procedure, no sensor: cost reading before the run |
| L6 | T019 (budget alert) is dropped if not started by 2026-10-21 23:00; it is then recorded as dropped and FR-005 as not qualified | coordinator | coordinator procedure, no sensor |
| L7 | T022 (final chain) finishes, with its recovery, on 2026-10-22 | coordinator | coordinator procedure, no sensor |
| L8 | On 2026-10-23 only verification and cleanup start, none whose recovery allowance (deadline plus destroy budget) would end after 23:00 | coordinator | coordinator procedure, no sensor; see *Latest start* |
| L9 | No run creates a project, a retained bucket or a credential handed to anyone | coordinator | coordinator procedure, no sensor: the planned roots and stacks below create none |
| L10 | Each run's evidence records run id, SHA, outcome, retries seen, approximate cost and credit left | coordinator | coordinator procedure, no sensor; the reviewer of the run's evidence checks it |
| L11 | Nothing a disposable run created goes on `evidence/retained.md`; while one exists, no further feature run starts and this spec cannot complete, until a clean leftover check shows it gone | coordinator | coordinator procedure, no sensor; the leftover check is the evidence (FR-011) |
| L12 | Every resource a live run creates carries the run id in its name or tag, or is found through the pre-apply baseline (FR-006) | coordinator; the lz-live baseline and leftover check | coordinator procedure, no sensor, for the naming of a new root; the baseline (`baseline.json`) and the leftover check find the rest |

**Latest start.** The default run deadline is 45 min (`tools/internal/live/deadline.go:11`) and the
destroy budget is `max(DefaultDeadline, run)` (`deadline.go:37`), so a run with the default
deadline has a 90-minute recovery allowance. Verified 2026-10-19 by reading the code.
- 2026-10-21: T019 starts by 23:00 at the latest (L6).
- 2026-10-22: T022 starts by 21:30 with the default deadline, so its recovery ends on the 22nd (L7).
- 2026-10-23: nothing starts after 21:30 with the default deadline; a longer `--deadline` D
  (over 45 min) moves the latest start to 23:00 minus 2 × D (L8).
- The chain applies its stacks under one deadline and destroys on exit with at least the default
  (`tools/internal/live/chain.go:66`, `:342`); that the same 90 minutes cover a whole chain's
  recovery is believed, not measured, so T022 starts with margin rather than at 21:30.

## Planned runs, in order

| # | Run | Who | Earliest | Cut-off | Depends on | Creates | Status |
|---|---|---|---|---|---|---|---|
| 0 | Owner steps: bootstrap re-apply (Object Lock read for the bootstrap state user, spec 006 T048) and clearing the 9 pre-baseline probe runs with `--cleanup <run-id> --accept-lost-baseline`, through `~/.local/share/ovh-lz/review/owner-monday.sh` in the owner's own terminal | owner (`bootstrap:*` and the terminal-gated flag stay the owner's) | now | before T019's cut-off | US3 on `main` (#136) | nothing new (re-apply of the existing bootstrap) | `not-run` |
| 1 | T019: the budget-alert probe, `task live:probe -- --reviewed-sha <sha> tests/live/probes/budget-alert` | coordinator | after row 0 | started by 2026-10-21 23:00 (L6) | row 0; T006, T007, T009, T014–T016 reviewed on `main`; this file | one disposable budget alert, deleted by destroy and sweep | `not-run` |
| 2 | The Object Lock chain: spec 006 FR-010's `state-bucket-object-lock` assertion on the state buckets (`lz-bkt-state` among them), observed live after the bootstrap re-apply, with `task live:chain -- --reviewed-sha <sha> all`: the L7 assertions run under `-- all` only (`tools/internal/live/chain.go:333`). Spec 006's observation, not a spec 007 task; its spend counts against the same ceiling. If T022 runs after the re-apply, its observations carry the same assertion, and the coordinator may fold this row into row 4 | coordinator | after row 0, and after row 1 has ended | 2026-10-22, with its recovery, before row 4 | row 0 | the whole chain: ephemeral instance destroyed on exit, retained stacks kept as they are | `not-run` |
| 3 | T021: `evidence/retained.md` from the latest run record, no API call | coordinator | after row 1 (or after this file if T019 is dropped) | before row 4 | row 1 if it ran | nothing | `not-run` |
| 4 | T022: the final chain, `task live:chain -- --reviewed-sha <sha> all` | coordinator | after row 3 with no open blocker | finished with its recovery on 2026-10-22 (L7) | row 3 | ephemeral instance, destroyed on exit; retained stacks as listed in `retained.md` | `not-run` |
| 5 | T023: verification and cleanup — `--cleanup <run-id>` or `task live:destroy` until clean; `retained.md` re-checked; total spend recorded | coordinator | 2026-10-23 | no start whose recovery ends after 23:00 (L8) | row 4 | nothing | `not-run` |

No two runs overlap: one workstation, one credit (tasks.md *Parallel opportunities*). Checked by
the coordinator: coordinator procedure, no sensor.

## Abort conditions

Checked by the coordinator before and after each run: coordinator procedure, no sensor. Stop the
window — start no further run, record why in the run's evidence and in this file — on any of:
- a resource that cannot be cleaned up (a delete the API refuses or that keeps failing);
- a leftover that neither the root's sweep nor `--cleanup <run-id>` removes;
- a credit reading below 20 € (L5), or the spec's spend reaching about 5 € (L4);
- any card charge (L4);
- a red check on the reviewed SHA (spec.md *Dependencies and stop conditions*).

After an abort, only cleanup of the stuck resource runs; anything left is reported to the owner, and
it never goes on `retained.md` (L11).

## Cost recording

Before and after each run, the coordinator runs `sh ~/.local/share/ovh-lz/review/ovh-cost.sh`, a
read-only check (`GET /me/credit/balance` and per-project credit and current usage; credentials
stay in its child process). Each run's evidence records the two readings, their difference as the
approximate cost, and the credit left. Whether the usage reading lags the run is UNVERIFIED; the
after-reading is repeated at the next run's before-reading, and T023 records the spec's total.

## Second account (D121)

D121 extends the same bounded exception to the fresh free-trial account (D120), with its own bound:
until that account's voucher expires or its credit falls below 20 €. That window is separate: it is
not part of this plan, its spend does not count against this spec's ceiling, and no spec 007 run
targets it. Its plan, if any, is written in its own evidence before its first run.
