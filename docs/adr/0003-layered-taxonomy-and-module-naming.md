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
- **Layer directories** (`modules/`, `components/`, `blueprints/`) — chosen in ADR-0002.
- **Naming module** as a pure-function module vs. a locals-only convention documented in prose.

## Decision (proposed)
**Layers**
- `modules/` — provider-thin, one OVH product concern (`cloud-project`, `iam-policy`, `private-network`,
  `kube-cluster`, `object-storage`, `logs-stream`, `naming`). May call providers; may not call other layers.
- `components/` — landing-zone features composed of modules (`project-baseline`, `network-hub`,
  `identity-baseline`, `observability-baseline`, `project-factory`).
- `blueprints/` — composed, deployable landing zones made of ordered stages (ADR-0004).

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
- Pure function: inputs `(org, workload, environment, region, instance, resource_kind)`, outputs
  `names`, `project_name`, `tags`.
- One convention, encoded once; every other module takes names from it or from the caller, never builds
  its own. Length and charset limits per OVH resource kind are data in the module, tested by unit tests.
- OVH project display names have limits — _UNVERIFIED, to be confirmed in a spike_.

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
_(empty)_
