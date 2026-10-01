# OVHcloud public-cloud-examples: reuse and value map

Static inspection on 2026-10-01 of `ovh/public-cloud-examples` at
[`0ab581a39afbe61a2daa55b39238530f38665cc2`](https://github.com/ovh/public-cloud-examples/tree/0ab581a39afbe61a2daa55b39238530f38665cc2).
The local clone lives under `references/public-cloud-examples/` in the main checkout; `references/`
is gitignored. No upstream scripts, plans, installs or cloud operations were executed. Links below
are immutable source references. This map is a design input, not deployment qualification.

## What already exists

The [landing-zone tree](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/README.md)
contains deployable OpenTofu network patterns: multi-vRack IPsec/VTI with OPNsense HA, mono-vRack
LAN transit, and an HA firewall in an existing Public Cloud project. It includes firewall,
spoke-network and storage modules, separate hub/spoke deployments, IAM examples and a fictional
company with a hub and four spokes. The [repository warning](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/README.md#L11)
labels the code demonstration-only. Our former claim that OVHcloud publishes no accelerator was
too broad and has been corrected in ADR-0001.

At this snapshot, tracked-path inspection found no landing-zone `.tftest.hcl` files or test suite
and no CI workflow in `.github/` or root GitLab configuration. The
[pre-commit configuration](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/.pre-commit-config.yaml)
does configure format/TFLint and basic hygiene; contributor instructions ask for tests. These
observations do not mean all upstream examples lack tests or that lint was run successfully.

## What to take and how to qualify it

All checks in this table are **required before future adaptation**, not checks run during this
documentation comparison. Implementation enters a feature spec with creators, positive/negative
controls and private evidence under the constitution. No upstream implementation code is copied
by this change.

| Source at the pinned revision | Recommendation and local use | Minimum verification before adaptation |
| --- | --- | --- |
| [Existing-project HA deployment](https://github.com/ovh/public-cloud-examples/tree/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/deployments/opnsense-ha-existing-project) | First adoption reference for ADR-0005/0016; inspect existing objects and provider assumptions before choosing a topology | Import/no-replacement plan, private/public exposure checks, actual create/update/destroy and interrupted cleanup in protected sandbox |
| [Spoke network module](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/modules/network/spoke-one-vrack/network.tf#L5-L60) | Candidate resource substrate, after interface/security adaptation; ADR-0003/0017. Reuse the shared-transit/no-DHCP lesson | Reject duplicate VLAN/CIDR/fixed IP, verify routes/DHCP/isolation and port-security exceptions, second apply has no unintended changes |
| [HA firewall module](https://github.com/ovh/public-cloud-examples/tree/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/modules/firewall/opnsense-ha) | Reference for OPNsense templates, HA placement and peering; no default promise of HA from two VMs alone | Image integrity/provenance, TLS identity, secret-safe API calls, failover/reboot/route readiness, partial peering failure and teardown |
| [Lifecycle guide](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/deployments/multi-vrack-ipsec/docs/04-lifecycle-and-operations.md#L3-L32) | Turn manual spoke/hub prerequisites and post-destroy route checks into explicit task gates | Dependency order, stale route/peering detection, bounded retry of eventual consistency, cleanup failure remains visible |
| [OrbitalEdge IAM policies](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/examples/orbital-edge/tofu/spoke-constellation-dev/iam.tf#L4-L35) | Reuse resource/identity URN and prerequisite patterns as ADR-0018 probe inputs; account identities already need to exist | Real allow/deny across two projects, absent identity, offboard with already-issued tokens; explicit scope rather than copying broad action wildcards |
| [Local encryption/provider configuration](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/examples/orbital-edge/tofu/hub/providers.tf#L1-L46) and [remote backend example](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/examples/orbital-edge/tofu/README.md#L128-L148) | ADR-0009/0011 mechanism references, including ephemeral provider inputs; retain our separate recovery and lock probes | Exact pinned tool/backend behavior, encrypted state **and saved plans**, contention, writer fencing, separate-key recovery and scoped credentials; ephemeral input is not a JIT issuer |
| [Object-storage backend walkthrough](https://github.com/ovh/public-cloud-examples/tree/0ab581a39afbe61a2daa55b39238530f38665cc2/use-cases/create-and-use-object-storage-as-tf-backend) and [storage module](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/modules/storage/main.tf) | Bucket/endpoint/replication input to ADR-0009, not a completed recovery design | Least-privilege bucket/key policy, replicas/restores/retention/deletion, provider semantics and cleanup exposure; review the module's `s3:*` policy |
| [Scalar spoke identity and network map](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/modules/network/spoke-one-vrack/variables.tf#L34-L45) | Evidence for a scalar resource context with ordinary `for_each`; feed the pending ADR-0003 comparison, not a decided naming API | Same-kind distinct identities, exact imported name, two org orderings, recipe upgrade, scoped collisions and target-specific labels (001 V004–V006) |
| [Region/AZ defaults](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/examples/orbital-edge/tofu/hub/locals.tf#L10-L37) | Reference data for explained region defaults, with version/source provenance | Current region/AZ coverage, unsupported region refusal and replacement impact of region edits; no stale table presented as live availability |
| [OrbitalEdge context-to-operations story](https://github.com/ovh/public-cloud-examples/tree/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/examples/orbital-edge/docs) | Reuse the learning progression in ADR-0013/0023; create our own tested user journey and explain choices | Human walkthrough plus 004 V001–V005; runnable adapted examples retain behavioral tests, prose review is exempt |

## What to avoid inheriting unchanged

The [security guide](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/docs/05-security-and-secrets.md)
acknowledges disabled TLS verification, API secrets exposed by `local-exec` arguments and manual
rotation. Those are cautions, not defaults to reuse. The
[operations guide](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/deployments/multi-vrack-ipsec/docs/04-lifecycle-and-operations.md#L25-L28)
also correctly warns that state can contain credentials. Sensitive/ephemeral inputs alone do not
prove secret-free outputs, plans, state, process arguments or logs. Manual copy/edit of component
trees and shell secrets needs adaptation to our authority, provenance and recovery contracts.

Do not make every tenant pay for an OPNsense hub: select connectivity from a concrete use case.
Do not claim multi-vRack IPsec and mono-vRack transit are interchangeable isolation boundaries.
Qualify each topology separately. The upstream [duplication discussion](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone/examples/orbital-edge/tofu/README.md#L86-L124)
is a useful reason to compare composition, rather than copy every deployment tree.

## Does this project still add value?

**Yes, if it delivers the operating workflows and qualified behavior it proposes.** The network
recipes, IAM snippets, state encryption, backend guidance and worked stories already exist.
Another collection of similar templates alone would provide little additional value.

| Proposed addition | Difference from the inspected example | Proof owed |
| --- | --- | --- |
| Reproducible reference baseline | Explicit supported tuples, honest gaps, tests and retained evidence across pillars | 001–003 and the later adoption slice; lint alone is insufficient |
| Tenant request/auto-merge workflow | Bound policy decisions, protected execution, races and recovery | 002 rehearsals plus actual forge/authority qualification |
| JIT credentials and recovery | Issuer-bound per-run grant/token lifecycle, scoped backend access and independent recovery | 003 issuer/revocation/recovery probes; ordinary ephemeral inputs do not qualify it |
| Generated installation docs | Desired/observed maps, ownership/control coverage and private publication | ADR-0020 renderer, drift/staleness, redaction and publication tests in its future consumer spec |
| Flexible naming/labelling | Existing-name preservation, frozen recipes, strict inputs and per-target metadata | 001 V004–V006 and the pending joint interface choice |
| Resumable terminal preconfiguration | Explained use-case defaults, conditional progress, read-only checks and draft export | 004 V001–V005 plus independent human tone review |

All additions remain planned. This is a reason to experiment, not evidence that our project is
already safer or easier. Keep a separate accelerator for the cross-cutting tenant, authority,
evidence and user workflows; use upstream as selective substrate. Generally useful provider fixes,
tests and guides are good upstream contributions. If the extra workflows prove burdensome or
converge upstream, contribution there and an integration layer remain useful outcomes.

## Source provenance and redistribution

Upstream is [Apache-2.0](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/LICENSE#L89-L121).
Before copying code, retain applicable notices, ship the license, mark changed files prominently,
record source revision/path and check the destination's licensing decision (ADR-0015). No `NOTICE`
file was found at this snapshot; inspect again for each later reuse. Upstream
[contribution instructions](https://github.com/ovh/public-cloud-examples/blob/0ab581a39afbe61a2daa55b39238530f38665cc2/CONTRIBUTING.md)
also specify headers and DCO sign-off for contributions. Keep provider/image/OS dependencies and
their licenses/provenance separate from the example repository's license.
