# ADR-0002: Repository structure (monorepo)
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0003, ADR-0004, ADR-0007, ADR-0010

## Context
The operator prefers a monorepo. Terraform/OpenTofu registries expect one repository per module
(`terraform-<provider>-<name>`), so a monorepo needs an explicit distribution answer (ADR-0010).
Prior art: OCI Landing Zones and Alibaba LZA layer `modules` / `components` / tests; Google splits
example-foundation from its module collection; Azure splits catalogue (AVM), pattern, accelerator.
Spec Kit will be initialised in this repo (`.specify/`, `specs/`).

## Options considered
1. **Monorepo, layered top-level directories** (below).
2. **Multi-repo**, one repo per module plus a blueprint repo — registry-native, but one maintainer plus
   agents cannot keep N repos green; cross-module changes become N PRs.
3. **Monorepo with a split-mirror job** publishing per-module repos — keeps one source of truth, adds
   release infrastructure. Deferred to ADR-0010 as an *option*, not a layout concern.

## Decision (proposed)
Option 1. Top-level layout:

```
ovh-landing-zone-accelerator/
├── README.md  LICENSE  NOTICE  CONTRIBUTING.md  SECURITY.md  AGENTS.md
├── mise.toml  Taskfile.yml  .tflint.hcl  .trivyignore  .editorconfig
├── modules/<name>/          # layer 1: one OVH product concern per module (ADR-0003)
│   ├── main.tf variables.tf outputs.tf versions.tf
│   ├── README.md            # generated reference + hand-written intro
│   ├── CONTRACT.yaml        # inputs/outputs/semver surface (ADR-0010)
│   ├── examples/<case>/     # runnable; executed by CI (ADR-0008)
│   └── tests/*.tftest.hcl   # unit tests with mock_provider
├── components/<name>/       # layer 2: landing-zone features composed of modules
├── blueprints/<name>/       # layer 3: deployable landing zones
│   ├── blueprint.yaml       # manifest: inputs, outputs, API permissions, cost note, compliance claims
│   ├── stages/NN-<name>/    # one root module + own state per stage (ADR-0004)
│   └── examples/
├── starter/                 # copy-once repo template driven by inputs.yaml (ADR-0005)
├── schemas/                 # JSON Schema: factory input, blueprint.yaml, CONTRACT.yaml
├── policies/                # Rego policies + their tests; profiles/ (ADR-0006, ADR-0012)
├── pipelines/
│   ├── github/  gitlab/     # thin templates calling `task …` (ADR-0007)
│   └── tacos/<name>/        # generated TACO configs, optional
├── tools/                   # small CLIs: contract check, stacks generator, detective scanner
├── tests/{integration,e2e,snapshots,fixtures}/   # cross-cutting only; unit tests live with the module
├── docs/{tutorials,how-to,reference,explanation,adr}/   # Diataxis (ADR-0013)
├── .github/  .gitlab-ci.yml # both CI definitions live in-repo; neither is primary
└── .specify/  specs/        # Spec Kit
```

Rules:
- A module's directory name is its name; nothing in `modules/` depends on `components/` or `blueprints/`
  (dependencies point down only; CI enforces with a script).
- Unit tests, examples, contract and generated docs live **next to the module**; `tests/` holds only what
  spans modules.
- `knowledge-base/` (ADR-0014) is gitignored; never committed.
- Tool pins in `mise.toml` (OpenTofu, tflint, trivy, conftest, task, terraform-docs). Nix is not required;
  a `flake.nix` may be added later for contributors who prefer it.
- `tools/` is one Go module (static binaries ease CI on both GitHub and GitLab). _Open question: Go vs
  Python for tools; Go proposed because the workspace has Go practice and no runtime to install._

## Consequences
- One PR can change a module, its examples, tests and docs atomically.
- Registry-style consumption requires per-module tags and possibly a mirror (ADR-0010).
- The tree is deep; `docs/reference/repository-map.md` must be generated from it to stay true.

## Counterpoints
- A flat `modules/` with `res-`/`ptn-` prefixes (AVM style) is simpler to glob; layered directories make
  the dependency rule visible and enforceable instead. Chosen for enforceability.
- Putting unit tests beside modules bloats module directories when mirrored; the mirror job can exclude them.

## Verification
- Spike: a dummy repo with three modules proves per-module tags, `//modules/x?ref=` consumption in
  OpenTofu and a CI path filter (only changed modules run).

## Review log
_(empty)_
