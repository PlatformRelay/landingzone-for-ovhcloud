# ADR-0007: Pipeline portability and instance orchestration with Terramate
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0004, ADR-0005, ADR-0008, ADR-0009, ADR-0021

## Context
Requirement: work with GitHub, GitLab and the common TACOs (Atlantis, Terrateam, Spacelift, env0,
Scalr, Digger/OpenTaco; HCP Terraform cannot run OpenTofu and is out of scope). An installation is
many deployment instances (ADR-0004): a root template per tenant × environment × region, each with its
own backend key, a dependency order by artefact edges, and CI that should run only changed instances
and their dependants. Generating all of that with a bespoke tool would be a second product before the
first vertical slice. Terramate CLI (MPL-2.0, v0.17.3 at 2026-09-08, optional SaaS not needed)
provides stacks as directories, code generation of backend and provider files from data, git-based
change detection, graph-ordered orchestration, and emits plain OpenTofu that any runner can execute;
it also orchestrates Terragrunt stacks if that is ever needed. Its outputs-sharing feature is
experimental and by default reads outputs with `terraform output -json`, but the retrieval command is
configurable. Operator decision (D12): default to Terramate, skip the custom generator, defer Terragrunt.

## Options considered
- Per-platform pipelines, each hand-written — drift guaranteed.
- A task contract plus a bespoke generator for instances and TACO configs — a second product.
- **Terramate at the deployment-instance layer, a task contract for the monorepo's own checks, plain
  OpenTofu everywhere else** (chosen).
- Terragrunt as the orchestrator — widest TACO support, but its dependency model reads state and its
  per-unit configuration is a second DSL; deferred until a real need appears.

## Decision
1. **Library layers stay plain OpenTofu.** Modules, components and root templates (`stages/`) carry
   no orchestrator dependency; library consumers and every TACO can use them as they are.
2. **Terramate owns the deployment-instance layer in the tenant repo.** `templates/tenant-repo/`
   ships a Terramate configuration: one stack directory per deployment instance, generated from
   `deployments.yaml` through globals and `generate_hcl` (backend, providers, the module call of the
   root template with the effective document as input, mandatory labels); `after`/`before` express the
   artefact edges of ADR-0004; `terramate list --changed` and `terramate run` give changed-instance
   selection and ordered execution. Generated files are committed and checked for freshness, so the
   repo always contains runnable vanilla OpenTofu.
3. **Outputs stay artefacts, not state reads.** Instances consume `outputs.json` artefacts
   (ADR-0004). If Terramate outputs sharing is used, its `sharing_backend.command` points at the
   artefact reader, never at `tofu output` on another instance's state. Until that is spiked, inputs
   are generated from the artefact store directly.
4. **The Taskfile is the contract for the monorepo** (`task lint|test|policy|docs|check|dod`): CI
   definitions contain no logic beyond checkout, tool setup (mise), credentials and `task <target>`;
   `pipelines/github/` and `pipelines/gitlab/` ship reusable workflows and includes, tested on real
   repositories on both forges (`act` and `gitlab-ci-local` are a local convenience only).
5. **Tenant-repo pipeline** (ADR-0005): schema validation → `assent run` (policy-driven auto-merge,
   same on both forges) → `terramate generate` freshness check → `terramate run` plan on changed
   instances → post-plan policy gate on each current plan → apply of the exact approved saved-plan
   artefact from the protected branch; a re-plan invalidates the approval (ADR-0006).
6. **TACOs run the generated instances.** Because each instance directory is plain OpenTofu, a TACO
   points at the instance directories; Terramate's generation and change detection run in CI before
   the TACO, or the TACO runs the generated directories directly. Each adapter must provide pre-plan
   input checks, a post-plan policy gate on the current run's plan, immutable approval-to-apply
   binding, per-state serialisation, credential isolation and evidence export; a TACO that lacks a
   mandatory hook is unsupported for unattended deployment. Support tiers: *Tier 1* (tested in CI)
   GitHub Actions, GitLab CI, Atlantis; *Tier 2* (smoke-tested) Digger/OpenTaco, Spacelift, Scalr,
   env0, Terrateam; *unsupported* HCP Terraform. `docs/reference/tacos.md` has one row per TACO with
   a `verified_on` date; rows older than six months are flagged.
7. **Forge-native security controls stay explicit** in the forge files (protected environments,
   approvals, artefact integrity, concurrency groups, runner trust); duplicated business logic stays
   out of them. No line-count rule.
8. **Two pipelines by construction** (ADR-0009): the identity instance has its own workflow, service
   account and review rule; tenant pipelines cannot change governance IAM.
9. **Forks get no credentials, ever.** A plan executes provider code and external data sources, so
   read-only credentials do not make fork execution safe; no `pull_request_target` checkout of fork
   code; fork jobs cannot poison caches used by privileged runs. Live layers (ADR-0008 L5+) run only
   on maintainer branches.
10. Credentials never live in pipeline definitions: short-lived tokens minted per run (ADR-0009);
    OIDC federation only if a spike finds OVHcloud supports it.

## Consequences
- One more pinned tool for every consumer (Terramate in `mise.toml`), in exchange for no bespoke
  generator and change detection that an agent and an adopter already know.
- The monorepo's own tests never run inside Terramate or a TACO; the dependency checker (ADR-0002)
  selects tests in the monorepo, Terramate selects instances in the tenant repo.
- Terragrunt is a documented future option; Terramate can orchestrate it if a consumer needs it.

## Counterpoints
- An orchestrator at the instance layer is still a second tool for every adopter; accepted because
  the alternative was our own.
- Terramate outputs sharing is experimental; mitigated by keeping artefacts as the contract and the
  retrieval command under our control.
- A TACO that cannot run `terramate generate` depends on committed generated files; the freshness
  check in CI is what keeps those honest.

## Verification
- Spike (with the state-ownership spike of ADR-0004): two tenants, two instances generated from
  `deployments.yaml` by Terramate; distinct backend keys; `terramate list --changed` selects only the
  touched instance and its dependants; ordered run respects the artefact edges; outputs flow through
  artefacts without any state read; the generated directories plan and apply under plain `tofu` and
  under Atlantis.
- Spike: OVHcloud OIDC/federated credentials for CI jobs; if absent, the lease model of ADR-0009 stands.
- Spike: Atlantis + OpenTofu + S3 backend on OVH Object Storage end to end.

## Review log
- 2026-10-01: round-2 external adversarial review applied.
