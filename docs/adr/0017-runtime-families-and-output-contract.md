# ADR-0017: Runtime families and the output-only contract (multiple base tech stacks)
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0003, ADR-0004, ADR-0016, ADR-0018

## Context
The operator requires support for multiple base tech stacks. On OVHcloud these are: Managed
Kubernetes (`ovh_cloud_project_kube*`, OIDC via `ovh_cloud_project_kube_oidc` — verified in the
provider docs), plain OpenStack compute in a Public Cloud project (`openstack` provider), managed
services only (databases, Object Storage, Logs Data Platform, OKMS), and hybrid attachments to bare
metal or Hosted Private Cloud over vRack (`ovh_vrack_dedicated_server*`, `ovh_vrack_dedicated_cloud*`
exist). The trap named by every blind design is the lowest common denominator: a "compute" interface
that fits none of them.

## Options considered
- **A. Free-form outputs per runtime** — no contract; stages branch on runtime kind everywhere.
- **B. Common outputs, kind-specific inputs** — one output schema; inputs stay native to the kind.
- **C. Common inputs and outputs** — the LCD trap.

## Decision (proposed)
Option B.

- `components/runtime/<kind>/` with kinds `kube-managed`, `vm-openstack`, `managed-only`,
  `hybrid-vrack` (v1: the first three tested; `hybrid-vrack` documented, partially automated).
- **Output contract** `schemas/runtime-outputs.schema.json`, identical for every kind:
  `network { private_network_id, subnet_ids, gateway_id }`, `endpoints { kind, uri, credentials_ref }`,
  `workload_identity { plane, principal }`, `log_sink`, `pending_actions[]` (manual steps the kind
  cannot automate, e.g. vSphere-side work for `hybrid-vrack`).
- **Inputs are kind-specific**, validated by `oneOf` in the tenant schema (ADR-0005) under
  `runtime.<kind>:`; a runtime may expose rich native inputs (node pools, flavours, images).
- A runtime **consumes** identities from ADR-0018 and never creates IAM policies; it binds principals
  in its own plane (K8s RBAC from OIDC groups, Keystone application credentials for VM deployers).
- Stages select the runtime at one point (ADR-0016 rule 2); `stages/40-runtimes` instantiates one
  runtime per tenant environment with `for_each`.
- **Contract tests**: a shared `*-contract.tftest.hcl` is run against every kind with `override_*`
  blocks replacing providers; every output field is asserted with `can()`/`regex`. An orphan runtime
  (not referenced by any profile in the capability matrix) fails CI.
- **Network family specifics:** an **IPAM ledger** (`ipam.yaml` in the tenant repo: CIDRs, VLAN ids,
  routing ownership, reservations) is validated before any network change; ranges are never derived
  from a tenant's position in a list; IPv6 is covered or explicitly disabled. OpenStack's default
  allow-all egress is unmanaged by `ovh_cloud_security_group`, so "controlled egress" uses explicit
  rule sets (or the `openstack` resource with `delete_default_rules`) and is proven by a traffic
  probe (ADR-0008 L7), never by a plan read.
- Observability sinks and networks are families with the same rule (`components/network/{island,hub-vrack}`,
  `components/observability/{ldp,byo,none}`); this ADR's contract pattern applies to all families.

## Consequences
- Adding a stack = one directory passing the contract test and one matrix entry.
- Breaking an output field is a major version of the component (ADR-0010).
- Nothing abstracts the data plane itself: what runs inside the cluster or VM is the tenant's.

## Counterpoints (kept even if overruled)
- Contracts calcify early; `pending_actions` and `endpoints.kind` are the escape valves.
- Option A is less ceremony for a solo setup; rejected because the ceremony is what keeps profiles
  from forking (ADR-0016).

## Verification
- Spike: write the schema, implement `managed-only` and `kube-managed`; pass if `team-vm` compiles
  against both unchanged and the contract test passes for all three.
- Spike: `ovh_cloud_project_kube_oidc` against a Keycloak realm (ADR-0018).

## Review log
- 2026-10-01: IPAM ledger, IPv6 rule and the default-egress finding adopted from the external blind
  design review.
