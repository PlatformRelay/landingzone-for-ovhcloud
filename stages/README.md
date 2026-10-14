# stages

Layer 3: one reusable module per deployment step (`bootstrap`, `tenant-state`,
`account-governance`, `project`, `project-network`, `runtime`). Stage = reusable code; stack = one
deployed instance of a stage with its own state, generated under [`stacks/`](../stacks/README.md).
The stage table (order, authority, retained or ephemeral) is in the
[architecture overview](../docs/explanation/architecture-overview.md#stages-their-order-and-who-applies-them)
and fixed in code in `tools/internal/stacks/stages.go`.

**Rule: no state, no credentials, no resources.** A stage calls one component, once, and declares
no resource, backend or provider configuration of its own. Its outputs are what its stack
publishes in `outputs.json`, so they must match `schemas/outputs/<stage>.schema.json`.

Each stage is `main.tf`, `variables.tf`, `outputs.tf`, `versions.tf` and its mocked tests in
`tests/*.tftest.hcl`.

Checks, offline: `task lint -- stages/<name>` and `task test:unit -- stages/<name>` for one
stage, `task test:slice` for every module, component and stage, `task test:dependencies` for the
layer rules, and `task test:outputs` for the output envelopes and their schemas.

More: [glossary](../docs/reference/glossary.md),
[repository map](../docs/reference/repository-map.md),
[ADR-0004](../docs/adr/0004-staged-roots-and-output-contracts.md) (stages, stacks and output
contracts).
