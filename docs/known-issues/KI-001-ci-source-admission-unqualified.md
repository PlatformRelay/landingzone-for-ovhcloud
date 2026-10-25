---
id: KI-001
title: CI workflow/evaluator source admission is unqualified (C2)
kind: limitation
status: accepted
likelihood: low
impact: high
severity: medium
verified:
  at: 804680b
  on: 2026-10-19
  how: code read; repository settings and ruleset 25423554 read through the GitHub API (read-only)
issue:
source: C2, offline-foundation CI source admission
decision: "#decision"
references:
  - path: specs/001-offline-foundation/spec.md
    blob: 5b18e1d04a145c62f2d840542b5fbf56563fabf7
  - path: .github/workflows/foundation.yml
    blob: b2c3d6d4f9b127743ccb0d08e8208fb0b996a5a8
  - path: pipelines/github/foundation-source.json
    blob: 83d664d922c1e9cabbc6971b1633e9d808fa8988
  - path: tools/internal/checks/foundation_ci.go
    blob: fae4afa68b5c56fb3a5b460c6a877226fcf5ae0c
last_verified: c5a00702293eb09a2c66e27f122fc0046925244f
---

## What is true

Code observations at `9f318ea`, re-read at `804680b`, and of the repository's settings on
2026-10-19. They are committed-source and settings observations, not runtime
qualification:

- `specs/001-offline-foundation/spec.md:106` (C010.5) and `:51` (P4) require
  independently approved workflow/evaluator source before candidate publication or
  runner execution, and defer that proof to this issue.
- Minimal foundation CI exists (spec 001 T023, closed). `.github/workflows/foundation.yml`
  is rendered from `pipelines/github/foundation-source.json` and judged by
  `CheckFoundationCI` in `tools/internal/checks/foundation_ci.go`: no token permission, no
  Action, no secret, a hosted runner, the head fetched as data, the runtime fetched by
  digest and every target run through the offline entry.
- Since D126 part A (2026-10-25, code read at `9200607`) the targets run in parallel
  jobs, and the check named `foundation` comes from a gate job that needs them all. Its
  condition is `if: ${{ always() }}`, which GitHub documents as running the job whatever
  its needs concluded, cancellation included
  ([GitHub: status check functions](https://docs.github.com/en/actions/reference/workflows-and-actions/expressions#status-check-functions)),
  so a failed or skipped job does not skip the gate. Its step fails unless every job's
  result is `success`. That GitHub behaviour is cited, not yet observed on a run of this
  workflow. `ci:foundation` requires the gate to have seen every job succeed on a head
  run and to have failed on the red control.
  The ruleset still requires `foundation` and `SonarCloud Code Analysis` (re-read through
  the API on 2026-10-25). Admission is unchanged by the split.
- None of that is admission. The workflow runs on `pull_request`, and the source file, the
  renderer and the check all sit in the candidate's own tree, so a candidate that changes
  them together passes its own check: a candidate-owned manifest approves itself. Which
  workflow file GitHub runs for a `pull_request` event is GitHub's documented behaviour,
  not tested here.
- `main` now has a ruleset. The owner set it up on 2026-10-19 (spec 006 out-of-scope row
  C3 left it to the owner). `GET .../rulesets/25423554` answered: ruleset `main`,
  `enforcement: active`, target the default branch, no bypass actors, and these rules:
  - required status checks `foundation` and `SonarCloud Code Analysis`, with no expected
    source app on either and `strict_required_status_checks_policy: false` (the branch
    need not be up to date with `main`);
  - `non_fast_forward` and `deletion`: no force-push, no branch deletion;
  - `pull_request`: a pull request is required, `allowed_merge_methods: ["rebase"]`,
    `required_approving_review_count: 0`.
- What the ruleset changes: a change reaches `main` only through a pull request, by
  rebase-merge, after both checks pass (GitHub counts `success`, `neutral` and `skipped` as
  passing, so a pass does not prove the evaluator ran), and `main` cannot be
  force-pushed or deleted. The bypass list is empty, so this holds for the owner too; an
  admin can still edit the ruleset itself.
- What it does not change: the `foundation` check is still produced by the candidate's
  own workflow, renderer and check, and no approval is required, so a candidate that
  edits all three together can still pass its own check and merge. Neither required
  check names an expected source app, so a passing check or status of that name from
  any source counts; where several share the name, all must pass
  ([GitHub: troubleshooting required status checks](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks)).
  That path was not exercised here. Admission stays unqualified. No
  disposable-repository admission experiment has run.
- The repository is now public (`gh repo view`: `PUBLIC`). That is the reassessment
  trigger in *Decision*; the reassessment is the owner's (spec 006 out-of-scope row C2)
  and has not happened, so the decision below still stands.
- Spec 006 did not change admission. Its CI changes were the CI source check tracing
  spec 006 (FR-022) and the pin of runtime r5 (PR 135). Later than the `9f318ea`
  observation, spec 006 T045 added two targets the workflow runs, `test:update-lane`
  and `test:release`; admission is unchanged by it. Spec 004's `test:configure` (creator
  T016, closed) joined job `foundation-3` on 2026-10-25, admitted by the same rule; admission
  is unchanged by it too.

The first version of this entry cited revision `996b845`, which is not in this
repository's history (`git cat-file -t 996b845` fails); its citations are replaced by
the ones above.

## Why it matters

A candidate-controlled workflow/evaluator could execute unreviewed code or report
success without enforcing the intended checks. Maintainers could then rely on
results that do not establish the source and head actually evaluated.

Likelihood was assessed low for the private-development scope accepted on 2026-10-02.
This is a scoped risk assessment, not an observed frequency or a preventive control:
private visibility alone does not prevent malicious workflow execution. Impact is high
because compromised evaluation can undermine the trust placed in CI results. Low
likelihood × high impact gives medium severity. The repository has since become public,
which the decision names as a reason to reassess; until the owner does, the fields above
keep the 2026-10-02 assessment.

## How to re-check

```sh
git grep -n -e 'C010.5' -e '^| P4 ' -- specs/001-offline-foundation/spec.md
git ls-tree --name-only HEAD -- .github/workflows/foundation.yml pipelines/github tools/internal/checks/foundation_ci.go
gh api repos/PlatformRelay/landingzone-for-ovhcloud/rulesets
gh api repos/PlatformRelay/landingzone-for-ovhcloud/rulesets/25423554
gh api repos/PlatformRelay/landingzone-for-ovhcloud/branches/main --jq .protected
gh repo view PlatformRelay/landingzone-for-ovhcloud --json visibility
```

The first command shows the deferred requirement, the second the CI files, the last
four the settings above. Update `verified`, the citations and `references` together.
Files and settings alone do not qualify admission; closure requires the behavioural
evidence below.

## Fix sketch

KI-001 owns the deferred source-admission qualification. Spec 001 T023 delivered the
minimal CI and does not own this deferred proof. Derive a future implementation task
from this item when scheduled, including the foundation evidence prerequisites. Future
acceptance:
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
checked by this code and settings read. The future implementation task derived from
KI-001 must provide the bounded proof and exact source/head evidence before any hardened
CI qualification claim; spec 001 T023 does not own that deferred proof.
