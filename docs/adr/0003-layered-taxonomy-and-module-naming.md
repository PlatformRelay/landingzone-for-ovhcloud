# ADR-0003: Layered taxonomy and module naming
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0002, ADR-0010

## Context
Azure Verified Modules classify `res` (single resource), `ptn` (pattern) and `utl` (utility) with numbered
requirements; OCI and Alibaba use modules → components → blueprints. The operator asked for
"terraform/tofu naming modules" — ambiguous: a **module naming convention**, a **naming module** (like
Azure's `naming`, generating resource names and tags), or both. This ADR does both.

## Options considered
- **Prefix classes in a flat directory** (`res-cloud-project`, `ptn-network-baseline`).
- **Layer directories** (`modules/`, `components/`, `stages/` + `profiles/`) — chosen in ADR-0002.
- **Naming module** as a pure-function module vs. a locals-only convention documented in prose.

## Decision (proposed)
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
- Required: `name` (or `name_prefix`), `tags` (map).
- Standard optional inputs where meaningful: `enabled` (OpenTofu 1.11 `enabled` meta-argument, behind a
  variable for Terraform compatibility), `iam_policies`, `lock`-style protection (`prevent_destroy` wired
  to a variable where the OpenTofu version allows).
- Outputs: stable, documented, never whole resource objects.
- No telemetry variable (unlike AVM): this project collects none, by principle.

**Naming module** (`modules/naming`)
- Pure function, no providers or data sources: inputs are the hierarchy coordinates
  `(org, domain, tenant, environment, region, resource_kind, instance)`, outputs `names` (map by
  kind), `name_short`, `tags` (including `lz:domain`, `lz:tenant`, `lz:env`, `lz:owner`), and
  `urn_prefix` for IAM resource matching. Profiles are folded into tags, never names (names are
  immutable; profiles can change).
- **`names.yaml` is the single source**: per resource kind the pattern, max length, charset,
  separator and the OVH doc URL that states the limit. From it a generator emits (a) the table-driven
  `tofu test` runs for the module, including `expect_failures` cases for invalid input, (b) the CEL
  regex fixtures for the assent policies (ADR-0005), (c) the tflint rule data, and (d) the docs page.
  CI fails if any generated artefact is stale, so the self-service gate and the module can never
  disagree about a conformant name.
- OVH per-resource name limits are _UNVERIFIED_ and are gathered in the spike; each row carries a
  source URL.

**Spec**: a written module spec with numbered requirements (functional `LZFR`, non-functional `LZNFR`,
AVM-style) in `docs/reference/module-spec.md`, each one checkable by CI or marked manual.

## Consequences
- Naming is testable and centralised; rename = one-module change plus a release.
- The layer rule gives a linter-enforceable dependency direction.

## Counterpoints
- A naming module can become a god-module. Mitigation: it only emits strings and maps, no resources.
- Directory names without a provider prefix collide in mirrored repos; the mirror applies the prefix.

## Verification
- Spike: confirm OVH naming limits (project description, IAM resource group names, S3 bucket rules, K8s
  cluster names, tag key/value constraints).

## Review log
- 2026-10-01 revision: component families with variants; `stages/` replaces `blueprints/`;
  `names.yaml` dual source. Source: agent-context/research/BRAINSTORM-2026-10-01-round2.md.
