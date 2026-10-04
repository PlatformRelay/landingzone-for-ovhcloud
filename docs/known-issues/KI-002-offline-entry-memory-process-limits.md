---
id: KI-002
title: The offline entry bounds disk and time but not memory or process count
kind: security
status: open
likelihood: low
impact: medium
severity: low
verified:
  at: 883b296
  on: 2026-10-04
  how: code read
issue:
source: independent review of the T003 offline entry, 2026-10-04
decision:
---

## What is true

`lz-offline` runs candidate commands inside bwrap with private, size-bounded tmpfs
mounts for every writable area (`/tmp`, `/run`, `/home`, `/candidate`), a 120 s
deadline and an 8 MiB output cap (`tools/cmd/lz-offline/main.go`, constants
`tmpBytes`, `candidateBytes`, `runBytes`, `homeBytes`, `childDeadline`). The new PID
namespace and `--die-with-parent` end every candidate process when the deadline
kills bwrap.

No memory or process-count limit is applied. tmpfs pages count toward host memory up
to the tmpfs sizes, and a candidate can allocate memory or fork freely until the
deadline.

## Why it matters

A hostile candidate can exhaust workstation or runner memory, or slow the host with a
fork storm, for up to 120 s. It cannot reach the network, credentials or host files
through this path. Today the candidate is the maintainer's own repository, so the
likelihood is low; it rises when untrusted contributors or shared runners execute
candidate code.

## How to re-check

```sh
git grep -n -e '--size' -e 'childDeadline' -e 'prlimit' -e 'MemoryMax' -- tools/cmd/lz-offline/main.go
```

Expected today: `--size` and `childDeadline` match; no `prlimit` or `MemoryMax`.

## Fix sketch

Run bwrap inside a transient cgroup with `MemoryMax` and `TasksMax` (for example a
`systemd-run --user --scope` unit, or a delegated cgroup on CI runners) and refuse to
start when the limits cannot be applied. Given a candidate that allocates beyond the
limit or forks beyond the task budget, when the entry runs it, then the child is
killed and the entry reports a resource-limit failure, while the valid boundary suite
still passes.
