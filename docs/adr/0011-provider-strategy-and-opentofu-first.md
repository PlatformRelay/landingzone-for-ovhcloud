# ADR-0011: Provider strategy and OpenTofu-first
- Status: Accepted
- Date: 2026-10-06
- Related: ADR-0003, ADR-0009

## Context
The `ovh/ovh` provider (v2.21.0 on 2026-09-24, MPL-2.0, 177 resource doc pages = 99 SDK + 78
framework registrations; an earlier "~233" counted source files) covers IAM
(`ovh_iam_policy`, `ovh_iam_resource_group`, `ovh_iam_permissions_group`, `ovh_iam_resource_tags`),
identities (`ovh_me_identity_user/group`, `ovh_me_api_oauth2_client`), projects (`ovh_cloud_project`),
vRack and private networks, gateways, load balancers, Managed Kubernetes, Object Storage and S3 policies,
databases, alerting, Logs Data Platform, OKMS, quotas (`ovh_cloud_quota`) and budget alerts
(`ovh_cloud_project_alerting`). Confirmed gaps: SAML identity provider, organisation-level objects.
Public Cloud compute and network are also reachable through the OpenStack API/provider, with a
per-project OpenStack user or the IAM service-account bridge (ADR-0018). OpenTofu 1.11 added
ephemeral resources, write-only attributes and the `enabled` meta-argument; Terraform has ephemeral
resources since 1.10 and write-only arguments since 1.11, but not `enabled` or state encryption.

## Options considered
- Terraform-compatible lowest common denominator.
- **OpenTofu-first**, Terraform compatibility best effort and tested where cheap.
- Both fully supported, tested in CI.

## Decision
- **OpenTofu is the supported engine** (minimum 1.13). Terraform compatibility is a separately
  labelled, **version-specific qualification for selected modules**, generated from a passing
  qualification job; a failing qualification removes the label before release. There is no
  "informational" job that can stay red behind a badge (ADR-0022).
- **Providers:** prefer `ovh/ovh` where its resource lifecycle and controls satisfy the contract; use
  `terraform-provider-openstack/openstack` where the required **semantics** are absent or inadequate
  (strict security groups with `delete_default_rules` is the first case), with one writer per cloud
  object and a documented ownership boundary (`catalog/resource-ownership.yaml`).
- **No custom provider** (Azure's `alz` provider showed the cost: bespoke behaviour, no `depends_on`).
- **Pinning:** `.terraform.lock.hcl` committed for stages and examples, not for library modules;
  constraints `~>` minor; a scheduled job tests the newest provider release and opens an issue on break.
- **Gap register:** `docs/reference/provider-gaps.md` lists things a landing zone wants that the provider
  does not offer, each with: workaround (API script, manual runbook, or none), upstream issue link, and
  the module that is affected. This is a deliverable, kept current. Seeded on 2026-10-01 from the
  provider docs listing: **SAML identity provider** (`/me/identity/provider` has no resource; singleton
  per account) → `pending_actions` + scanner check (ADR-0018); OAuth2 client **secret rotation with
  overlap** (two live secrets? UNVERIFIED) → client-swap rotation; **audit/login event stream**
  (UNVERIFIED) → spike; **cost estimate** → public catalogue API script (ADR-0006). Confirmed present
  and used: `ovh_iam_policy` (deny, conditions, `expired_at`), `ovh_cloud_quota`,
  `ovh_cloud_project_alerting`, `ovh_cloud_project_kube_oidc`, `ovh_me_identity_user_token`,
  `ovh_me_api_oauth2_client`, the `ovh_vrack_*` family.
- **Minimum OpenTofu 1.13** (wildcard overrides in tests, ADR-0008); Terramate CLI (MPL-2.0, 0.17.x)
  pinned for the instance layer (ADR-0007); `mock_provider` `source` (1.14)
  adopted when released to separate `ovh/ovh` from `openstack` mocks.
- **Custom endpoint:** go-ovh accepts an arbitrary endpoint URL; the provider's `endpoint` is used by
  the record/replay proxy (ADR-0008). Whether the provider passes a full URL through unchanged is a spike.
- **Provider schema snapshot** (`tofu providers schema -json`) committed and diffed on every bump;
  Renovate bumps run the canary lane (ADR-0008 L10).

## Consequences
- Contributors need OpenTofu; Terraform users may hit unsupported features and are told which.
- The gap register doubles as upstream feedback to OVHcloud.

## Counterpoints
- OpenTofu-first excludes shops standardised on HCP Terraform; acceptable, because HCP's free tier ended
  and it cannot run OpenTofu.
- Isolating OpenStack-provider usage is extra structure for what may be few resources.

## Verification
- Spike: for each landing-zone pillar, list the exact provider resources needed and mark covered/gap
  against provider docs for 2.21.x; verify with `tofu validate` on a skeleton.

## Amendments
- 2026-10-07 (*Pinning*): a provider-using library module commits a **test lock file**: the offline
  entry runs its tests and lint with `init -lockfile=readonly`, which fails without one (spec 005
  T013). The effective lock stays with the consuming root (stage, example, stack); a library's lock
  file pins only its own tests, with hashes for the entry's platform (linux_amd64); another platform
  is added with `tofu providers lock -platform=…`.

## Review log
- 2026-10-01: round-2 external adversarial review applied.
