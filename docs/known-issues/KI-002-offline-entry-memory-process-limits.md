---
id: KI-002
title: The offline entry bounds disk and time but not memory or process count
kind: security
status: open
likelihood: low
impact: medium
severity: low
verified:
  at: 71c2de8
  on: 2026-10-16
  how: code read of the spec 006 T032 source (limits applied); the published r4-e2 entry has none
issue:
source: independent review of the T003 offline entry, 2026-10-04
decision:
references:
  - path: tools/cmd/lz-offline/main.go
    blob: 1561dacadcec35f3bee515d7cc26363f4509ef4c
  - path: tools/cmd/lz-offline/limits.go
    blob: 90ec35fdb1b1fd535e669e29280b92717d1debfb
last_verified: 71c2de81d7bfd86776df728dc5ae87976e26836b
---

## What is true

`lz-offline` runs candidate commands inside bwrap with private, size-bounded tmpfs
mounts for every writable area (`/tmp`, `/run`, `/home`, `/candidate`), a 240 s
deadline per target and an 8 MiB output cap (`tools/cmd/lz-offline/main.go`, constants
`tmpBytes`, `candidateBytes`, `runBytes`, `homeBytes`, `childDeadline`). The new PID
namespace and `--die-with-parent` end every candidate process when the deadline
kills bwrap.

The published entry applies no memory or process-count limit. tmpfs pages count toward
host memory up to the tmpfs sizes, and a candidate can allocate memory or fork freely
until the deadline. The source now runs each child in a transient user scope with
`MemoryMax`, `MemorySwapMax=0` and `TasksMax` and refuses a host that cannot apply them
(spec 006 FR-013, `tools/cmd/lz-offline/limits.go`); this issue stays open until an
entry built from it is published and pinned.

## Why it matters

A hostile candidate can exhaust workstation or runner memory, or slow the host with a
fork storm, for up to 240 s per target. The deadline was 120 s until the r4-e2 entry;
it was doubled because `test:live-lane` ran close to 120 s on slow runners, so this
exposure window doubled with it. Giving each target its own deadline would not narrow
it: a hostile candidate can redefine any target the entry does not run itself, so the
window is the largest deadline. It cannot reach the network, credentials or host files
through this path. Today the candidate is the maintainer's own repository, so the
likelihood is low; it rises when untrusted contributors or shared runners execute
candidate code.

## How to re-check

```sh
git grep -n -e '--size' -e 'childDeadline' -- tools/cmd/lz-offline/main.go
git grep -n -e 'MemoryMax' -e 'TasksMax' -- tools/cmd/lz-offline/limits.go
```

Expected today: all four match in the source. Whether the published entry carries them
is the pinned entry revision's question, not the source's.

## Fix sketch

Run bwrap inside a transient cgroup with `MemoryMax` and `TasksMax` (for example a
`systemd-run --user --scope` unit, or a delegated cgroup on CI runners) and refuse to
start when the limits cannot be applied. Given a candidate that allocates beyond the
limit or forks beyond the task budget, when the entry runs it, then the child is
killed and the entry reports a resource-limit failure, while the valid boundary suite
still passes.
