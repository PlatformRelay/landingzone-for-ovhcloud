# Quickstart: Cleanup-first live slice

## Offline (any contributor, no credentials, no network)
Through the offline entry, from `$HOME`:

```sh
lz-offline --candidate <checkout> -- task test:live-lane     # retry, record, alias lane, baseline, budget-alert root, chain locks
lz-offline --candidate <checkout> -- task test:stacks        # alias check, shared locks, two-tenant fixture
lz-offline --candidate <checkout> -- task check:specs -- specs/007-cleanup-first-live-slice
```

## Live (coordinating session only, under D118, until 2026-10-23 23:00)
From the clean live clone at a reviewed SHA on `main` (never a worktree, never a subagent):

```sh
S=<reviewed sha>
mise exec -- task live:probe -- --reviewed-sha $S tests/live/probes/budget-alert   # T018
mise exec -- task live:chain -- --reviewed-sha $S all                              # T023, after evidence/retained.md
mise exec -- task live:probe -- --reviewed-sha $S --cleanup <run-id>               # a probe run that is not clean
```

A retry prints `LZ-LIVE retry listing <path> in <wait>: <status> (attempt <n> of 3)`. A refusal
`project-alias` names two refs that `account.env` maps to one project: give each its own project or
drop one row. Record each run's id, SHA, outcome, retries and approximate cost in the task's
evidence; check the remaining credit before each run (Control Panel or `ovhcloud`) and stop below
20 €.

## What changes for the owner
Nothing to run. Four defaults stand until answered (spec *Clarifications*): the date governs the
cleanup day, the sandbox flag does not admit two tenants on one project, the alert goes to the
account's own address, and listings retry three times. A second tenant live would need a new
`LZ_PROJECT_ID_LAB_DEV` in the owner's account file (research R7), which is why it is off by
default.
