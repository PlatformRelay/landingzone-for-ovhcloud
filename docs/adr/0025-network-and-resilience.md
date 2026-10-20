# ADR-0025: Network and resilience architecture
- Status: Accepted (2026-10-20, D128)
- Date: 2026-10-20
- Related: ADR-0004, ADR-0006, ADR-0009, ADR-0016, ADR-0017, ADR-0021, ADR-0022, ADR-0024;
  spec 005 R9, spec 006 E5

## Context
The design names a network family (`island | hub-vrack`) and a resilience dimension (`1az | 3az`)
in every profile ([ADR-0016](0016-golden-paths-as-profiles.md)), but no record says what an
environment's network is, how traffic leaves it, which failures the landing zone survives, or how
state comes back after a loss. The first slice built one network and one protected state bucket
without that record. The second-slice work that adds the `account-fabric` stage (vRack and audit
facilities, [ADR-0004](0004-staged-roots-and-output-contracts.md)) depends on it
([spec 006, postponed row E5](../../specs/006-harden-first-slice/spec.md#out-of-scope--postponed-with-trigger)).

**Decision drivers**
- Tenant isolation: tenants get separate networks, and a tenant credential must fail to reach
  another tenant's resources ([ADR-0021](0021-threat-model-authorisation-boundaries-and-supply-chain.md), *Tenant ↔ tenant*).
- Prevention by topology: "islands-first networking (no east-west path exists), no public gateway
  unless requested" is a guardrail plane ([ADR-0006](0006-guardrails-without-org-level-policy.md), *Preventive (architecture)*).
- Address plans come from a ledger, never from list position, and "controlled egress" is proven
  by traffic probes, not by a plan ([ADR-0017](0017-runtime-families-and-output-contract.md), *Network family specifics*).
- Recovery: replication alone is not recovery, promotion fences the old writer, and key loss needs
  a separately decryptable backup (constitution V; [ADR-0009](0009-state-backend-bootstrap-and-credentials.md)).
- Cost and scope: the cheapest useful slice first (constitution VI); a gateway is billed
  ([spec 005 research R9](../../specs/005-first-landing-zone-slice/research.md#r9-network)); a second
  region or project needs an operator decision ([ADR-0024](0024-cost-and-sandbox-operations.md)).

**OVHcloud facts this relies on** (docs mirror paths are under `docs.ovhcloud.com` guides):
- New Public Cloud projects come with a vRack, which is free
  (`public-cloud/network-services/vrack.mdx:196`, `:201`).
- Outbound Internet access from a private subnet goes through a Public Cloud Gateway (SNAT); the
  gateway is also needed for Floating IPs (`public-cloud/network-services/getting-started-gateway.mdx:12`).
- A 1-AZ region is one availability zone and stays exposed to a whole-data-centre failure; a 3-AZ
  region has three isolated zones (`public-cloud/cross-functional/deployment-modes-comparison-resilience-details.mdx:42`,
  `storage-and-backup/object-storage/s3-regions-comparison.mdx:30`, `:60`). Instances are zonal and
  must be spread by the customer; Object Storage is regional
  (`public-cloud/cross-functional/deployments-modes-reference-architecture.mdx:21`, `:26`).
- Native S3 asynchronous replication keeps the source's Object Lock configuration, but source and
  destination buckets must be in the **same** Public Cloud project
  (`storage-and-backup/object-storage/s3-asynchronous-replication.mdx:90`, `:251`).
- Private networks sharing a VLAN in one vRack share one address space, so overlapping ranges
  collide across regions (`public-cloud/network-services/extend-private-network-multi-regions.mdx:27`).
- Whether a given region, such as `GRA11`, is 1-AZ or 3-AZ: UNVERIFIED here; the catalogue
  (ADR-0022) records each region's class with its source.

## Options considered
| Option | Assessment |
| --- | --- |
| **A. Island per environment** — one private network per environment and region, in that environment's project; no gateway, no cross-environment path; egress and ingress are per-environment opt-ins | Recommended. Matches ADR-0006's topology guardrail and the one-project-per-environment tenancy (ADR-0005); costs nothing until a gateway is asked for; what the slice already builds. Cross-environment or on-premises connectivity needs option B. |
| **B. Shared hub vRack from day one** — transit routing through a hub with a firewall, as in OVHcloud's hub-and-spoke guide (`public-cloud/cross-functional/landing-zone-hub-spoke-cloud-architects.mdx:9-11`) | Kept as the `hub-vrack` variant behind the same output contract. As a default it adds a billed firewall, a shared routing owner and an east-west path that ADR-0006 wants absent, before any consumer needs it. |
| **C. Public ports only** — instances on public addresses, filtered by security groups | Rejected. Every instance is exposed separately (`public-cloud/network-services/concepts.mdx:69`), and OpenStack's default allow-all egress is not removed by allow rules (ADR-0017), so nothing is private by construction. |

## Decision
Option A, with the resilience rules below.

### Network per environment
- Each environment gets **one private network per region it uses**, in its own project, through
  `components/network/island`. ADR-0017's rule stands: ranges are validated before any network
  change. Deferring its `ipam.yaml` ledger is a **bounded amendment** to ADR-0017: until the
  trigger below fires, manifest validation (the checks listed below) is the temporary substitute
  for the one network. The separate `ipam.yaml` arrives with the second network that shares a
  vRack, because only then can ranges collide across rows.
- **No egress by default.** Outbound access is a per-environment opt-in that adds one gateway to
  that environment's network. "Controlled egress" also removes or neutralises OpenStack's default
  egress rule and is shown by IPv4 and IPv6 traffic probes (ADR-0017; patterns LZ-NET-002, LZ-NET-004).
- **No ingress by default.** Public entry is an explicit load balancer or Floating IP request,
  which needs the gateway above.
- `hub-vrack` stays a family variant with the same output contract. It ships when its trigger
  fires (below), and its routing owner is the `account-fabric` stage, never a tenant stack.

### Regions and availability
- One region per environment by default. A manifest may list several regions; each gets its own
  network and runtime stacks (ADR-0004), with non-overlapping ranges from the ledger. No
  active-active cross-region traffic is designed here.
- `1az` is the default resilience value. `3az` means: a 3-AZ region from the catalogue, and a
  runtime that spreads its zonal resources over the zones. The landing zone provides the region
  and the network; spreading workloads is the runtime's job (ADR-0017).

### Failure domains
| Failure | What carries the landing zone through it |
| --- | --- |
| A zone, in a 3-AZ region | Regional services (Object Storage, the network); zonal workloads only if the runtime spread them. |
| A region | Nothing automatic. Planned: the state backup (layer 2 below, not built) lives in another region; rebuild elsewhere by re-applying the stacks. Until it exists, region loss loses state. |
| A project (deleted, suspended or compromised) | `prevent_destroy` and `deletion_protection` on projects the landing zone manages or adopts (`modules/cloud-project/main.tf:8-32`). A referenced project is only read, so neither covers it; the sandbox project is referenced (KD-1). Planned: the state backup lives outside the state project (layer 2 below, not built). |
| The account | Only a backup copy held outside the account (layer 3 below, not built) plus the escrow package and the resource-id manifest (ADR-0009); the bootstrap re-runs on a fresh account ([how-to](../how-to/set-up-a-fresh-account.md)). Until layer 3 exists, account loss loses state. |
| A state writer killed mid-apply | The S3 lockfile, versioning and stale-lock recovery (ADR-0009). |

### State backup and restore
Three layers, from cheapest to strongest:
1. **In the bucket (in force):** versioning plus Object Lock in governance mode. During the
   retention period a state user cannot delete a locked version (ADR-0009, D104). The policy's
   version-delete deny is not what protects it: it was observed unenforced (spec 005 T010, row 11).
2. **Out of the project (to build):** the scheduled backup job of ADR-0009 copies each state
   snapshot, encrypted under the escrow method, to a bucket in **another region and another
   project**. Native replication cannot do this, because it stays inside one project (fact above);
   a same-project replica remains, at most, an optional extra, never the backup.
3. **Out of the account (to build):** a periodic copy of the layer-2 backup to storage the
   failed account cannot reach, such as a second OVHcloud account or another provider, with its
   own credentials. ADR-0009 already says independent decryption does not cover account
   unavailability; this layer is what covers it. With the escrow package, it is enough to rebuild
   the infrastructure and recover its state on a new account; it restores no workload data, which
   state snapshots do not hold. Where it lives is an owner decision.

Restore is a copy of an earlier version or a backup object into an **isolated** backend first,
validated, then promoted by an explicit, logged step that fences the old writer (ADR-0009).
This narrows ADR-0008's L10 drill, "DR restore of state from a replica", to a restore from the
layer-2 backup: a same-project replica is not the backup.
Proposed targets for the `solo` profile, still an open owner decision (below): lose at most one
day of state changes, and be able to plan again within one working day.

### What the first slice already does
- One private network and one subnet in exactly one region, DHCP on, `no_gateway = true`, no
  gateway resource (`modules/private-network/main.tf:9-25`); VLAN id required and distinct per
  project, 0..4000 (`modules/private-network/variables.tf:58-72`); IPv4 RFC 1918 ranges from /16
  to /29 only (`modules/private-network/variables.tf:37-56`).
- The network's region must be one of the project's regions
  (`components/network/island/variables.tf:28`); two networks of one environment with the same
  VLAN id are refused before any stack is generated (`DUPLICATE_VLAN`,
  `tools/internal/stacks/manifest.go:85-88`, `:499`).
- The sandbox manifest: one environment, one region (`GRA11`), `10.20.0.0/24`, VLAN 0
  (`stacks/deployments.yaml:33-35`); state in region `gra` (`stacks/deployments.yaml:19-23`).
- State buckets: versioning, Object Lock (governance) and `prevent_destroy`
  (`modules/object-storage-protected/main.tf:10-31`); state users' policy denies version
  deletion, the governance bypass and versioning or lock changes
  (`components/state-backend/main.tf:21-33`). That policy is intent: its version-delete deny was
  observed unenforced, and Object Lock carries that protection
  (`modules/object-storage-protected/main.tf:14-18`).
- Observed: a second writer refused and the state object encrypted (spec 005 T049 run 4); stale-lock
  recovery and restore-by-copy on a locked probe bucket (spec 003 T028), both summarised in
  [verification status](../reference/verification-status.md) and ADR-0009.
- Not done: no gateway, no egress probe, no IPv6, no `ipam.yaml`, no backup job, no second region,
  no `account-fabric` stack; state shares the tenant's project in the sandbox (KD-1).

### Postponed, with triggers
| Part | Trigger |
| --- | --- |
| Gateway, controlled egress and its IPv4/IPv6 traffic probes | The first runtime with compute that needs outbound access |
| `ipam.yaml` ledger and its validation | A second network in one vRack: a second region, environment or tenant network |
| `hub-vrack` variant and the `account-fabric` stage (inventory B10) | A consumer that needs cross-environment, cross-project or on-premises connectivity |
| `3az` profile value | The first profile that pins it (`regulated`, ADR-0016) or an owner request |
| Backup job to another region and project | A separate state project (lifts KD-1), or the first installation outside the sandbox |
| Out-of-account copy (layer 3) and where it lives | The backup job exists, and the owner chooses the destination |
| Restore and promotion drill (ADR-0008 L10) | The backup job exists |
| Recovery targets (time and data loss) | The owner's decision (open, below) |

### Open owner decisions
Accepting this ADR does not decide these; each stays with the owner:
- Recovery targets: RPO and RTO (the `solo` proposal above).
- Where layer 3 lives.
- The second region and project that hold the layer-2 backup.
- Whether `1az` stays the default resilience value.
- Paid gateways, and a hub (`hub-vrack`).

## Consequences
- The default landing zone has no path out and no path in. A workload that needs one says so in
  its environment's settings, and the cost of the gateway shows up in that environment.
- Cross-environment traffic needs the hub variant; teams wanting it early pay for that work first.
- A real backup needs a second project and a second region, so it waits for the separate state
  project. Until then, state survives operator error (versioning and lock) but not the loss of
  the project or the region.

## Counterpoints (kept even if overruled)
- Native replication needs no job of our own to run. It is rejected as *the* backup because it
  cannot leave the project and carries the same deletion authority; it may still serve as a fast
  same-region copy. Which of the two costs less is UNVERIFIED.
- Some readers expect a hub from the start, as in OVHcloud's own guide. Building it before a
  consumer exists would ship an untested routing owner and an east-west path the guardrails forbid.
- A `3az` default would survive a zone loss for regional services, but it limits the regions a
  tenant can choose, and the runtime must spread its zonal resources. Whether it also costs more
  for the first slice's resources is UNVERIFIED; `1az` keeps the simplest path first.

## Verification (how we will know / spikes)
- Offline today: the module's validation tests refuse bad ranges and VLAN ids (L1).
- When the gateway lands: an L7 probe shows no outbound path without it and an outbound path with
  it, for IPv4 and IPv6.
- When the backup job lands: an L10 drill restores state from the other region into an isolated
  backend and plans with no changes; the old writer is fenced before promotion.
- A spike reads the catalogue's region classes from OVHcloud's region list and records the source.

## Review log
- 2026-10-20: drafted; one independent review round, findings applied.
- 2026-10-20: accepted (D128) after two further independent reviews; their changes applied. The
  owner decisions above stay open.
