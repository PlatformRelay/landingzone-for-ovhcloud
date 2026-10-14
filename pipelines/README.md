# pipelines

The CI source and the record of CI runs. Today there is one forge, GitHub, and one workflow.

| Path | What it holds |
| --- | --- |
| `github/foundation-source.json` | The approved source of `.github/workflows/foundation.yml`: runner, the offline entry's pinned digests, and the targets CI runs. |
| `github/foundation-runs.json` | Recorded GitHub runs of that workflow, judged by `task ci:foundation`. |

**Rule: the workflow is generated; do not edit it by hand.** Edit the source, then on the host
run `task generate:foundation-ci` to render `.github/workflows/foundation.yml`, and commit both.
The run record is built on the host from read-only GitHub captures with `lz-check ci-observe`.
No credentials here.

Checks, offline: `task test:foundation-ci` judges the source against the check registry and the
committed workflow against the rendered one. `task ci:foundation` judges the recorded runs; CI
does not run it, because a run cannot judge itself.

More: [architecture overview](../docs/explanation/architecture-overview.md),
[glossary](../docs/reference/glossary.md), [repository map](../docs/reference/repository-map.md),
[ADR-0007](../docs/adr/0007-pipeline-portability.md) (pipeline portability; `gitlab/` and
others are planned).
