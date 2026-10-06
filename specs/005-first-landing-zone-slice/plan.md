# Implementation Plan: First landing-zone slice
Branch: `005-first-landing-zone-slice` · Date: 2026-10-06 · Revised: 2026-10-06 (D88) · Spec: [spec.md](spec.md) · Status: draft

## Summary
Real OpenTofu for six stacks (`bootstrap`, `tenant-state`, `account-governance`, `project`,
`project-network`, `runtime`), built as modules → components → stages and instantiated as Terramate
stacks generated from `stacks/deployments.yaml`. The admin service account is created by the
bootstrap script through the API, outside OpenTofu (D88). Offline: L0/L1 per directory, L2-lite per
stack and an output-exchange integration, all through `lz-offline`. Live: a host-only Go wrapper
(`lz-live`) with a tested run core (host guard, child environment, account binding, deadline,
inventory, destroy-on-exit, leftover check) that runs the probes, the bootstrap, plan/apply/destroy
and one apply→destroy chain, started by the owner. Decisions and evidence: [research.md](research.md).

## Technical context
- **Language/version**: OpenTofu 1.13.0, Terramate 0.17.3, Go 1.27.1, Task 3.53.1, TFLint 0.64.0
  (`mise.toml`); provider `ovh/ovh` 2.21.0 (filesystem mirror in `lz-offline`).
- **Host-only tool**: `ovhcloud` CLI 0.15.0 via `mise.live.toml` (research R19).
- **Storage**: OVH Object Storage (S3) for state and `outputs.json`, all state buckets in
  `spec.state.project` (the one sandbox project, KD-1); local encrypted state for `bootstrap` only;
  credentials and passphrase in `~/.config/ovh-lz/` bound per account (mode 600).
- **Testing**: `tofu test` + `mock_provider`; Go tests (`go -C tools test`) with fake `tofu`,
  `ovhcloud`, `git` binaries, a fake OVH API and a fake terminal for the live lane and bootstrap;
  captured tool outputs for Terramate and `ovhcloud`.
- **Target**: maintainer workstation (Linux amd64); OVHcloud `ovh-eu`, region GRA11 (compute) / `gra`
  (S3), confirmed by the operator (D87). One existing sandbox project.
- **Constraints**: no secret in output, argv, generated files or repo; offline targets need no
  network; live runs are owner-started from the reviewed main checkout; one run per tenant and one
  per account scope at a time.
- **Scale**: 1 tenant × 1 environment × 1 region in the sandbox; growth fixture 2 × 2 × 2 (two runtime
  slots) and a tenant-only fixture offline.

## Constitution check
| Principle | Status | Note |
| --- | --- | --- |
| I Evidence, UNVERIFIED | pass | Premises P1–P25 with probes (spec); live tasks depend on probe results; offline implementation ahead of a premise runs only under a recorded waiver naming the premise and the revision a refutation forces (research R23: T025–T028 for P5, T037/T038 for P1–P4/P6, T054/T055 for P24) |
| II Traceability | pass after T001–T002 | per-task fields present; registry made spec-scoped (R18) |
| III Tests first; test the sensors | pass | test task precedes each implementation task; captured fixtures for Terramate/`ovhcloud`; live tests in `tests/live/` |
| III Mutation survivors block claimed coverage | pass (scoped) | mutation coverage is claimed only for FR-013's security guards G1–G15 (D85) |
| IV Instances are roots, one owner, artefacts not state | pass | one state per stack; `outputs.json` through one adapter; no `terraform_remote_state` |
| IV Privileged execution from protected immutable candidates | **deviation** (KD-2) | owner runs live targets from the workstation; already a known deviation in AGENTS.md (ADR-0019); narrowed by the host guard (main checkout via git metadata, clean tree, reviewed SHA on `origin/main`) |
| IV Tenant isolation | **deviation** (KD-1, D88) | state buckets share the sandbox project with tenant resources; tenant authority reaches them through the management API; recorded in spec and ADR-0009, demonstrated by V010, not claimed |
| V Fail closed; cleanup | pass | run core with deadline, incremental inventory, trap destroy and a leftover check over the full kind matrix, tested (T052–T055) before the first credential load or live probe; retained infrastructure protected on every verb |
| V Cost guards (constitution 1.3.0) | pass | constitution 1.3.0 (D87) makes cost guards hygiene, not admission gates. Built: inventory of created ids outside state, bounded runtime, tested trap destroy and leftover check, approximate cost per live run; spend admission, leases and reaper are not built |
| V Key-loss recovery | non-claim | sandbox state is disposable and the bootstrap re-runnable; no recovery claim until OKMS/escrow |
| VI Smallest useful slice; generators after two examples | pass | Terramate templates serve six real stacks; growth and tenant-only fixtures are the second shapes |
| VI AgentEx scope | deferred (D87) | constitution 1.3.0 explicitly defers the full ADR-0019 AgentEx scope from this slice; trigger: the second vertical slice, or agents repeatedly misreading check output. 001/T017–T019 are postponed with it |
| VII Planned vs implemented | pass | all commands planned with creators |
| VIII Decisions recorded | pass | operator decisions D83/D85/D87/D88 with their counterpoints |

ADR alignment (D87, D88): ADR-0004 lists the stage set with `tenant-state` and the reserved
`account-admin` name (the admin authority is created by the bootstrap script) and lets
`deployments.yaml` list or derive (backend key, authority and edges derived; project ids outside the
manifest); ADR-0008 "Sandbox safety" and ADR-0024 say cost guards are hygiene; ADR-0009's one state
bucket per tenant is followed as written, and its 2026-10-06 note records KD-1. Deviations still
recorded (operator direction overrides): ADR-0002 (`stacks/` in this repo, not
`templates/tenant-repo/`); ADR-0003 (limits table in `modules/naming/kinds.yaml`, `release` label
constant `unreleased`); ADR-0007 (reconciler reduced to create + `UNSUPPORTED_CHANGE`); ADR-0009
(passphrase only; OKMS and escrow postponed; KD-1; the admin client is not in OpenTofu state, so its
drift is found by the bootstrap's `admin` phase). Consistent with the reworded ADR-0004: transaction
postponed, outputs in the `artifacts/` prefix of each instance's state bucket, producers first,
changed consumers re-planned, one run per tenant.

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
├── object-storage/                       # ovh_cloud_project_storage (runtime)
├── object-storage-protected/             # same, literal prevent_destroy, versioning on (state buckets)
├── object-storage-user/                  # ovh_cloud_project_user + _s3_credential + _s3_policy (bucket-scoped)
├── iam-service-account/                  # ovh_me_api_oauth2_client
├── iam-policy/                           # ovh_iam_policy
├── identity-group/                       # ovh_me_identity_group
├── cloud-project/                        # ovh_cloud_project (adopt), ovh_iam_resource_tags, ovh_cloud_project_alerting (optional)
├── cloud-quota/                          # ovh_cloud_quota (optional)
└── private-network/                      # ovh_cloud_project_network_private + _subnet
components/
├── state-backend/                        # one protected bucket + bucket-scoped S3 users (bootstrap, tenant-state)
├── identity/ovh-native/                  # deployers, policies, group
├── project-factory/                      # adopt project, labels, alert, quota
├── network/island/                       # private network + subnet
└── runtime/managed-only/                 # bucket + runtime envelope
stages/{bootstrap,tenant-state,account-governance,project,project-network,runtime}/
stacks/
├── deployments.yaml
├── _lz/{globals,generate,backend,providers}.tm.hcl
├── account/{bootstrap,account-governance,tenant-state/demo}/      # generated
└── tenants/demo/dev/{project, gra11/{project-network,runtime}}/   # generated
schemas/
├── deployments.schema.json
└── outputs/{envelope,bootstrap,tenant-state,account-governance,project,project-network,runtime}.schema.json
tools/
├── cmd/lz-live/                          # bootstrap, probe, plan, apply, destroy, chain
├── cmd/lz-stacks/                        # reconcile, check, order
├── internal/stacks/                      # manifest, stages, reconcile, outputs, adapter, generate, selection, lock
├── internal/live/                        # guard, env, binding, files, runner, inventory, deadline, leftovers,
│                                         # bootstrap, rootkeys, ovhapi, redact, credentials, apply, protect,
│                                         # publish, chain, observe
└── internal/probes/live/chain/           # L7 assertions (build tag live)
tests/
├── check/fixtures/{unit,dependencies}/   # runner and purity-rule fixtures
├── fixtures/{terramate,tofu-probes,ovhcloud,outputs,manifests}/  # captured tool output, producer fixtures, growth/tenant-only manifests
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
2. **Live safety core** (T052–T055): host guard, child environment, account binding, run core
   (deadline, inventory, destroy-on-exit, leftover matrix), `lz-live probe`.
3. **Premise probes** (T007 offline captures, T008 probe roots — agents; T009 read-only and T010
   create→destroy — owner sessions through `lz-live probe`). T007/T008/T052 start at once; T009 waits
   for T055; US3 waits for T007; T038/T039 wait for T010's result, T039 and T046 for T009's.
4. **US1** (T011–T016): naming, object storage modules (plain and protected), `state-backend`,
   `bootstrap` stage.
5. **US2** (T017–T032): output contracts, IAM, `tenant-state`, governance, project, network, runtime.
6. **US3** (T033–T041, T060–T061): manifest, reconciler, generation, sandbox stacks, output exchange,
   order/selection/locks.
7. **US4** (T042–T043, T056–T057, T044–T045): bootstrap tool in two pairs, owner bootstrap runs
   (current and fresh account).
8. **US5** (T058–T059, T046–T047, T048, T062, T049): plan/apply with protection, chain and destroy, L7
   collector and assertions, owner chain run.
9. **Polish** (T050–T051): how-to, exit.

## Verification strategy
V001–V012 in `contracts/checks.md`. Every offline V-check runs through `lz-offline` and records
discovery counts. Live V009/V010 are owner sessions; their evidence is the PR description (command
output summary, run id, approximate cost, known deviations) plus `evidence/T0NN.md`.

## Risks
| Risk | Effect | Mitigation |
| --- | --- | --- |
| Project import plans a replacement (P5) | an order for a new project | `prevent_destroy` fails the plan; the lane refuses delete/replace of retained resources; plan-only probe first; fallback reference mode |
| Lockfile or encryption incompatible with OVH S3 (P1–P3) | no remote state | probe first; fallback: local encrypted state per stack with a per-tenant flock — changes FR-008, so the spec and V005/V010 are revised before T038 closes |
| Retained infrastructure destroyed by a later apply (`org` change, tenant removal, removed block) | state and artefacts lost | `prevent_destroy` on state buckets and project; plan-level refusal on every verb; `destroy` refuses retained instances (G7) |
| Tenant authority deletes state buckets through the API (KD-1) | tenant can destroy platform or other tenants' state in the sandbox | narrowest allowlist; recorded deviation; V010 canary negative; no isolation claim; separate state project lifts it |
| Provider docs ahead of pin (P20) | validate errors | L0 validate against the mirrored 2.21.0 provider catches it |
| Bucket name collision (P21) | bootstrap fails on a new account | `org` discriminator (`lz`, D87) in the manifest; bootstrap fails clearly on a taken name and names the override |
| Tenant allowlist too narrow (P9) | chain fails mid-apply | T010 probes the allowlist first; trap destroys; widen with recorded evidence |
| Previous account's files used after migration | apply against the wrong account | per-account binding directory; credential's `GET /me` compared before use (G13) |
| Admin drift (outside OpenTofu state) | bootstrap authority wider or narrower than intended | bootstrap `admin` phase compares client and policy every run; repair only with root keys |
| Passphrase loss | all state unreadable | sandbox only; re-run bootstrap; documented non-claim |
| Live run from a candidate worktree or unreviewed tree | agent-authored code runs with credentials | host guard via git metadata, clean tree and reviewed SHA on `origin/main` (G10) |

## Complexity tracking
| Addition | Why | Simpler alternative rejected |
| --- | --- | --- |
| Components layer in a PoC | keeps ADR-0002 matrix unwidened (R1) | stage → module edge needs an ADR change |
| `tenant-state` stack per tenant | tenant onboarding without re-applying the most privileged local-state root (D88) | tenant buckets in `bootstrap` |
| Scripted admin creation through the API | no stack can run as the principal it creates; root keys never stored (D88) | `account-admin` stack (authority loop, secret import id) |
| Run core before probes | constitution V cleanup must be tested before the first live resource | ad-hoc probe scripts with manual cleanup |
| Go live wrapper | tested guards, traps and redaction with mutation proof | shell is hard to test |
