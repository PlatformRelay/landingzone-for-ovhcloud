# stacks

The deployed copies of the stages. Each stack is one generated directory that calls one stage
from [`stages/`](../stages/README.md) and holds its own backend, provider block and state. Stage =
reusable code; stack = one deployed instance of a stage with its own state.

**Generated: do not edit stack directories by hand.** Edit only
[`deployments.yaml`](deployments.yaml), the list of instances (one row per stack). Then, on the
host, run `task stacks:reconcile` (creates the missing stack directories and their
`stack.tm.hcl`) and `task stacks:generate` (renders their `_lz_*` files with Terramate and copies
the stage's `.terraform.lock.hcl` into each stack without one; it refuses a row not yet
reconciled, and a stack whose stage has no lock), and
commit the result. A stack's path is its state key: moving or renaming one is a state migration,
not a rename.

| Path | What it holds |
| --- | --- |
| `deployments.yaml` | The deployment manifest; its contract is `schemas/deployments.schema.json`. |
| `_lz/`, `lz.tm.hcl` | The hand-written Terramate templates every stack is generated from. |
| `account/` | Account-level stacks: `bootstrap`, `account-governance`, `tenant-state/<tenant>`. |
| `tenants/` | Tenant stacks: `<tenant>/<env>/project`, `<tenant>/<env>/<region>/{project-network,runtime}`. |

Checks, offline: `task stacks:check` (the committed files match what the manifest generates; CI
does not run it yet, so run it before you commit),
`task test:stack-plans` (every stack plans against mocks), `task test:stacks` (manifest decoder,
reconciler and generator) and `task stacks:order -- all` (the apply order). Applying to an
account is maintainer-only: see the [runbook](../docs/how-to/run-the-first-slice.md).

More: [architecture overview](../docs/explanation/architecture-overview.md),
[glossary](../docs/reference/glossary.md), [repository map](../docs/reference/repository-map.md),
[ADR-0004](../docs/adr/0004-staged-roots-and-output-contracts.md) (one state per stack, outputs
as files).
