# Implementation Plan: First landing-zone slice
Branch: `005-first-landing-zone-slice` · Date: 2026-10-06 · Spec: [spec.md](spec.md) · Status: draft

## Summary
Real OpenTofu for six stacks (`account-admin`, `bootstrap`, `account-governance`, `project`,
`project-network`, `runtime`), built as modules → components → stages and instantiated as Terramate
stacks generated from `stacks/deployments.yaml`. Offline: L0/L1 per directory, L2-lite per stack, all
through `lz-offline`. Live: a host-only Go wrapper (`lz-live`) for bootstrap, plan/apply/destroy and one
apply→destroy chain, run by the owner. Decisions and evidence: [research.md](research.md).

## Technical context
- **Language/version**: OpenTofu 1.13.0, Terramate 0.17.3, Go 1.27.1, Task 3.53.1, TFLint 0.64.0
  (`mise.toml`); provider `ovh/ovh` 2.21.0 (filesystem mirror in `lz-offline`).
- **Host-only tool**: `ovhcloud` CLI 0.15.0 via `mise.live.toml` (research R19).
- **Storage**: OVH Object Storage (S3) for state and `outputs.json`; local encrypted state for
  `account-admin` and `bootstrap`; credentials and passphrase in `~/.config/ovh-lz/` (mode 600).
- **Testing**: `tofu test` + `mock_provider`; Go tests (`go -C tools test`) with fake binaries for the
  live lane and bootstrap; captured tool outputs for Terramate and `ovhcloud`.
- **Target**: maintainer workstation (Linux amd64); OVHcloud `ovh-eu`, region GRA11 (compute) / `gra`
  (S3), confirmed by the operator (D87). One existing sandbox project.
- **Constraints**: no secret in output or repo; offline targets need no network; live runs are
  owner-started; one run per tenant at a time.
- **Scale**: 1 tenant × 1 environment × 1 region in the sandbox; growth fixture 2 × 2 × 2 offline.

## Constitution check
| Principle | Status | Note |
| --- | --- | --- |
| I Evidence, UNVERIFIED | pass | Premises P1–P22 with probes (spec); live tasks gated on probes |
| II Traceability | pass after T001–T002 | per-task fields present; registry made spec-scoped (R18) |
| III Tests first; test the sensors | pass | test task precedes each implementation task; captured fixtures for Terramate/`ovhcloud`; live tests in `tests/live/` |
| III Mutation survivors block claimed coverage | pass (scoped) | mutation coverage is claimed only for FR-013's security guards (D85) |
| IV Instances are roots, one owner, artefacts not state | pass | one state per stack; `outputs.json`; no `terraform_remote_state` |
| IV Privileged execution from protected immutable candidates | **deviation** | owner runs live targets from the workstation; already a known deviation in AGENTS.md (ADR-0019) |
| V Fail closed; cleanup | pass | trap destroy, leftover check fails on listing error, missing record re-plans |
| V Cost guards (constitution 1.3.0) | pass | constitution 1.3.0 (D87) makes cost guards hygiene, not admission gates. Built: inventory of created ids outside state, bounded runtime, tested trap destroy and leftover check, approximate cost per live run; spend admission, leases and reaper are not built. The trap destroy and leftover check are load-bearing and are tested (T046–T047) before the first live chain |
| V Key-loss recovery | non-claim | sandbox state is disposable and the bootstrap re-runnable; no recovery claim until OKMS/escrow |
| VI Smallest useful slice; generators after two examples | pass | Terramate templates serve six real stacks; growth fixture is the second shape |
| VI AgentEx scope | deferred (D87) | constitution 1.3.0 explicitly defers the full ADR-0019 AgentEx scope from this slice; trigger: the second vertical slice, or agents repeatedly misreading check output. 001/T017–T019 are postponed with it |
| VII Planned vs implemented | pass | all commands planned with creators |
| VIII Decisions recorded | pass | operator decisions D83/D85/D87 with their counterpoints |

ADR alignment (D87): ADR-0004 lists the `account-admin` stage and lets `deployments.yaml` list or
derive (backend key, authority and edges derived; project ids outside the manifest); ADR-0008
"Sandbox safety" and ADR-0024 say cost guards are hygiene; ADR-0009's one state bucket per tenant
is followed as written. Deviations still recorded (operator direction overrides):
ADR-0002 (`stacks/` in this repo, not `templates/tenant-repo/`); ADR-0003 (limits table in
`modules/naming/kinds.yaml`, `release` label constant `unreleased`); ADR-0007 (reconciler reduced to
create + `UNSUPPORTED_CHANGE`); ADR-0009 (passphrase only; OKMS and escrow postponed). Consistent with the reworded ADR-0004: transaction postponed, outputs in
the `artifacts/` prefix of each instance's state bucket, producers first, changed consumers re-planned, one run per tenant.

## Project structure

### Documentation
```text
specs/005-first-landing-zone-slice/
├── spec.md  plan.md  research.md  data-model.md  quickstart.md
├── contracts/checks.md
├── tasks.md
└── evidence/T0NN.md          # per-task summary pointing at the PR
```

### Source (new paths only)
```text
terramate.tm.hcl                          # Terramate project root config
mise.live.toml                            # ovhcloud CLI pin, host-only (MISE_ENV=live)
modules/
├── naming/{main,variables,outputs,versions}.tf  kinds.yaml  README.md  tests/
├── object-storage/                       # ovh_cloud_project_storage
├── object-storage-user/                  # ovh_cloud_project_user + _s3_credential + _s3_policy (bucket-scoped)
├── iam-service-account/                  # ovh_me_api_oauth2_client
├── iam-policy/                           # ovh_iam_policy
├── identity-group/                       # ovh_me_identity_group
├── cloud-project/                        # ovh_cloud_project (adopt), ovh_iam_resource_tags, ovh_cloud_project_alerting (optional)
├── cloud-quota/                          # ovh_cloud_quota (optional)
└── private-network/                      # ovh_cloud_project_network_private + _subnet
components/
├── account-baseline/                     # admin service account + policy
├── state-backend/                        # account bucket, one bucket per tenant, platform S3 user
├── identity/ovh-native/                  # deployers, policies, group, tenant S3 user
├── project-factory/                      # adopt project, labels, alert, quota
├── network/island/                       # private network + subnet
└── runtime/managed-only/                 # bucket + runtime envelope
stages/{account-admin,bootstrap,account-governance,project,project-network,runtime}/
stacks/
├── deployments.yaml
├── _lz/{globals,generate,backend,providers}.tm.hcl
├── account/{account-admin,bootstrap,account-governance}/          # generated
└── tenants/demo/dev/{project, gra11/{project-network,runtime}}/   # generated
schemas/
├── deployments.schema.json
└── outputs/{envelope,account-admin,bootstrap,account-governance,project,project-network,runtime}.schema.json
tools/
├── cmd/lz-live/                          # bootstrap, plan, apply, destroy, chain, order
├── cmd/lz-stacks/                        # reconcile, check, order
├── internal/stacks/                      # manifest, reconcile, outputs, generate, selection
├── internal/live/                        # bootstrap, credentials, redaction, trap, inventory, leftovers
└── internal/probes/live/chain/           # L7 assertions (build tag live)
tests/
├── check/fixtures/{unit,purity}/         # runner and purity-rule fixtures
├── fixtures/{terramate,ovhcloud,outputs,manifests}/  # captured tool output, producer fixtures, growth manifest
└── live/{probes,chain}/                  # owner-only discovery root
harness/checks.yaml                       # spec-scoped requirement keys (T002)
Taskfile.yml                              # new targets, see tasks.md table
docs/how-to/run-the-first-slice.md
```

**Structure decision**: ADR-0002 layered tree, new directories created with their first artefact.
Nothing in `stacks/` is hand-written HCL except `deployments.yaml` and `_lz/*.tm.hcl`; a hand-written
`.tf` under `stacks/` fails the dependency checker (T005–T006). Live Go assertions live in the
tools module; `tests/live/` holds only probe roots and observation data.

## Phases and ordering
1. **Setup** (T001–T006): spec-scoped traceability, `test:unit`/`test:slice` runners, purity rules.
2. **Premise probes** (T007 offline captures, T008 probe roots — agents; T009 read-only and T010
   create→destroy — owner sessions). T007/T008 start at once; US3 waits for T007; T039 and T046
   wait for T009; live owner tasks wait for T010.
3. **US1** (T011–T016): naming, object storage modules, `state-backend`, `bootstrap` stage.
4. **US2** (T017–T032): output contracts, IAM, `account-admin`, governance, project, network, runtime.
5. **US3** (T033–T041): manifest, reconciler, generation, sandbox stacks, order/selection.
6. **US4** (T042–T045): bootstrap tool, owner bootstrap runs (current and fresh account).
7. **US5** (T046–T049): live lane, L7 assertions, owner chain run.
8. **Polish** (T050–T051): how-to, exit.

## Verification strategy
V001–V011 in `contracts/checks.md`. Every offline V-check runs through `lz-offline` and records
discovery counts. Live V009/V010 are owner sessions; their evidence is the PR description (command
output summary, run id, approximate cost) plus `evidence/T0NN.md`.

## Risks
| Risk | Effect | Mitigation |
| --- | --- | --- |
| Project import plans a replacement (P5) | an order for a new project | `prevent_destroy` fails the plan; plan-only probe first; fallback reference mode |
| Lockfile or encryption incompatible with OVH S3 (P1–P3) | no remote state | probe first; fallback: local encrypted state per stack in `~/.config/ovh-lz/state/` with a per-tenant flock (records deviation) |
| Provider docs ahead of pin (P20) | validate errors | L0 validate against the mirrored 2.21.0 provider catches it |
| Bucket name collision (P21) | bootstrap fails on a new account | `org` discriminator (`lz`, D87) in the manifest; bootstrap fails clearly on a taken name and names the override |
| Tenant allowlist too narrow (P9) | chain fails mid-apply | trap destroys; widen allowlist with evidence in a follow-up commit |
| Passphrase loss | all state unreadable | sandbox only; re-run bootstrap; documented non-claim |
| Live run from a candidate worktree | agent-authored code runs with credentials | live runs start only from the owner's main checkout on a reviewed commit (D87); `lz-live` refuses agent worktrees |

## Complexity tracking
| Addition | Why | Simpler alternative rejected |
| --- | --- | --- |
| Components layer in a PoC | keeps ADR-0002 matrix unwidened (R1) | stage → module edge needs an ADR change |
| `account-admin` stack | re-runnable bootstrap on a fresh account (2026-10-06 update) | out-of-repo one-off config is not reproducible |
| Go live wrapper | tested traps/redaction with mutation proof | shell is hard to test |
