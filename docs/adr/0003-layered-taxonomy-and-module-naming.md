# ADR-0003: Layered taxonomy, naming and labelling
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0002, ADR-0005, ADR-0006, ADR-0010, ADR-0020

## Context
Azure Verified Modules classify `res` (single resource), `ptn` (pattern) and `utl` (utility) with numbered
requirements; OCI and Alibaba use modules → components → blueprints. The operator asked for
"terraform/tofu naming modules" and clarified (D2): **both** a flexible naming convention (the resource
short name may come first or last; organisations differ) and a naming module, **plus a labelling
convention from the start** that adapts to different organisation structures, is policy-checkable, and
always carries "managed by OpenTofu" and "managed in this repository" style tags, labels or
annotations. OVHcloud IAM conditions on `resource.Tag(<key>)` make tags part of the authorisation
model (ADR-0006), and the generated documentation (ADR-0020) and the scanner read them back, so the
labelling convention is infrastructure, not decoration. Tag support is uneven across OVH products
and tag key/value limits are UNVERIFIED.

## Options considered
- **Prefix classes in a flat directory** (`res-cloud-project`, `ptn-network-baseline`).
- **Layer directories** (`modules/`, `components/`, `stages/` + `profiles/`) — chosen in ADR-0002.
- **Naming module** as a pure-function module vs. a locals-only convention documented in prose.

## Decision
**Layers**
- `modules/` — provider-thin, one OVH product concern (`cloud-project`, `cloud-quota`, `iam-policy`,
  `private-network`, `kube-cluster`, `object-storage`, `logs-stream`, `naming`). May call providers;
  may call only `modules/naming` among modules.
- `components/<family>/<variant>/` — landing-zone features composed of modules. Families with
  variants behind one output contract: `runtime/{kube-managed,vm-openstack,managed-only,hybrid-vrack}`
  (ADR-0017), `identity/{ovh-native,ovh-saml,keystone-machine,k8s-oidc}` (ADR-0018),
  `network/{island,hub-vrack}`, `observability/{ldp,byo,none}`. Singletons: `account-baseline`,
  `project-factory`, `guardrails`, `state-backend`.
- `stages/` — the one composition graph: ordered roots with their own state (ADR-0004), driven by a
  profile (ADR-0016). "Blueprint" is no longer a layer.

**Names**
- Directory name = module name = lowercase kebab-case, product-noun-first, no provider prefix inside the
  repo (`private-network`, not `ovh-private-network`).
- Published/mirrored names follow the registry convention: `terraform-ovh-<name>` (ADR-0010).
- Resource labels inside modules: `this` for the single primary resource, otherwise a role noun;
  never `main`, never repeat the type.

**Interfaces** (every module, enforced by a linter script)
- Require explicit, typed identifiers and ownership metadata appropriate to the resource; expose
  `name` and `tags` only where the API carries them (an identity user has a login and one group, not a
  name and tags) and document where metadata is recorded when it cannot. Primitive modules accept
  existing names (import override); governing components apply the naming convention.
- Standard optional inputs where meaningful: `enabled` (OpenTofu 1.11 meta-argument; a module using it
  is OpenTofu-only and labelled so — a variable does not make the syntax Terraform-compatible),
  `iam_policies`, deletion protection (`prevent_destroy` plus the decommission gate of ADR-0005).
- Outputs: stable, documented, never whole resource objects.
- No telemetry variable (unlike AVM): this project collects none, by principle.

**Naming module** (`modules/naming`) — a flexible convention, one implementation
- Pure function, no providers or data sources: inputs are the hierarchy coordinates
  `(org, domain, tenant, environment, region, resource_kind, instance)` plus the organisation's
  **naming template**; outputs `names` (map by kind), `name_short` and `labels` (below). No
  generated URN prefix: a display name is not an IAM identifier; URN matching uses provider-returned
  ids per resource kind. Profiles are folded into labels, never names (names are immutable; profiles
  can change).
- **The template is per organisation, in data** (`naming.yaml` in the tenant repo, platform-controlled):
  `pattern` as an ordered list of segments (`[kind, org, tenant, env, region, instance]` or
  `[org, tenant, env, kind, instance]` — the resource short name may come first or last), the
  separator, the case rule, the **abbreviation table** (segment values → short forms, resource kinds →
  short names such as `pn`, `gw`, `k8s`), per-kind overrides (an S3 bucket cannot carry a separator
  the pattern uses), and a deterministic truncation strategy (keep the identifying segments, drop or
  hash the rest, never silently cut) when the resolved name exceeds the kind's limit. The module
  validates every resolved pattern against the per-kind limits at plan time; an impossible template
  fails before any resource is created. The generated docs page (ADR-0020) shows the organisation's
  resolved convention with examples.
- **`names.yaml` (repo-wide) is the single source for limits and keys**: per resource kind the max
  length, charset, allowed separators and the OVH doc URL that states the limit; the label key
  schema below. From it a generator emits (a) the table-driven `tofu test` runs for the module,
  including `expect_failures` cases for invalid input and templates, (b) the CEL fixtures for the
  assent policies (ADR-0005), (c) the Rego and tflint rule data, and (d) the docs page. CI fails if
  any generated artefact is stale, so the self-service gate, the plan policy and the module can never
  disagree about a conformant name or label. Independently authored golden vectors (not generated)
  test collision, truncation, template changes and upgrades.
- OVH per-resource name limits and tag constraints are _UNVERIFIED_ and are gathered in the spike;
  each row carries a source URL.

**Labelling convention** (tags on OVHcloud resources, labels and annotations on Kubernetes objects,
metadata on OpenStack resources, inventory records where an API carries none)
- **Mandatory keys on every labellable resource**, under a configurable namespace (default `lz`;
  Kubernetes form `lz.platformrelay.dev/<key>`):

  | Key | Value | Why |
  |---|---|---|
  | `managed-by` | `opentofu` | Humans and the scanner can tell managed resources from console-made ones; drift triage |
  | `managed-in` | `<forge>/<org>/<repo>//<path>` of the owning root template or extension | Points from a resource in the console to the code that owns it; one writer per object (ADR-0021) |
  | `instance` | the immutable deployment instance id (ADR-0004) | Links a resource to its state owner; feeds the generated state-owner map (ADR-0020) |
  | `release` | the release-train version that last applied it | Upgrade and drift analysis |

- **Organisation-structure keys** are a per-organisation schema (`labels.yaml`, platform-controlled):
  a list of keys with `required | optional`, allowed values or a pattern, and the hierarchy level
  they derive from (`domain`, `tenant`, `env`, `owner`, `cost-centre`, `data-classification`,
  `profile`, …). The default set mirrors the virtual hierarchy (ADR-0005); an organisation with
  business units instead of domains, or a regulator-mandated classification key, changes the schema,
  not the code. Values the IAM plane conditions on (`resource.Tag(lz:tenant)`, ADR-0006) are marked
  `authorisation: true` and may be set only by the platform's roots, never by a tenant file.
- **Policy-checkable at every plane**: the tenant schema and the assent policies accept only keys
  from the organisation schema (merge gate); the Rego plan policy requires the mandatory and
  required keys on every resource kind the applicability table marks as labellable (apply gate);
  the scanner reports resources with missing, unknown or foreign keys and console-made resources
  without `managed-by` (detective). Kubernetes objects created by the runtime carry the same keys as
  labels, with long values (`managed-in`, `release`) as annotations.
- **Applicability table** in `names.yaml`: per resource kind whether the API carries tags, labels,
  metadata or nothing; where it carries nothing, the metadata is recorded in the inventory the
  scanner and the docs renderer maintain, and the docs say so. No invented provider fields.
- Keys and values obey the OVH tag constraints once the spike establishes them; until then the
  module enforces a conservative pattern (lowercase, `[a-z0-9:_./-]`, bounded length).

**Spec**: a written module spec with numbered requirements (functional `LZFR`, non-functional `LZNFR`,
AVM-style) in `docs/reference/module-spec.md`, each one checkable by CI or marked manual.

## Consequences
- Naming is testable and centralised. **Naming-algorithm changes are migrations**: existing names
  stay stable by default, overrides support import, and independently authored golden vectors (not
  generated from `names.yaml`) test collision, truncation and upgrade behaviour, because a wrong limit in
  the data would pass every generated test.
- The layer rule gives a linter-enforceable dependency direction.

## Counterpoints
- A naming module can become a god-module. Mitigation: it only emits strings and maps, no resources.
- Directory names without a provider prefix collide in mirrored repos; the mirror applies the prefix.
- A configurable segment order weakens cross-organisation recognisability; accepted because the
  template is data, validated, and documented per installation.
- Mandatory labels on every resource cost plan noise where a product ignores them; the applicability
  table keeps the rule honest instead of universal.

## Verification
- Spike: confirm OVH naming limits (project description, IAM resource group names, S3 bucket rules, K8s
  cluster names) **and tag key/value constraints** (charset, length, count per resource, whether
  `resource.Tag()` conditions see tags set at creation and after mutation).
- Spike: two organisations with different templates and label schemas produce valid names and labels
  for every kind in the catalogue; the plan policy rejects a resource missing `managed-by`.

## Review log
- 2026-10-01: round-2 external adversarial review applied.
