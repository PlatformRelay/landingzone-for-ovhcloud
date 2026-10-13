# Contributing

Changes come as pull requests against `main`; a pull request that depends on another targets
its parent branch and says so, and the parent is reviewed first. Every change is
offline-checkable: you need no OVHcloud account and no credentials. Applying to a real account is
the maintainer's job today (see *Live runs* below).

Before you start, read:

- [`AGENTS.md`](../AGENTS.md), the working rules for everyone, human or automated. It is short.
- The [repository map](../docs/reference/repository-map.md) and the
  [architecture overview](../docs/explanation/architecture-overview.md) to find your way.
- The [glossary](../docs/reference/glossary.md) for ids such as Tnnn, Pn and KD-n.

## The working rules, in brief

[`AGENTS.md`](../AGENTS.md) is the source; this is a summary.

- Work on a branch in a git worktree with a clean tree, one logical change per commit, every
  commit green.
- Credentials never enter the repository, a command line or any output. Account and project ids
  stay out of committed files.
- A claim about OVHcloud behaviour cites a source (docs.ovhcloud.com, the API schema or the
  provider docs) or is marked UNVERIFIED.
- Nothing in commits, pull requests or committed files names the tool or model that authored it.

## Pick and close work

Work is planned in [`specs/`](../specs/README.md), one folder per feature (spec, plan, tasks,
evidence). The design is in the [ADRs](../docs/adr/README.md) and the project constitution
(`.specify/memory/constitution.md`).

1. Read the task in the spec's `tasks.md`, its requirements, its Verify and Evidence lines, the
   ADRs it cites and its dependencies. A checked box upstream is not proof that a dependency works
   for your scope.
2. Write the failing test first, and check that it fails for the right reason: a missing
   executable or a syntax error is a blocked test, not a red one. Keep a control that passes.
3. Make it pass with the smallest coherent change, run the same controls green, then run the
   checks below. Record the revision, the tool versions and the exact command with its result.
4. Have the diff reviewed by someone (or a session) that did not write it. Verify each finding
   against the cited code before you fix it.
5. Close only what the evidence supports. The closing record is `specs/<spec>/evidence/Tnnn.md`:
   what ran, its result, and what was not verified. Docs-only work needs a content review, not
   invented behavioural tests.

Stop and record the reason, rather than working around it, at an unanswered decision, an unmet
dependency, an unverified premise outside your task or a failing required check. Then carry on
with other work that does not depend on it.

## Checks to run

Every check runs inside the offline entry `lz-offline`; the [README](../README.md#try-it-offline-about-5-minutes-no-credentials)
shows how to get it. Then, from the root of your clone:

```sh
"$LZ_RUNTIME/runtime/lz-offline" --candidate "$PWD" -- task <target>
```

- **Docs only:** `docs:check`, `check:specs` and `test:traceability`. Check every relative link
  you add or move; a moved page's inbound links change in the same commit.
- **Code or configuration:** every target in `LZ_TARGETS` of
  [`.github/workflows/foundation.yml`](workflows/foundation.yml), which is what CI runs.
- `task dod -- <path>` shows the definition of done for a path; `task --list` shows every target.

A page under `docs/` with `references:` front matter pins the files it describes by git blob id.
When one of those files changes, re-read the page, then update its blob ids and `last_verified`
([ADR-0013](../docs/adr/0013-documentation-strategy.md)).

Never widen an allow-list, an exclusion list or a baseline to get green. Loosening a gate needs a
written justification on its own line in the commit body
([ADR-0008](../docs/adr/0008-testing-strategy.md)).

## Commits

```
:gitmoji: type(scope): short summary

Optional body: why, and the trade-offs.
```

The gitmoji is an ASCII shortcode (`:sparkles:`, `:bug:`, `:memo:`), not a Unicode emoji. Types:
`feat fix docs style refactor test chore ci build`. No `Co-Authored-By:` or other attribution
trailers.

## Merging

Pull requests land by **rebase and merge**: linear history, no squash, no merge commits
([ADR-0010](../docs/adr/0010-versioning-release-and-distribution.md)). A pull request merges after
an independent review and with every check green; a missing check is not a green one.

## Live runs

Live runs spend a one-off trial credit and act on a real account. Only the maintainer runs them,
from the maintainer's workstation, by the
[runbook](../docs/how-to/run-the-first-slice.md) (known deviation KD-2). A contribution never
needs `task bootstrap:*` or `task live:*`. The full live suite runs after a change to IAM, state
or credentials, so say in the pull request when yours is one
([`AGENTS.md`](../AGENTS.md) *Sandbox budget*).

## Security

Report vulnerabilities privately, as described in [SECURITY.md](SECURITY.md), not in a public
issue.
