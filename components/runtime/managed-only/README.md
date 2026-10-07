# runtime/managed-only

One empty, labelled Object Storage bucket for a managed-only runtime, and the runtime envelope's
values (spec 005 T031/T032; ADR-0017; research R5, R8, R14, R22). The bucket goes through the
plain, replaceable [`modules/object-storage`](../../../modules/object-storage/README.md), never the
protected state-bucket module; its name and tags come from
[`modules/naming`](../../../modules/naming/README.md) (kind `bucket`, role `runtime`, the
instance's optional `slot`, so two slots give two bucket names). The `runtime` stage calls this
component exactly once (`lz-check deps`, `singleComponentStages`).

Outputs: `kind` (`managed-only`), `slot` (null when unset), `scope {instance, project_id, region}`,
`readiness` (`ready`), `pending_actions` (`[]`), `capabilities` with only `object-storage
{bucket, endpoint, region}` (no `network` key, not even an empty one), `unlabelled` (`[]`: the
bucket carries tags) and `labels` (the bucket's tags; the stage does not publish them).

## Object Storage region

The bucket's region is the instance region's leading letters, upper case (`GRA11` → `GRA`,
`SBG5` → `SBG`); the endpoint is `https://s3.<lower region>.io.cloud.ovh.net`. The endpoint form
is OVHcloud's (`storage-and-backup/object-storage/s3-post-object-upload.mdx`, kb mirror, read
2026-10-07), and the provider's storage example uses `GRA` (`cloud_project_storage.md`). The
mapping from a compute region to its Object Storage region, and so the endpoint host the
component publishes for it (standard storage class only), is **UNVERIFIED until T010** shows it
on a live create, and is believed wrong for 3-AZ regions (e.g. `EU-WEST-PAR`, a region id in the
API schema `api/v2/publicCloud.json`; its leading letters are `EU`): revisit before a 3-AZ region enters a manifest (T031 decision request 2). The
derivation never fails on a string, so a refused region fails only its own rule.

Tests: `tests/unit.tftest.hcl`, `tests/replaceable.tftest.hcl` (mocked provider, no credential);
`task test:unit -- components/runtime/managed-only`. The stage's real plan is pinned in
`tools/internal/stacks/outputs_stage_test.go` (`TestOutputsRuntimeStagePlan`).
