# ADR-0002: Repository structure (monorepo)
- Status: Proposed (revised 2026-10-01 after brainstorm round 2)
- Date: 2026-10-01
- Related: ADR-0003, ADR-0004, ADR-0007, ADR-0010, ADR-0016

## Context
The operator prefers a monorepo. Registries expect one repository per module, so a monorepo needs an
explicit distribution answer (ADR-0010). Prior art layers modules / components / tests (OCI, Alibaba)
or splits catalogue / pattern / accelerator (Azure). Round 2 added: golden paths are **data** over
one composition graph (ADR-0016); runtime, identity, network and observability are **families with
variants** (ADR-0017, ADR-0018); tenants self-serve through a separate repo gated by `assent`
(ADR-0005). Spec Kit will be initialised here (`.specify/`, `specs/`).

## Options considered
1. **Monorepo, layered directories** (below).
2. **Multi-repo** — registry-native; one maintainer plus agents cannot keep N repos green.
3. **Monorepo plus a split-mirror job** — kept as an ADR-0010 option, not a layout concern.

## Decision (proposed)
Option 1.

```
ovh-landing-zone-accelerator/
├── README.md  LICENSE  NOTICE  CONTRIBUTING.md  SECURITY.md  AGENTS.md
├── mise.toml  Taskfile.yml  stacks.yaml  .tflint.hcl  .editorconfig
├── modules/<name>/                 # layer 1: provider-thin primitives (ADR-0003)
│   ├── main.tf variables.tf outputs.tf versions.tf README.md (authored guidance + generated interface section) CONTRACT.yaml
│   ├── examples/<case>/            # runnable; each is a test target (ADR-0008)
│   └── tests/*.tftest.hcl          # unit + contract, mock_provider / override_*
├── modules/naming/                 # pure function; names.yaml is the single source (ADR-0003)
├── components/<family>/<variant>/  # layer 2: runtime/{kube-managed,vm-openstack,managed-only,hybrid-vrack}
│                                   #   identity/{ovh-native,ovh-saml,keystone-machine,k8s-oidc}
│                                   #   network/{island,hub-vrack}  observability/{ldp,byo,none}
│                                   #   singletons: account-baseline/ project-factory/ guardrails/ state-backend/
├── stages/<root-template>/         # layer 3: reusable root templates; one instance = one state owner (ADR-0004)
├── profiles/<golden-path>.yaml     # golden paths = presets over the supported catalogue (ADR-0016)
├── catalog/                        # data: supported-combinations.yaml, resource-ownership.yaml, controls.yaml
├── schemas/                        # JSON Schema only: tenant, profile, deployments, outputs artefacts,
│                                   #   identity/runtime capability outputs, CONTRACT.yaml, names, guardrails
├── releases/<version>/manifest.yaml # immutable release-train closure and evidence (ADR-0022)
├── harness/                        # agent harness: artifact-kinds.yaml, checks.yaml, capabilities.yaml,
│                                   #   guides/, schemas/ (diagnostic, evidence, verdict), evals/ (ADR-0019)
├── policies/
│   ├── guardrails.yaml             # rule → enforcement-plane matrix (ADR-0006)
│   ├── plan/                       # Rego on plan JSON + tests
│   ├── assent/                     # .assent policies + fixtures for the tenant repo (ADR-0005)
│   └── mutants/                    # mutation harness for plan and assent policies (ADR-0008)
├── templates/tenant-repo/          # self-service repo skeleton: tenants/, owners.yaml, deployments.yaml,
│                                   #   identities.yaml, ipam.yaml, .assent/, CI include (ADR-0005)
├── examples/<profile>/             # one runnable example per golden path; tutorials include from here
├── tests/{contracts,security,live,migrations,recovery,snapshots,harness,fixtures}/
│                                   # cross-cutting only; live/ is a protected discovery root; unit tests live with the code
├── tools/                          # one Go module: lz-audit scanner, contract diff, stacks generator,
│                                   #   json→junit, reaper, budget guard, kb sync
├── pipelines/{github,gitlab}/      # thin adapters calling `task …` (ADR-0007)
├── pipelines/tacos/<name>/         # generated consumer configs + conformance checklist
├── docs/{tutorials,how-to,reference,explanation,adr}/   # Diataxis (ADR-0013)
├── kb/manifest.yaml                # committed index of cited OVH docs; kb/mirror/ is gitignored (ADR-0014)
├── .github/  .gitlab-ci.yml        # both CI definitions in-repo; neither is primary
└── .specify/  specs/               # Spec Kit
```

Rules:
- **Dependencies point down only**: `modules → modules/naming`; `components → modules`;
  `stages → components, schemas`; `profiles`, `catalog`, `templates`, `policies` are data; nothing
  imports `stages/`. A dependency checker parses HCL with a maintained parser, resolves local paths and
  package boundaries, rejects unclassified dependencies and computes **transitive** test selection;
  its own fixtures cover aliases, generated files, subdirectories and external sources.
- Generators for an artefact kind are added only after two real examples establish the shape;
  tools are added when the first vertical slice needs them, not before.
- Unit tests, examples, `CONTRACT.yaml` and generated docs live **next to the code**; `tests/` holds only
  what spans directories.
- `kb/mirror/` and any `knowledge-base/` directory are gitignored; never committed.
- Tool pins in `mise.toml` (OpenTofu ≥ 1.13, tflint, trivy, conftest, task, terraform-docs, assent).
- `tools/` is one Go module (static binaries on both forges). Go chosen; Python rejected for runtime
  install cost in CI.
- Spec Kit files live only in `.specify/` and `specs/`.

## Consequences
- One PR changes a component, its tests, examples and docs atomically.
- The tree is deep; `docs/reference/repository-map.md` is generated from it.
- "Blueprint" is no longer a directory; the word is replaced by "golden path" (profile) everywhere.

## Counterpoints (kept even if overruled)
- A flat `modules/` with AVM-style prefixes globs more easily; layered directories make the
  dependency rule visible and enforceable. Chosen for enforceability.
- One root per golden path (`blueprints/<name>/`) reads more directly than a profile-driven graph;
  rejected in ADR-0016 (fork risk), kept as the hedge there.
- Per-component unit tests bloat mirrored repos; the mirror job can exclude them.

## Verification
- Spike: dummy repo with three modules proves per-module tags, `//modules/x?ref=` consumption and a
  CI path filter that runs only changed directories and their dependants.

## Review log
- 2026-10-01 revision: `blueprints/` → `profiles/` + `stages/`; component families with variants;
  `templates/tenant-repo/`; `policies/{guardrails.yaml,plan,assent,mutants}`; `kb/manifest.yaml`.
  Source: agent-context/research/BRAINSTORM-2026-10-01-round2.md.
- 2026-10-01 round-2 adversarial review: accepted — stages as root templates, `catalog/`, `releases/`,
  `harness/`, security/migrations/recovery test dirs, a real HCL dependency checker with transitive
  selection, authored READMEs, generators only after two examples. Rejected: renaming `stages/` to
  `roots/` (same semantics, name kept).
