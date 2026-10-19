# tools

One Go module (`go.mod`) with the command-line tools that check, generate and run the landing
zone. Most `task` targets in `Taskfile.yml` call one of them.

| Path | What it holds |
| --- | --- |
| `cmd/lz-check` | The checks: layer rules, lint, unit tests, slice, specs, DoD, CI source, docs. |
| `cmd/lz-offline` | The offline entry: the sandbox every check runs in, with the pinned toolchain and no network. |
| `cmd/lz-stacks` | Stack reconcile, generation, freshness check, offline plans and apply order. |
| `cmd/lz-live` | The live lane to a real account. Host only, maintainer only; it refuses to run inside the entry. |
| `cmd/lz-pack` | Packs the runtime bundle and launcher into a reproducible OCI image. |
| `cmd/lz-release` | Renders the release-train manifest of a commit from its git objects. Host only, read-only: the entry carries no `.git`. |
| `cmd/json2junit` | Renders a `tofu test -json` stream as JUnit XML. |
| `internal/` | Their libraries: `checks`, `stacks`, `live`, `probes`, `bundle`, `report`, `planjson`, `release`. |
| `vendor/` | Vendored dependencies, so builds need no network. |
| `scripts/` | Host scripts, not Go: `fetch-offline-entry.sh` fetches, verifies and installs the offline entry ([README](../README.md#try-it-offline-about-5-minutes-no-credentials)). |
| `harness/` | Data, not Go: the check registry (`checks.yaml`) and what the offline entry may do (`capabilities.yaml`). |

**Rules:** no credentials in code, tests or fixtures; tests use fakes and captured output, never
a real account. A new dependency is vendored (`go mod vendor`), because the entry has no network.

Checks: CI runs the `task` targets in the `LZ_TARGETS` list of
`.github/workflows/foundation.yml`; most of them run the Go tests of these packages, for example
`task test:traceability`, `task test:dependencies`, `task test:stacks` and `task test:live-lane`.
`task --list` names every target. Run them through the entry as the
[README](../README.md#try-it-offline-about-5-minutes-no-credentials) shows.

More: [architecture overview](../docs/explanation/architecture-overview.md),
[glossary](../docs/reference/glossary.md), [repository map](../docs/reference/repository-map.md),
[ADR-0008](../docs/adr/0008-testing-strategy.md) (testing) and
[ADR-0019](../docs/adr/0019-agent-experience-sensors-and-guides.md) (checks as sensors).
