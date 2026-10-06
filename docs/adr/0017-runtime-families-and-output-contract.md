# ADR-0017: Runtime families and the output-only contract (multiple base tech stacks)
- Status: Accepted
- Date: 2026-10-06
- Related: ADR-0003, ADR-0004, ADR-0016, ADR-0018

## Context
The operator requires support for multiple base tech stacks. On OVHcloud these are: Managed
Kubernetes (`ovh_cloud_project_kube*`, OIDC via `ovh_cloud_project_kube_oidc` — verified in the
provider docs), plain OpenStack compute in a Public Cloud project (`openstack` provider), managed
services only (databases, Object Storage, Logs Data Platform, OKMS), and hybrid attachments to bare
metal or Hosted Private Cloud over vRack (`ovh_vrack_dedicated_server*`, `ovh_vrack_dedicated_cloud*`
exist). The trap to avoid is the lowest common denominator: a "compute" interface
that fits none of them.

## Options considered
- **A. Free-form outputs per runtime** — no contract; stages branch on runtime kind everywhere.
- **B. Common outputs, kind-specific inputs** — one output schema; inputs stay native to the kind.
- **C. Common inputs and outputs** — the LCD trap.

## Decision
Option B.

- `components/runtime/<kind>/` with kinds `kube-managed`, `vm-openstack`, `managed-only`,
  `hybrid-vrack` (v1: the first three tested; `hybrid-vrack` documented, partially automated).
- **Output contract = a small versioned envelope plus typed capability outputs**
  (`schemas/runtime-envelope.schema.json`, `schemas/capabilities/*.schema.json`). The envelope
  identifies runtime kind, deployment scope (instance, project, region), readiness and
  `pending_actions[]`. Capabilities (`network`, `ingress`, `kubernetes-api`, `workload-identity`,
  `log-stream`, …) are published only when they exist; **no placeholder values** — a managed-only
  runtime publishes no `network` capability rather than an empty gateway id. Every resource reference
  carries its API authority, project, region and identifier. **Consumers declare the capabilities they
  require**; an absent capability fails validation early with a precise diagnostic, never late at
  apply. Exact fields are designed against real consumers in the first vertical slice.
- A tenant environment may compose **several capabilities** (a VM runtime plus a managed database);
  "one runtime per environment" is a preset, not a rule.
- **Inputs are kind-specific**, validated by `oneOf` in the tenant schema (ADR-0005) under
  `runtime.<kind>:`; a runtime may expose rich native inputs (node pools, flavours, images).
- A runtime **consumes** identities from ADR-0018 and never creates IAM policies; it binds principals
  in its own plane (K8s RBAC from OIDC groups, Keystone application credentials for VM deployers).
- The `runtime` root template is instantiated once per deployment instance (ADR-0004), selecting the
  variant from the profile at one point (ADR-0016 rule 2).
- **Contract tests**: the **actual** runtime implementation runs under `mock_provider` and its real
  outputs are asserted against the envelope and capability schemas; `override_*` is used only for
  dependencies outside the implementation under test. Consumer tests prove each intended consumer
  works against the capabilities it needs and that an unsupported consumer fails early. An orphan
  runtime (not in any supported tuple) fails CI.
- **Network family specifics:** an **IPAM ledger** (`ipam.yaml` in the tenant repo: CIDRs, VLAN ids,
  routing ownership, reservations) is validated before any network change; ranges are never derived
  from a tenant's position in a list; IPv6 is covered or explicitly disabled. OpenStack's default
  allow-all egress is unmanaged by `ovh_cloud_security_group`, and explicit allow rules **do not
  remove it**; "controlled egress" therefore requires the default rule to be removed or separately
  neutralised (the `openstack` security-group resource with `delete_default_rules`, one writer per
  group) and is proven by IPv4 and IPv6 traffic probes (ADR-0008 L7), never by a plan read.
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
- Spike (ranked 11 by the review): implement `managed-only` and `kube-managed` for real under mocks;
  pass if each intended consumer works against the capabilities it needs, an unsupported consumer
  fails early with a precise diagnostic, no placeholder gateway or principal exists, and a VM plus
  managed-database composition is expressible.
- Spike: `ovh_cloud_project_kube_oidc` against a Keycloak realm (ADR-0018).

## Review log
- 2026-10-01: round-2 external adversarial review applied.
