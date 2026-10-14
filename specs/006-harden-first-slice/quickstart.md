# Quickstart: Harden and tidy the first slice
Commands marked *planned* do not exist until their creating task lands (`tasks.md`).

## Offline (any contributor, no credentials, no network)
```sh
E=<approved-absolute-path>/lz-offline; C=$(pwd)
$E --candidate "$C" -- task test:live-lane       # V001, V002: secrets, bounded verbs, budgets
$E --candidate "$C" -- task test:bootstrap       # V001: scratch home on the bootstrap path
$E --candidate "$C" -- task test:stacks          # V003: unreconciled rows, stage locks
$E --candidate "$C" -- task test:traceability    # V004: registry reads cover go list -deps
$E --candidate "$C" -- task test:runtime-image   # V005, V006: limits (fakes), preparation, provenance
$E --candidate "$C" -- task test:update-lane     # V007 (planned, T035)
$E --candidate "$C" -- task test:release         # V008 (planned, T036)
$E --candidate "$C" -- task docs:check           # V009
$E --candidate "$C" -- task check:specs -- specs/006-harden-first-slice   # V010
```

## Host, no credentials (agent or maintainer)
The entry limits need the host's user manager, so their real-enforcement test runs outside the
entry (planned, T029):
```sh
go -C tools test -tags hostlimits -run TestHostLimits ./cmd/lz-offline
```
If it reports `LIMITS_UNAVAILABLE no-user-manager`, the host has no systemd user session; run it from
a login session, or enable lingering for the user.

## Rendering a release-train manifest (planned, T038; host, read-only)
```sh
go -C tools run ./cmd/lz-release manifest --commit "$(git rev-parse HEAD)" --version 2026.10.0
```
A read-only host step with no credentials: it reads git objects, which the entry's snapshot does
not carry (`.git` is not admitted), so it cannot run inside the entry; the checks of FR-016 run in
the entry on a fixture repository instead. Prints to standard output; creates no tag and no file.
Publishing a train is an owner decision.

## What changes for the owner
Nothing to run. `live:plan`, `live:apply` and `live:destroy` now print a run summary like the chain;
keep the run id with the approximate cost in the PR as before.
