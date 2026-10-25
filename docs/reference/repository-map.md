---
references:
  - path: stacks/deployments.yaml
    blob: a4e4c7b24494733e88b0fb9e4d3baa3fa8b2d132
  - path: tools/internal/checks/dependencies.go
    blob: 793040e8a2f7c2e9d569428fbfe1788c79683a13
  - path: tools/cmd/lz-check/docs.go
    blob: 1b263ef4a77db373ffac5ad3bffcabdd89f3cf6f
  - path: tools/internal/checks/offline.go
    blob: 761a61f38609c26f431c4ef80fcb35d85e3f067c
verified_against: {task: "3.53"}
last_verified: fb968189fcc70955232a704509663c595151aeeb
---

# Repository map

Every top-level entry and its main subdirectories, one line each. This map is written by hand;
ADR-0002 plans to generate it from the tree later. `task docs:check` holds it to the tree: every
path under a top-level name the offline entry admits must exist, and every such top-level
directory needs a row of its own; other paths (`.gitignore`, `.sonarcloud.properties`) are
counted as unjudged. Counts are as of the first slice
([spec 005](../../specs/005-first-landing-zone-slice/spec.md)). How the layers fit together:
[architecture overview](../explanation/architecture-overview.md). Terms: [glossary](glossary.md).

## The landing zone (what gets deployed)

| Path | What it holds |
| --- | --- |
| `modules/` | **11 modules**, layer 1: one OVHcloud concern each. |
| `modules/naming/` | The pure naming and labelling module every name and label comes from. |
| `modules/cloud-project/`, `modules/cloud-quota/` | The Public Cloud project (retained) and its quota guard. |
| `modules/iam-policy/`, `modules/iam-resource-tags/`, `modules/iam-service-account/`, `modules/identity-group/` | IAM: policies, tags on IAM resources, OAuth2 service accounts, groups. |
| `modules/object-storage/`, `modules/object-storage-protected/`, `modules/object-storage-user/` | Buckets: plain, protected against deletion (state buckets), and bucket-scoped S3 users. |
| `modules/private-network/` | A private network and its subnet. |
| `components/` | **5 components**, layer 2: features composed from modules. |
| `components/state-backend/` | A state bucket and its S3 users (used by `bootstrap` and `tenant-state`). |
| `components/identity/ovh-native/` | Deployer clients, policies and groups in OVHcloud IAM. |
| `components/project-factory/` | Reference or adopt a project, with quota and budget alert switches. |
| `components/network/island/` | One private network and subnet in one region, with no gateway. |
| `components/runtime/managed-only/` | The runtime placeholder: one object storage bucket, no compute. |
| `stages/` | **6 stages**, layer 3: `bootstrap`, `tenant-state`, `account-governance`, `project`, `project-network`, `runtime`. |
| `stacks/` | **6 stacks**, generated from `stacks/deployments.yaml` (one tenant, one environment, one region). |
| `stacks/account/` | Account-level stacks: `bootstrap`, `account-governance`, `tenant-state/<tenant>`. |
| `stacks/tenants/` | Tenant stacks: `<tenant>/<env>/project`, `<tenant>/<env>/<region>/{project-network,runtime}`. |
| `stacks/_lz/` | The Terramate generation templates (backend, provider, stage call, outputs, offline test). |
| `schemas/` | JSON schemas: the deployment manifest, the check report, and `outputs/` (one per stage plus the envelope). |
| `terramate.tm.hcl` | The Terramate project root. |

## How it is verified

| Path | What it holds |
| --- | --- |
| `tools/` | One Go module (`go.mod`) with the command-line tools and their libraries. |
| `tools/cmd/` | Seven commands: `lz-check` (checks), `lz-offline` (offline entry), `lz-stacks` (stack generation and order), `lz-live` (live lane), `lz-pack` (runtime packing), `lz-release` (release-train manifest), `json2junit` (report conversion). |
| `tools/internal/` | Their libraries: `checks`, `stacks`, `live`, `probes`, `bundle`, `report`, `planjson`, `release`. |
| `tools/vendor/` | Vendored Go dependencies, so builds need no network. |
| `tools/scripts/` | Host scripts: `fetch-offline-entry.sh` fetches, verifies and installs the offline entry. |
| `tools/harness/` | The check registry (`checks.yaml`), what the offline entry may do (`capabilities.yaml`), and the cognitive-complexity baseline that may only shrink (`complexity-baseline.txt`). |
| `tests/` | Test data outside the Go packages: check fixtures, captured tool output, live probes and chain results. |
| `tests/check/` | Fixtures for `lz-check`'s own tests. |
| `tests/fixtures/` | Manifests, output envelopes, captured `tofu`, Terramate and `ovhcloud` output, probe captures. |
| `tests/live/probes/` | Live probe roots, one per premise group; owner sessions only. |
| `tests/live/chain/` | Expected and observed live-chain results that the chain's assertions are tested against. |
| `pipelines/` | The CI source and the record of CI runs. |
| `pipelines/github/` | The CI source the GitHub workflow is rendered from, and recorded CI runs. |
| `.github/` | GitHub configuration: the CI workflow, Renovate rules, contribution and security policies. |
| `.github/workflows/foundation.yml` | The one CI workflow; its jobs' `LZ_TARGETS` lists are what CI runs, and its `foundation` job passes only when they all did. |
| `.github/CONTRIBUTING.md`, `.github/SECURITY.md` | How to contribute (checks, commits, merging); how to report a vulnerability. |
| `Taskfile.yml` | Every check and operation as `task <target>`; `task --list` shows them. |
| `mise.toml`, `mise.live.toml` | Pinned tool versions; the second adds host-only live tools. |
| `.tflint.hcl`, `.sonarcloud.properties` | Linter and code-analysis configuration. |
| `.gitignore` | Keeps local files out of Git: `.local/`, state and plan files, credentials and secrets, local copies of reference material. |

## Why it is the way it is, and the delivery record

| Path | What it holds |
| --- | --- |
| `docs/` | Design and user documentation; the [docs index](../README.md) lists every page. |
| `docs/adr/` | Architecture decision records. |
| `docs/explanation/`, `docs/how-to/`, `docs/reference/` | Explanations, task guides and look-up pages. |
| `docs/known-issues/` | Verified findings and accepted limitations (KI-nnn). |
| `specs/` | The delivery record: one folder per feature with spec, plan, tasks and evidence. Not needed to use the landing zone. |
| `.specify/` | The Spec Kit templates, scripts and project constitution behind `specs/`. |
| `README.md`, `AGENTS.md` | The front page; working rules for contributors, human or automated. |
| `LICENSE`, `NOTICE`, `docs/LICENSE` | Apache-2.0 for code, CC BY 4.0 for `docs/`, third-party notices. |

Not in the repository: `.local/` (built binaries and live run records) and the maintainer's
credentials and per-account files, which live outside the clone.
