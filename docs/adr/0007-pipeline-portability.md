# ADR-0007: Pipeline portability: task contract, generated TACO config
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0004, ADR-0008, ADR-0009

## Context
Requirement: work with GitHub, GitLab and the common TACOs. Landscape (research, 2026-10): Atlantis
(self-hosted, `atlantis.yaml`), Digger (core rebranded OpenTaco), Terrateam, Spacelift (native Terragrunt
GA 2026-03), env0, Scalr (strong OpenTofu parity), HCP Terraform (free tier ended 2026-03-31; not an
OpenTofu runner). Terraform "Stacks" is HCP-only — avoid for portability. Vendor comparison pages are
biased; claims need checking against official docs.

## Options considered
- **Per-platform pipelines, each hand-written** — drift guaranteed.
- **A task contract** (`task lint|test|plan|apply|docs|scan`) that every CI and TACO calls, with thin
  per-platform templates.
- **An orchestrator** (Terramate/Terragrunt) as the common layer — DRY, but a second tool for every user.

## Decision (proposed)
1. The **Taskfile is the contract** (`Taskfile.yml`; same targets for stages and modules). CI definitions
   contain no logic beyond checkout, tool setup (mise), credentials and `task <target>`.
2. `pipelines/github/` and `pipelines/gitlab/` ship reusable workflows/includes, themselves tested
   (workflow linting + a dry-run job on this repo).
3. TACO support via a **generator** `tools/` command: one `stacks.yaml` (stage order, directories,
   dependencies) → `atlantis.yaml`, Spacelift/Scalr/env0/Digger/Terrateam configs. Tiered support:
   *Tier 1* (tested in CI): GitHub Actions, GitLab CI, Atlantis. *Tier 2* (generated, smoke-tested):
   Digger/OpenTaco, Spacelift, Scalr, env0, Terrateam. *Unsupported*: HCP Terraform (documented why).
4. No orchestrator is required. The directory layout stays orchestrator-friendly; a Terramate or
   Terragrunt recipe lives in `docs/how-to/`. Revisit if stage count grows.
5. Credentials never in pipeline definitions: short-lived tokens minted per run ("lease, don't
   store", ADR-0009); OIDC/federation only if a spike finds OVH supports it.
6. **Thin adapter, fat task**: forge files stay under ~40 lines each; a conformance test runs the
   smoke task under `act` (GitHub) and `gitlab-ci-local` (GitLab) on a schedule (tools: spike).
7. **Two pipelines by construction** (ADR-0009): the identity stage has its own workflow, service
   account and review rule; tenant pipelines cannot change IAM.
8. The **tenant repo** (ADR-0005) gets its own thin pipeline from `templates/tenant-repo/`: schema
   validation → `assent run` (GitLab; GitHub: CODEOWNERS until the adapter) → plan → policy → apply
   from the protected branch, with the plan hash pinned between plan and apply.
9. TACO support is **consumer-side**: TACOs run plan/apply of a golden path with a pre-plan hook
   calling `task policy:check`; the monorepo's own tests never run inside a TACO. Each supported
   TACO has a row in `docs/reference/tacos.md` (OpenTofu binary? hooks? plan JSON export? OPA
   bundle?) with a `verified_on` date; rows older than six months are flagged by the freshness gate.

## Consequences
- Contributors can run everything locally with `task`.
- A TACO is a view over the repo, not a source of truth; swapping it costs a regeneration.

## Counterpoints
- Generated TACO configs lag TACO releases; Tier 2 is "best effort" by design.
- The Taskfile contract hides plan output formatting that TACOs render natively (PR comments); accept.

## Verification
- Spike: does OVHcloud accept OIDC/federated credentials for CI jobs (GitHub OIDC, GitLab ID tokens)?
  If not, document the weaker model and the rotation story.
- Spike: Atlantis + OpenTofu + S3 backend on OVH Object Storage works end to end.

## Review log
- 2026-10-01 revision: thin-adapter rule, two-pipeline rule, tenant-repo pipeline with assent,
  consumer-side TACO support with a conformance checklist. Source:
  agent-context/research/BRAINSTORM-2026-10-01-round2.md.
