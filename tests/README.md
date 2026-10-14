# tests

Test data for the checks, and the live probe roots. The unit tests of modules, components and
stages live next to them (`<dir>/tests/`); the Go tests live in [`tools/`](../tools/README.md).

| Path | What it holds |
| --- | --- |
| `check/fixtures/` | Valid and broken inputs for `lz-check`'s own tests (`dependencies`, `static`, `unit`). |
| `fixtures/` | Deployment manifests, output envelopes, and captured `tofu`, Terramate and `ovhcloud` output that the Go tests replay. |
| `live/probes/` | Live probe roots, one per premise group. Maintainer only: see its [run sheet](live/probes/README.md). |
| `live/chain/` | Expected and observed live-chain results that the chain's assertions are tested against. |

**Rules:** no credentials, and no real account or project ids (fixtures use fake ones). Captured
output comes from a recorder (the `capture.sh` beside it, a `task capture:*` target, or an
anonymised live run): re-record it rather than edit it by hand. Nothing here touches an account
except a probe run through `task live:probe` by the maintainer.

Checks: this directory has none of its own; the targets that read it do, for example
`task test:dependencies`, `task test:static`, `task test:outputs`, `task test:stacks` and
`task test:live-lane` (which also runs most probe roots' mocked tests offline).

More: [architecture overview](../docs/explanation/architecture-overview.md#what-runs-offline-and-what-runs-on-the-host),
[glossary](../docs/reference/glossary.md), [repository map](../docs/reference/repository-map.md),
[ADR-0008](../docs/adr/0008-testing-strategy.md) (test layers and the protected live root).
