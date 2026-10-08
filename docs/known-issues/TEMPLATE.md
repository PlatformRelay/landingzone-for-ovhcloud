---
id: KI-NNN
title: One line stating what is wrong
kind: bug
status: open
likelihood: medium
impact: high
severity: high
verified:
  at: 0000000
  on: YYYY-MM-DD
  how: code read
issue:
source:
decision:
references: []
last_verified: c3c7e7adf8ed8aa588669817f16c58da7294a33c
---

Use `bug | security | limitation | debt` for kind, `open | accepted | wont-fix`
for status, and `high | medium | low` for likelihood and impact. Derive severity
from the index matrix. Verification methods are `code read | executed | test |
claimed`. `issue` is an optional delivery link; `source` names the finding's origin.
For an accepted limitation, set `decision: "#decision"` and retain the section below.
`references` and `last_verified` are the docs header of ADR-0013: list each file the issue
describes with its blob id (`git hash-object <path>`, which is `HEAD:<path>` once committed)
and the commit you checked against; `task docs:check` reports the page stale once a listed
file changes.

## What is true

State the verified facts with `path:line` references at `verified.at`. Distinguish
missing implementation, planned evidence and runtime observations.

## Why it matters

Name who is affected and explain likelihood, impact and what would change the rating.

## How to re-check

Give reproducible read-only commands or a bounded test and its expected outcome.

```sh
git grep -n 'PATTERN' COMMIT -- path/to/file
```

## Fix sketch

Describe the smallest outcome that closes the issue and link existing tasks instead
of duplicating their procedure. Include Given/When/Then valid and negative outcomes
and the evidence needed to verify them.

## Decision

For an accepted limitation, record the date, accepted scope, rationale, owner and
exact reassessment trigger here. State what acceptance does and does not establish.
Omit this section for issues without a decision.

## Open questions

State what remains unverified and what would settle it. Omit if none.
