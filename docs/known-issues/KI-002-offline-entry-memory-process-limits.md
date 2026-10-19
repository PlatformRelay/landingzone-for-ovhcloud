---
id: KI-002
title: The offline entry's swap-off is unobserved on a host with free swap
kind: security
status: open
likelihood: low
impact: low
severity: note
verified:
  at: 9f318ea
  on: 2026-10-19
  how: code read of limits.go at 9f318ea; executed in CI runs 39249394208 (runner probe) and 39252705025 (r5 pin, PR 135), logs read 2026-10-19
issue:
source: independent review of the T003 offline entry, 2026-10-04; narrowed by spec 006 T043
decision:
references:
  - path: tools/cmd/lz-offline/main.go
    blob: 6b0916bca504d2a4c8c0b6a31577bb8ac3a03980
  - path: tools/cmd/lz-offline/limits.go
    blob: 90ec35fdb1b1fd535e669e29280b92717d1debfb
  - path: .github/workflows/foundation.yml
    blob: 445bac74c5390e57a0f844222ada15f8e64e69a6
last_verified: 2131f92c4b0b7a72413dd4300085052772608120
---

## What is true

`lz-offline` runs candidate commands inside bwrap with private, size-bounded tmpfs
mounts for every writable area (`/tmp`, `/run`, `/home`, `/candidate`), a 240 s
deadline per target and an 8 MiB output cap (`tools/cmd/lz-offline/main.go`, constants
`tmpBytes`, `candidateBytes`, `runBytes`, `homeBytes`, `childDeadline`). The new PID
namespace and `--die-with-parent` end every candidate process when the deadline
kills bwrap.

Since spec 006 (FR-013) the entry also runs each child in a transient
`systemd-run --user --scope` unit with `MemoryMax` 4 GiB, `TasksMax` 1024 and
`MemorySwapMax=0` (`tools/cmd/lz-offline/limits.go`, `entryMemoryMax`, `entryTasksMax`:
twice the largest measured peak, rounded up to a power of two). Before starting the child it checks that the scope
carries `memory.max`, `pids.max` and `memory.swap.max` = `0` and that its events files
read; otherwise it refuses with `LIMITS_UNAVAILABLE`. A child that trips either limit is
stopped (the whole scope through `cgroup.kill`, so a retrying fork storm ends too) and
the entry reports `RESOURCE_LIMIT memory|tasks`.

The published entry carries this: runtime r5, pinned in
`.github/workflows/foundation.yml` (PR 135). Observed, 2026-10-19:

- CI run 39249394208, a probe on the `ubuntu-24.04` runner: the user manager delegates
  `cpu memory pids` once linger is on, a scope carries `memory.swap.max=0`, and the
  host-tagged `TestHostLimits` passes there (a valid child passes, an allocation past
  the memory limit and a fork storm past the task limit are stopped, `--die-with-parent`
  ends the tree).
- CI run 39252705025 (PR 135's head): the first foundation run on the r5 entry, green
  on all 18 targets. Since that entry refuses a host where the limits do not apply,
  every target there ran limited.

What remains, and why this issue stays open but narrowed: no host with **free** swap
has run the limit test. The workstation that measured the peaks had its swap full
(spec 006 T032 evidence, matrix row 14), and the probe did not record the runner's
swap state. That a child past `MemoryMax` is killed, not swapped out, on such a host is
believed from cgroup v2 semantics with `memory.swap.max` = `0` (which the entry checks),
not observed.

## Why it matters

Before spec 006 a hostile candidate could take workstation or runner memory and fork
freely for up to 240 s per target. That exposure is now bounded to 4 GiB and 1024 tasks
per child for at most the same 240 s. If swap-off did not hold on a host with free swap,
a child past the memory limit would swap instead of stopping, slowing that host until
the deadline. Impact is low (bounded in time, no network, credentials or host files
through this path); likelihood is low (the entry checks the swap setting before it
starts the child, and the candidate is the maintainer's own repository).

A host without a delegating systemd user manager cannot run the entry at all; that is
the intended fail-closed refusal, not part of this issue.

## How to re-check

```sh
git grep -n -e '--size' -e 'childDeadline' -- tools/cmd/lz-offline/main.go
git grep -n -e 'entryMemoryMax' -e 'entryTasksMax' -e 'MemorySwapMax=0' -e 'memory.swap.max' -- tools/cmd/lz-offline/limits.go
git grep -n -e 'LZ_MANIFEST' -e 'LZ_LAYER' -- .github/workflows/foundation.yml
```

Expected: every pattern matches. To close the issue, run the host-tagged limit test
(`go -C tools test -count=1 -tags hostlimits -run TestHostLimits ./cmd/lz-offline`) on a
host with a delegating user manager and free swap, record `swapon --show` beside the
result, and remove this entry and its index row if the memory row passes.

## Fix sketch

None needed in the code unless the free-swap run fails. If it does, the entry should
refuse a host where `memory.swap.max` = `0` is not honoured, as it already refuses one
where the scope lacks it.
