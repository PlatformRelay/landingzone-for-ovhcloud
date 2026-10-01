# ADR-0011: Provider strategy and OpenTofu-first
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0003, ADR-0009

## Context
The `ovh/ovh` provider (v2.21.0 on 2026-09-24, MPL-2.0, ~233 resource files) covers IAM
(`ovh_iam_policy`, `ovh_iam_resource_group`, `ovh_iam_permission_group`, `ovh_iam_resource_tags`),
identities (`ovh_me_identity_user/group`, `ovh_me_api_oauth2_client`), projects (`ovh_cloud_project`),
vRack and private networks, gateways, load balancers, Managed Kubernetes, Object Storage and S3 policies,
databases, alerting, Logs Data Platform and OKMS. Gaps we could not confirm: budget/cost resources,
SSO/SAML federation resources, quota requests, organisation-level objects. Public Cloud compute and
network are also reachable through the OpenStack API/provider with a per-project OpenStack user.
OpenTofu 1.11 added ephemeral resources, write-only attributes and the `enabled` meta-argument; these
have no Terraform equivalent at that version (research, to verify against the OpenTofu docs).

## Options considered
- Terraform-compatible lowest common denominator.
- **OpenTofu-first**, Terraform compatibility best effort and tested where cheap.
- Both fully supported, tested in CI.

## Decision (proposed)
- **OpenTofu is the reference runtime**; minimum version set to the lowest release providing the features
  ADR-0009 needs (ephemeral resources, state encryption). Terraform compatibility is a CI *informational*
  job, not a gate, and docs say which modules are Terraform-safe.
- **Providers:** `ovh/ovh` is primary; `terraform-provider-openstack` only where a resource has no OVH
  provider equivalent, isolated in `modules/` with a documented reason in each module README.
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
- **Minimum OpenTofu 1.13** (wildcard overrides in tests, ADR-0008); `mock_provider` `source` (1.14)
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

## Review log
- 2026-10-01 revision: gap register seeded from verified provider docs; OpenTofu 1.13 minimum;
  endpoint note; schema snapshot. Source: agent-context/research/BRAINSTORM-2026-10-01-round2.md.
