# schemas

JSON Schemas for the project's public file formats. They are contracts: a change here is a
change for everyone who reads or writes those files.

| Path | What it describes |
| --- | --- |
| `deployments.schema.json` | The deployment manifest, [`stacks/deployments.yaml`](../stacks/deployments.yaml). |
| `outputs/envelope.schema.json` | A stack's published `outputs.json` envelope. |
| `outputs/<stage>.schema.json` | The `values` of that envelope, one schema per stage. |
| `check-report.schema.json` | One observation of a `tofu test -json` run, as the report adapter writes it. |

**Rule:** each schema mirrors Go types in `tools/internal/` (`stacks` for the manifest and
outputs, `report` for the check report), and a test fails when the two disagree. Change the
schema and the Go type in the same commit. Some rules live only in the Go decoder and validator;
the top-level `description` of the manifest and envelope schemas names them.

Checks, offline: `task test:outputs` (output schemas), `task test:stacks` (manifest schema) and
`task test:reports` (check-report schema).

More: [architecture overview](../docs/explanation/architecture-overview.md),
[glossary](../docs/reference/glossary.md), [repository map](../docs/reference/repository-map.md),
[ADR-0004](../docs/adr/0004-staged-roots-and-output-contracts.md) (output contracts).
