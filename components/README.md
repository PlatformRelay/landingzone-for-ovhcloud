# components

Layer 2: landing-zone features built from [modules](../modules/README.md). A component is
either a family with variants behind one output contract, at `<family>/<variant>`, or a
singleton.

| Path | What it holds |
| --- | --- |
| `state-backend/` | A state bucket and its S3 users (singleton; used by `bootstrap` and `tenant-state`). |
| `identity/ovh-native/` | Deployer clients, policies and groups in OVHcloud IAM. |
| `project-factory/` | Reference or adopt a project, with quota and budget alert switches (singleton). |
| `network/island/` | One private network and subnet in one region, with no gateway. |
| `runtime/managed-only/` | The runtime placeholder: one object storage bucket, no compute ([README](runtime/managed-only/README.md)). |

**Rules:** a component calls modules and `modules/naming`, never another component or a stage.
It configures no provider and no backend, and holds no credentials. Each one has `main.tf`,
`variables.tf`, `outputs.tf`, `versions.tf` and mocked tests in `tests/*.tftest.hcl`.

Checks, offline: `task lint -- components/<path>` and `task test:unit -- components/<path>` for
one component, `task test:slice` for every module, component and stage, and
`task test:dependencies` for the layer rules.

More: [architecture overview](../docs/explanation/architecture-overview.md#four-layers),
[glossary](../docs/reference/glossary.md), [repository map](../docs/reference/repository-map.md),
[ADR-0003](../docs/adr/0003-layered-taxonomy-and-module-naming.md) (layers),
[ADR-0017](../docs/adr/0017-runtime-families-and-output-contract.md) (families and variants).
