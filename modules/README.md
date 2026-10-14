# modules

Layer 1: thin wrappers over one OVHcloud product concern each (a project, a bucket, an IAM
policy, a private network), plus [`naming`](naming/README.md), the pure module every name and
label comes from. The [repository map](../docs/reference/repository-map.md#the-landing-zone-what-gets-deployed)
lists all eleven.

**Rules:** a module may call only `modules/naming`. It configures no provider and no backend,
and holds no credentials. Today's modules take their name and labels as inputs; the calling
component gets them from `modules/naming`.

Each module is `main.tf`, `variables.tf`, `outputs.tf`, `versions.tf`, mocked tests in
`tests/*.tftest.hcl`, and usually a `README.md` with its inputs and outputs. No tool generates or
checks these READMEs; keep them in step with the code by hand.

Checks, offline: `task lint -- modules/<name>` (pinned fmt, init, validate, TFLint) and
`task test:unit -- modules/<name>` for one module, `task test:slice` for every module, component
and stage, and `task test:dependencies` for the layer rules.

More: [architecture overview](../docs/explanation/architecture-overview.md#four-layers),
[glossary](../docs/reference/glossary.md),
[ADR-0003](../docs/adr/0003-layered-taxonomy-and-module-naming.md) (layers and naming).
