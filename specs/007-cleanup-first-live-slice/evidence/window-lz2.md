# Live-window plan — second window, account `lz2`

- Written: 2026-11-16, after the `lz2` bootstrap and before the window's first live run (T030).
  Requirements: FR-010 (FR-011 for the retained list). Status of every run below: `not-run`.
- Same structure and limits as the first window's [window.md](window.md); only what differs is
  stated in full here. Times are Europe/Berlin, CET (+01:00).
- Every limit marked **coordinator procedure, no sensor** is checked by hand by the coordinating
  session before each run. Limits marked **lz-live host guard** are enforced by the live lane.
- Each run's evidence (`evidence/T031.md`, `T032.md`, spec 003 `evidence/T044.md`) records its run
  id, SHA, outcome, retries seen, approximate cost and the credit left.

## Credit and bounds

| Item | Value | Source | Status |
|---|---|---|---|
| Trial voucher | 200 €; expires about 2026-12-16 | owner's Control Panel reading, 2026-11-16 ("200 €, about 30 days"; exact time not read) | believed |
| Window spend ceiling | about 5 € of the voucher; 0 € of card | D135, carrying D119; FR-010 | decided |
| Credit floor | no new live start below 20 € of remaining credit | D135; AGENTS.md *Second account* | decided |
| Grant | live targets for the coordinating session on this account until its voucher expires or credit < 20 € | AGENTS.md *Second account*; D121, D134 | decided |
| Goal stop | no new live start after 2026-12-09 23:00 (one week before the believed expiry, because the exact time is unread) | this plan | decided |

## Limits of FR-010 and who checks them

L1–L3 and L9–L12 are as in [window.md](window.md) (coordinator only, never a subagent; clean
live clone at a reviewed SHA on `main`, credentials only in the child; live code only after an
independent review; no project, retained bucket or handed-out credential created; evidence per run;
nothing disposable on the retained list; every created resource carries the run id or is found
through the baseline). The others for this window:

| # | Limit | Who checks | How |
|---|---|---|---|
| L4 | The window's live spend stays within about 5 € of voucher; 0 € card | coordinator | coordinator procedure, no sensor: sum of the per-run cost readings |
| L5 | No run starts below 20 € of remaining credit | coordinator | coordinator procedure, no sensor: credit reading before the run (the CLI has no billing read, so it is the owner's Control Panel reading or the last recorded one plus the sum of later costs) |
| L6 | No new live start after 2026-12-09 23:00 | coordinator | coordinator procedure, no sensor |
| L7 | Any `fail` or `refused` from a live run stops the window until it is understood | coordinator | coordinator procedure, no sensor; lz-live host guard prints it |
| L8 | Anything in the cloud that a run did not create stops the window | coordinator | the leftover check (lz-live host guard) for what it covers; coordinator procedure for the rest |

**Latest start.** Default run deadline 45 min, destroy budget `max(DefaultDeadline, run)`, so a
default run has a 90-minute recovery allowance (window.md *Latest start*, verified 2026-10-19).

## Planned runs, in order

| # | Run | Who | Depends on | Creates | Status |
|---|---|---|---|---|---|
| 0 | Owner: `lz2` bootstrap (`bootstrap:account --fresh-account`) | owner | — | admin, state bucket (retained) | done 2026-11-16 at `873018d`, every phase `ran` |
| 1 | T031: `task live:probe -- --reviewed-sha <sha> tests/live/probes/budget-alert` | coordinator | row 0; this file on `main` | one disposable budget alert | pass 2026-11-16, run `20261116T105420Z-627f` (one refused start before it; `T031.md`) |
| 2 | spec 003 T044: `task live:probe -- --reviewed-sha <sha> tests/live/probes/swift-reach` | coordinator | row 1's leftover check clean | disposable bucket, S3 user, Swift container | pass 2026-11-16, run `20261116T115812Z-a689` (a first run failed and was cleaned up; spec 003 `T044.md`) |
| 3 | `retained.md` for this account from the latest run records, no API call | coordinator | rows 1–2 | nothing | done 2026-11-16: `retained-lz2.md`, no open blocker |
| 4 | T032: `task live:chain -- --reviewed-sha <sha> all` | coordinator | row 3 with no open blocker; `state/probes/` empty | ephemeral instance, destroyed on exit | pass 2026-11-16, run `20261116T121429Z-ecca` (a first run failed on an API 500 and was destroyed; `T032.md`) |
| 5 | Verification and cleanup until the leftover checks are clean; spend recorded | coordinator | row 4 | nothing | `not-run` |

No two runs overlap (one workstation, one credit): coordinator procedure, no sensor.
