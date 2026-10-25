---
references:
  - path: docs/adr/0011-provider-strategy-and-opentofu-first.md
    blob: 995495a14dc93e285e5a66cb8a412eeb6f4fc6a2
  - path: docs/adr/0018-identity-planes-roles-and-providers.md
    blob: 638486606c33576b87c1bc3a0b648a46d1c11354
  - path: specs/003-platform-feasibility/research.md
    blob: b8585548b65621d609f272455b3b3a9e64dcbd29
  - path: specs/003-platform-feasibility/evidence/T030.md
    blob: 9253f1dc6af8b0a1e5f8ed8544ef117938b4b4e1
last_verified: fb968189fcc70955232a704509663c595151aeeb
---

# Provider gaps

Things a landing zone wants that the `ovh/ovh` provider does not offer. This is the gap register
that [ADR-0011](../adr/0011-provider-strategy-and-opentofu-first.md#decision) asks for. Each row
gives the workaround (an API script, a manual runbook, or none), the upstream issue, and the
module or code it affects. A claim that has not been checked against the provider or the API is
marked UNVERIFIED. The register also serves as feedback to OVHcloud.

## Gaps

| Gap | Workaround | Upstream issue | Affected | Source | Status |
| --- | --- | --- | --- | --- | --- |
| SAML identity provider: `/me/identity/provider` has no resource, and the federation is a singleton per account | `pending_actions` plus a scanner check ([ADR-0018](../adr/0018-identity-planes-roles-and-providers.md)) | none filed | the federated identity family, not built yet | ADR-0011 seed, 2026-10-01, from the provider docs listing | seeded |
| OAuth2 client secret rotation with overlap (two live secrets at once) | client-swap rotation: a new client, then the old one revoked | none filed | `modules/iam-service-account` | ADR-0011 seed | UNVERIFIED that the API cannot hold two live secrets |
| Audit and login event stream | none yet; a spike | none filed | none yet | ADR-0011 seed | UNVERIFIED that the provider lacks it |
| Cost estimate | a script against the public catalogue API ([ADR-0006](../adr/0006-guardrails-without-org-level-policy.md)) | none filed | none yet; the script is not built | ADR-0011 seed | seeded |
| Swift container: the provider has no resource for one; `ovh_cloud_project_storage` makes an S3-API bucket | the swift-reach check creates and deletes its control containers through the API; the leftover listing lists them by name as the kind `swift_container` | none filed | `tests/live/probes/swift-reach`, `tools/internal/live/leftovers.go` | [spec 003 research](../../specs/003-platform-feasibility/research.md) (decision on S3-API buckets and the Swift plane; the provider's `docs/resources/` listed) | verified from the provider docs |
| Object in a bucket: the provider has no object resource (its storage resources are the bucket, its lifecycle, replication and presign) | the swift-reach check writes its seed object with the bucket's cleanup S3 key | none filed | `tests/live/probes/swift-reach`, `tools/internal/probes/live/storageiam` | [spec 003 T030](../../specs/003-platform-feasibility/evidence/T030.md#design-notes-and-deviations) (provider docs, mirror commit 00cb8b0, checked 2026-10-16) | verified from the provider docs |

## Present and used

ADR-0011 records these as present in the provider when it was seeded on 2026-10-01:
`ovh_iam_policy` (deny, conditions, `expired_at`), `ovh_cloud_quota`,
`ovh_cloud_project_alerting`, `ovh_cloud_project_kube_oidc`, `ovh_me_identity_user_token`,
`ovh_me_api_oauth2_client` and the `ovh_vrack_*` family. Of these, the modules and stages use
`ovh_iam_policy`, `ovh_cloud_quota`, `ovh_cloud_project_alerting` and `ovh_me_api_oauth2_client`
today.

## Keeping this page current

Add a row when a spec, a probe or a review finds that the provider lacks something, and cite it.
A row from the seed stays UNVERIFIED until a source or a probe settles it. Link the upstream issue
once one is filed. Remove a row only when a provider release covers the gap, and name that
release.
