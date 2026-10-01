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
6. **Thin adapter, fat task**: duplicated business logic stays out of forge files; the forge-native
   security controls (protected environments, approvals, artefact integrity, concurrency groups,
   runner trust) stay explicit in them regardless of length — no line-count rule. Qualification uses
   real test repositories on both forges; `act` and `gitlab-ci-local` are a local convenience only.
7. **Two pipelines by construction** (ADR-0009): the identity stage has its own workflow, service
   account and review rule; tenant pipelines cannot change IAM.
8. The **tenant repo** (ADR-0005) gets its own thin pipeline from `templates/tenant-repo/`: schema
   validation → `assent run` (policy-driven auto-merge, same on both forges) → plan → policy →
   apply of the exact approved saved-plan artefact from the protected branch; a re-plan invalidates
   the approval (ADR-0006).
9. **Forks get no credentials, ever.** A plan executes provider code and external data sources,
    so read-only credentials do not make fork PR execution safe; no `pull_request_target` checkout
    of fork code; fork jobs cannot poison caches used by privileged runs. Live layers (ADR-0008 L5+)
    run only on maintainer branches.
10. TACO support is **consumer-side**: TACOs execute their **native run lifecycle**; each adapter must
   provide pre-plan input checks, a **post-plan policy gate on the current run's plan** (a pre-plan hook
   cannot see that run's plan), immutable approval-to-apply binding, per-state serialisation,
   credential isolation and evidence export. A TACO that lacks a mandatory hook is unsupported for
   unattended deployment. The monorepo's own tests never run inside a TACO. Each supported
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
- 2026-10-01 round-2 adversarial review: accepted — post-plan gate (pre-plan hooks cannot read the
  plan), saved-plan apply, native TACO lifecycles with mandatory hooks, no line-count rule, real forge
  fixtures for qualification. Vendor dates in the context are marked as unverified reasons.
