---
id: KI-001
title: CI workflow/evaluator source admission is unqualified (C2)
kind: limitation
status: accepted
likelihood: low
impact: high
severity: medium
verified:
  at: 996b845
  on: 2026-10-02
  how: code read
issue:
source: C2, offline-foundation CI source admission
decision: "#decision"
---

## What is true

These are committed-source observations at `996b845`, not runtime qualification:

- `specs/001-offline-foundation/spec.md:92` requires reviewed source/image identities
  bound to the candidate head; `:94` requires workflow/evaluator admission before
  candidate publication or runner execution.
- `specs/001-offline-foundation/tasks.md:203` leaves T023 unchecked and names its
  planned workflow, source manifest, checker and Taskfile. The committed tree has
  no files under `.github/workflows`, `pipelines/github` or `tools/internal/checks`,
  and no `Taskfile.yml`; absence is a tree observation, not a line-level citation.
- `specs/001-offline-foundation/tasks.md:207` specifies valid admission and malicious
  workflow controls; `:210` gives a planned evidence destination with initial status
  `not-run`. Neither establishes an executed proof.

## Why it matters

A candidate-controlled workflow/evaluator could execute unreviewed code or report
success without enforcing the intended checks. Maintainers could then rely on
results that do not establish the source and head actually evaluated.

Likelihood is low for the currently accepted private-development scope with no
public/contributor/privileged CI expansion. This is a scoped risk assessment, not
an observed frequency or a preventive control: private visibility alone does not
prevent malicious workflow execution. Impact is high because compromised evaluation
can undermine the trust placed in CI results. Low likelihood × high impact gives
medium severity. Expanded exposure or CI authority requires reassessment.

## How to re-check

```sh
git show 996b845:specs/001-offline-foundation/spec.md | nl -ba | sed -n '89,95p'
git show 996b845:specs/001-offline-foundation/tasks.md | nl -ba | sed -n '203,210p'
git ls-tree -r --name-only 996b845 -- .github/workflows pipelines/github tools/internal/checks Taskfile.yml
```

The first two commands show requirements and the unchecked task; the last returns
no paths. Repeat against the revision under review to assess changes, updating
`verified` and citations together. Files appearing alone do not qualify admission;
closure requires the behavioral evidence below.

## Fix sketch

KI-001 owns the deferred source-admission qualification. T023 at `996b845` is the
original proposal, not the owner of this deferred proof; active T023 covers minimal
CI. Derive a future implementation task from this item when scheduled, including
the foundation evidence prerequisites. Future acceptance:

- Given a disposable-repository setup with Actions disabled and that state read
  back, when the independently approved source closure is initially published,
  then no candidate run is queued or started, and a no-bypass freeze of all workflow
  paths or a proven equivalent is verified before Actions is enabled.
- Given independently approved workflow/evaluator source and a valid candidate,
  when admission is exercised, then approval is enforced before execution and
  actual run/check evidence binds exact workflow/launcher/image/publisher identities
  and candidate head, with nonzero discovery and successful required checks.
- Given the approved source closure and enforced admission, when the intended
  initial publication, unchanged-source candidate push and final rebase-merge
  history are exercised, then each is admitted while source bytes remain approved;
  ordinary file-push success alone is insufficient.
- Given an unreviewed workflow edit, deletion, rename, new automatic YAML or second
  source push, when available Git push/API paths are exercised, then admission
  refuses it before runner execution; a candidate-owned manifest cannot approve itself.
- Given stale/mismatched source or head, missing evidence, zero discovery,
  failed/cancelled/skipped checks, widened authority or malicious candidate host
  execution, when qualification is evaluated, then it fails with an observable
  reason instead of claiming hardened CI success.
- Given setup failure, source change or probe completion, when cleanup/recovery
  runs, then temporary runs are cancelled and recorded temporary resources are
  removed; failure or source change leaves Actions disabled and candidate
  admission suspended within the qualification setup until requalified. Cleanup
  errors fail qualification, and enforcement is never removed while execution is
  enabled. These future proof controls do not change the current accepted deferral.

Retain failing-first evaluator tests, passing controls, deliberately broken controls
and disposable-repository admission observations before closing this issue. Update
qualification documentation with the exact tested scope. Full T010–T012 forge
qualification and unrelated security work remain outside this issue.

## Decision

**2026-10-02 — Accepted limitation.** Defer C2 as nonblocking for private development,
PR creation and merge. This permits continued development while qualification remains
outstanding; it is neither test success nor hardened CI qualification. Ordinary
applicable test, review and merge gates still apply. This acceptance adds no mandatory
approval gate and does not waive other failures.

**Owner:** Repository maintainer; implementation assignee to be set on reassessment.
**Trigger:** Reassess before public/contributor/privileged CI expansion. The trigger
does not silently or immediately reblock current private development, PR creation
or merge. The historical blocking language in the cited revision is not the current
disposition recorded by this decision.

## Open questions

Actual remote admission enforcement and valid/negative runtime outcomes were not
checked by this code read. The future implementation task derived from KI-001 must
provide the bounded proof and exact source/head evidence before any hardened CI
qualification claim; active T023 does not own that deferred proof.
