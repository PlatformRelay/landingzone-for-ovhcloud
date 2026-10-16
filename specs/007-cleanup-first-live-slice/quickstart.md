# Quickstart: Cleanup-first live slice

## Offline (any contributor, no credentials, no network)
Through the offline entry, from `$HOME`:

```sh
lz-offline --candidate <checkout> -- task test:live-lane     # retry, record, alias lane, baseline, sweep, read-back, budget-alert root
lz-offline --candidate <checkout> -- task test:stacks        # alias check, two-tenant fixture
lz-offline --candidate <checkout> -- task test:bootstrap     # alias check in the bootstrap's setup
lz-offline --candidate <checkout> -- task check:specs -- specs/007-cleanup-first-live-slice
```

## Live (coordinating session only, under D118)
From the clean live clone at a reviewed SHA on `main` (never a worktree, never a subagent). Before
each run, check the remaining credit by hand (Control Panel or `ovhcloud`) and stop below 20 €; no
code does this for you.

```sh
S=<reviewed sha>
mise exec -- task live:probe -- --reviewed-sha $S tests/live/probes/budget-alert   # T019, not after 2026-10-21 23:00
mise exec -- task live:chain -- --reviewed-sha $S all                              # T022, after evidence/retained.md; done by 2026-10-22
mise exec -- task live:probe -- --reviewed-sha $S --cleanup <run-id>               # a probe run that is not clean
```

A retry prints `LZ-LIVE retry listing <path> in <wait>: <status> (attempt <n> of 3)`. A refusal
`project-alias` names two refs that `account.env` maps to one project: give each its own project or
drop one row. A refusal `unreconciled-run` names an earlier probe run whose files are still under
`state/probes/`: finish it with `--cleanup <run-id>` first. Record each run's id, SHA, outcome,
retries, approximate cost and the credit left in the task's evidence. Anything a probe created that
is still there blocks the next feature run; it never goes on `evidence/retained.md`.

## What changes for the owner
Nothing to run. Four defaults stand (spec *Clarifications*, confirmed by the coordinator): the date
governs the cleanup day, one ref per project (no second tenant in the one project), the alert goes
to the account's own address, and listings retry three times at 5 s and 15 s inside the shared
retry budget.
