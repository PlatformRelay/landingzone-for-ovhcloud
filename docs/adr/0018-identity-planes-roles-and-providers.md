# ADR-0018: Identity model — three planes, a role catalogue, pluggable providers
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0005, ADR-0006, ADR-0009, ADR-0016, ADR-0017

## Context
Verified on 2026-10-01 (agent-context/research/round2/FACTS): OVHcloud IAM has users, groups,
service accounts (OAuth2 clients, `ovh_me_api_oauth2_client`), identity-user tokens with expiry
(`ovh_me_identity_user_token`), and SAML 2.0 federation that is a **singleton per account**
(`/me/identity/provider`, GET/POST/PUT/DELETE) with **no Terraform resource**. Public Cloud projects
have a second plane, OpenStack Keystone users and application credentials. Managed Kubernetes has a
third, OIDC (`ovh_cloud_project_kube_oidc`). The operator requires multiple IAM providers.
Consequence of the singleton: "multiple IAM providers" means *one* federated IdP per account
(Entra ID, Okta, Keycloak, Google Workspace, AD FS — all SAML 2.0 on the OVH side) plus native
users, and the choice must be swappable without touching anything else.

## Options considered
- **A. One IAM module with a `provider = var.idp` switch** — untestable branching.
- **B. Adapters per plane × source behind one contract; IdP-side setup documented, not coded.**
- **C. Keycloak mandatory as the single broker** — simplest to test, excludes shops with Entra/Okta.

## Decision (proposed)
Option B, with C as the reference IdP for CI.

**Role catalogue (public API, fixed in v1):** `platform-admin`, `tenant-owner`, `tenant-developer`,
`tenant-viewer`, `ci-deployer`. Policies per role are action lists per resource family in
`components/identity/roles/`, reviewed like code.

**Adapters** `components/identity/<plane>-<source>/`, each taking `{tenant, env, role → groups}`
and emitting the contract `{ principals, groups, bindings, pending_actions }`:
- `ovh-native`: groups, users, service accounts in OVH IAM.
- `ovh-saml`: same groups and policies, populated by IdP group claims using the convention
  `lz:<domain>:<tenant>:<role>`; the federation object itself is **not** in the provider, so the
  adapter emits `pending_actions` (upload metadata, set group attribute) and the detective scanner
  (ADR-0006) checks the live `/me/identity/provider` matches. Entra, Okta, Keycloak, Google and AD FS
  are **how-to pages and example IdP configurations**, not code.
- `keystone-machine`: machine access to the OpenStack plane. **Default (after a spike): the OVH
  service-account bridge** — OVH documents that an IAM service account authenticates to OpenStack
  with `OS_AUTH_TYPE=v3oidcclientcredentials` and receives OpenStack rights through IAM policies
  (e.g. `publicCloudProject:openstack:infrastructureSupervisor`, "11 levels of rights"), so one
  identity spans both planes and offboarding is one action. **Fallback:** per-project Keystone
  application credentials with `expires_at` and access rules. The spike must prove the bridge works
  with the `openstack` Terraform provider, token refresh and revocation.
- `k8s-oidc`: cluster OIDC to the same IdP; K8s groups named identically to OVH groups so one
  offboarding action in the IdP removes all three planes.

**Identity ledger.** `identities.yaml` in the tenant repo lists every human group and workload and
which planes it exists in. Default: **humans exist only in OVH IAM**; a human Keystone user is allowed
only by an explicit ledger entry with an `expires` date (the VM-classic path may need Horizon/CLI
access). The scanner reconciles the ledger against the live account: an unknown principal is a
finding. The scanner also enumerates **contact-based delegations to other NIC handles**, a
separate access route that IAM group removal does not revoke (OVH delegation guide).

**Automation identities by authority** (ADR-0009): bootstrap/order, account-governance,
deployment per stage, observation, state, recovery. A pipeline may hold several; none holds
account-wide authority for convenience; a plan identity also needs lock permissions.

**Break-glass.** One sealed native user outside the deny-floor group (ADR-0006), plus time-boxed IAM
policies using `expired_at` for planned elevated work; both alert via the audit sink when used
(audit source: spike).

**Keycloak in CI.** A Keycloak container is the reference IdP for federation and K8s OIDC tests, so
the SAML and OIDC paths are tested without an enterprise tenant.

## Consequences
- Swapping the IdP changes one profile value and the IdP-side how-to; no HCL change.
- The role catalogue is a compatibility promise; adding a role touches every adapter and is a minor
  version of the identity family.
- The singleton federation and the missing resource are registered as provider gaps (ADR-0011).

## Counterpoints (kept even if overruled)
- Five roles will not fit every organisation; custom roles are a documented extension (a sixth role
  = a new action list + bindings), not a v1 feature.
- Banning humans from Keystone outright (one blind design's position) is cleaner for offboarding;
  rejected as too opinionated for `team-vm`. The ledger entry with expiry keeps it visible.

## Verification
- Spike: Keycloak SAML → OVH IAM group → policy; a federated user with group
  `lz:finance:payments:dev` can list only that tenant's project. Also answers whether a federated
  user reaches Horizon without a Keystone user.
- Spike: `ovh_cloud_project_kube_oidc` against Keycloak; `kubectl` with an OIDC token gets the mapped
  role.
- Spike: find the audit source for IAM logins and API activity consumable by Logs Data Platform.

## Review log
- 2026-10-01: service-account → OpenStack bridge (OVH guide, verified), delegation scan and the
  identity-authority catalogue adopted from the external blind design
  (agent-context/inbox/REVIEW-codex-gpt-6-blind-2026-10-01.md).
