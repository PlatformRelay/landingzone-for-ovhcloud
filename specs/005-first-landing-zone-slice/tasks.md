# Tasks: 005-first-landing-zone-slice
Status: planned · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md

All boxes are open. Acceptance targets are **planned** and do not exist yet. Test tasks close with a
valid control plus behavioural red against a compiling stub (for HCL: variables/outputs only, or a
resource without the asserted attribute); a missing tool, syntax error or outage is not red.
Implementation tasks close with the same controls green. One logical commit per task.
Offline commands run through `lz-offline` (`<approved-absolute-path>/lz-offline --candidate <checkout>
-- task <target>`); `go -C tools test …` lines are isolated-child commands entered the same way.
Host-only targets (`stacks:reconcile`, `stacks:generate`, `bootstrap:account`, `live:*`) run in the
owner's main checkout on a reviewed commit (D87). Tasks marked **Owner session** in their title are started by the operator; agents
prepare them and the agent loop skips them.
Mutation proof is required only where a Verify line names a guard G1–G11 (`contracts/checks.md`).
Evidence: the Verify output is summarised in the PR description with the run id where one exists;
`evidence/<TID>.md` in this directory points at the PR. No evidence packets.

## Setup and foundation

- [ ] T001 Write spec-scoped traceability tests in tools/internal/checks/traceability_test.go and tools/cmd/lz-check/main_test.go
  - Requirements: FR-014; ADRs: 0008, 0019. Depends on: 001/T007.
  - Verify: `go -C tools test ./internal/checks ./cmd/lz-check -run "TestTraceability|TestSpecs" -count=1`: a two-spec fixture where both specs define FR-001 traces each against its own registry entries; spec 001's real tree still passes. A spec-005 requirement resolving to spec 001's ADR list, an unmapped requirement and a task without Verify are behavioural red on the current global-key registry.
  - Evidence: PR; `evidence/T001.md`; initial status `not-run`.

- [ ] T002 Implement spec-scoped requirement keys in harness/checks.yaml, tools/internal/checks/traceability.go and Taskfile.yml
  - Requirements: FR-014; ADRs: 0008, 0019. Depends on: T001.
  - Verify: `task check:specs -- specs/001-offline-foundation; task check:specs -- specs/005-first-landing-zone-slice`: both green (planned spec 005 checks report not-run, never pass); T001 controls green. `check:specs` takes the spec directory from CLI_ARGS; spec 001 keys keep working (recommended form `005/FR-001`, research R18).
  - Evidence: PR; `evidence/T002.md`; initial status `not-run`.

- [ ] T003 Write unit-runner tests in tools/internal/checks/unit_test.go and tests/check/fixtures/unit/{pass,fail,zero,skip}/
  - Requirements: FR-013; ADRs: 0008. Depends on: 001/T005, 001/T009.
  - Verify: `go -C tools test -tags offlinetools ./internal/checks -run TestUnit -count=1`: pinned `tofu test -json` through the 001 report adapter on the pass fixture is accepted; fail, zero-tests and skipped fixtures and a directory list with zero entries are behavioural red against a stub that reports pass.
  - Evidence: PR; `evidence/T003.md`; initial status `not-run`.

- [ ] T004 Implement `task test:unit` and `task test:slice` in tools/internal/checks/unit.go, tools/cmd/lz-check/main.go and Taskfile.yml
  - Requirements: FR-013, SC-001; ADRs: 0008. Depends on: T003.
  - Verify: `go -C tools test -tags offlinetools ./internal/checks -run TestUnit -count=1; task test:slice`: T003 controls green; mirror-only `init -backend=false -lockfile=readonly` then `tofu test -json`; `test:slice` discovers library and stage directories from the dependency graph and reports `fail` with zero discovery until T012 creates modules/naming (expected and recorded, not a pass).
  - Evidence: PR; `evidence/T004.md`; initial status `not-run`.

- [ ] T005 Write purity-rule tests in tools/internal/checks/dependencies_test.go and tests/check/fixtures/dependencies/
  - Requirements: FR-003; ADRs: 0002, 0004. Depends on: 001/T009.
  - Verify: `go -C tools test ./internal/checks -run TestDependencies -count=1`: a generated stack with backend+provider calling a stage, a stage calling a component, a component calling a module and naming are accepted; a `backend` block or `provider` configuration block in a module/component/stage, `terraform_remote_state` in any directory, and a non-generated `.tf` under `stacks/` are behavioural red (currently unreported). Existing `mayUse` controls stay unchanged.
  - Evidence: PR; `evidence/T005.md`; initial status `not-run`.

- [ ] T006 Implement purity rules in tools/internal/checks/dependencies.go
  - Requirements: FR-003; ADRs: 0002, 0004. Depends on: T005.
  - Verify: `task test:dependencies`: T005 controls green with new rule ids (`LIBRARY_BACKEND`, `LIBRARY_PROVIDER_CONFIG`, `REMOTE_STATE`, `HANDWRITTEN_INSTANCE`) added to `DependencyRules`; `mayUse` diff is empty; the repository's own graph is green.
  - Evidence: PR; `evidence/T006.md`; initial status `not-run`.

## Premise probes

Independent test: premise status rows in spec.md updated with observations. Refutations are recorded
in research.md with the fallback; they never pass silently.

- [ ] T007 Capture pinned OpenTofu and Terramate behaviour for P4, P6, P16 and P17 in tests/fixtures/{tofu-probes,terramate}/
  - Requirements: FR-007, FR-008, FR-009; ADRs: 0007, 0009, 0011. Depends on: 001/T003 capture admission.
  - Verify: Procedure through `lz-offline` capture admission with the pinned binaries, recording each generating command and version: (P4) `tofu init`/`apply` of a provider-free root whose local backend path and PBKDF2 passphrase come from variables; wrong passphrase refuses to read state. (P6) `tofu test` with `mock_provider` where a root `import` block targets a nested module address. (P16) two applies with a changed `tags["lz:run-id"]` under `ignore_changes` keep the first value. (P17) `terramate create --id --tags --after`, `terramate generate`, `terramate list --run-order` on a scratch tree. Each premise is marked observed or refuted in spec.md; a refutation updates research.md before T035/T037.
  - Evidence: PR; `evidence/T007.md`; initial status `not-run`.

- [ ] T008 Write live probe roots in tests/live/probes/{project-import,alerting,quota,state-backend,iam,network}/ and their run sheet
  - Requirements: FR-004, FR-008, FR-010, FR-011; ADRs: 0005, 0008, 0009, 0018. Depends on: T004.
  - Verify: `task lint -- tests/live/probes/project-import` and the same for each probe root: L0 green; every root names its resources with the prefix `lzprobe-` and tag `lz:run-id`; the run sheet lists commands, expected observations, destroy step and leftover listing. No apply here.
  - Evidence: PR; `evidence/T008.md`; initial status `not-run`.

- [ ] T009 Owner session: run read-only and plan-only probes P5, P10, P11, P18 and capture `ovhcloud` listings in tests/fixtures/ovhcloud/
  - Requirements: FR-004, FR-011; ADRs: 0005, 0008, 0024. Depends on: T008; owner, `sandbox.env`, `ovhcloud` 0.15.0.
  - Verify: Owner session: `tofu plan -generate-config-out` with an import block for the sandbox project (no apply) — pass when no replacement or order is planned, refute otherwise (→ `reference` mode in T039); `tofu plan` of alerting and quota roots; `ovhcloud` list of buckets, private networks, OAuth2 clients and IAM policies captured with version and command, ids sanitised. A failed listing is recorded as refuted, not skipped.
  - Evidence: PR (commands, outcomes); `evidence/T009.md`; initial status `not-run`.

- [ ] T010 Owner session: run create→destroy probes P1–P3, P7, P8, P12–P15 from tests/live/probes/
  - Requirements: FR-004, FR-008, FR-010; ADRs: 0008, 0009, 0018. Depends on: T009.
  - Verify: Owner session per run sheet: versioned probe bucket as S3 backend with `use_lockfile` and encryption — concurrent plan refused with a lock error, state object not plaintext; probe OAuth2 client and IAM policy created and usable as identity; private network + subnet in GRA11; tags with `:` on bucket and project URN; destroy all; `ovhcloud` listing shows no `lzprobe-` leftover. Record approximate cost.
  - Evidence: PR (run id, cost, observations); `evidence/T010.md`; initial status `not-run`.

## US1 — Names, labels and the first stage, offline (P1)

Independent test: V001, V002 on the US1 directories, V003.

- [ ] T011 [US1] Write naming tests in modules/naming/tests/{unit,contract}.tftest.hcl (supersedes 001/T013 for the default-template scope)
  - Requirements: FR-001, FR-002, SC-001; ADRs: 0003, 0008. Depends on: T004.
  - Verify: `task test:unit -- modules/naming`: against a compiling stub (variables and outputs only) concrete names for every kind in the default template and in one reordered test template, and the five mandatory labels, are behavioural red; `expect_failures` cover over-limit, forbidden character, doubled bucket punctuation, empty segment, unknown kind and mandatory-key override; a label-only change keeps the name.
  - Evidence: PR; `evidence/T011.md`; initial status `not-run`.

- [ ] T012 [US1] Implement modules/naming/{main,variables,outputs,versions}.tf, kinds.yaml and README.md (supersedes 001/T014 for the default-template scope)
  - Requirements: FR-001, FR-002, SC-001; ADRs: 0003. Depends on: T011.
  - Verify: `task test:unit -- modules/naming; task lint -- modules/naming; task test:dependencies`: T011 controls green; provider-free module; each `kinds.yaml` row cites its source or is marked UNVERIFIED and refused for cloud kinds without a limit. Guard G4: a mutant letting extra labels override `lz:tenant` fails.
  - Evidence: PR; `evidence/T012.md`; initial status `not-run`.

- [ ] T013 [US1] Write object storage module tests in modules/{object-storage,object-storage-user}/tests/
  - Requirements: FR-002, FR-004, FR-013; ADRs: 0003, 0008, 0009. Depends on: T012.
  - Verify: `task test:unit -- modules/object-storage; task test:unit -- modules/object-storage-user`: with `mock_provider "ovh"` the bucket gets the given name, region, `versioning` when requested and exactly the given tags; the S3 user has `objectstore_operator`, the policy JSON allows only the given bucket ARNs; the credential secret is only a sensitive output. Stubs are behavioural red.
  - Evidence: PR; `evidence/T013.md`; initial status `not-run`.

- [ ] T014 [US1] Implement modules/object-storage and modules/object-storage-user
  - Requirements: FR-002, FR-004, FR-013; ADRs: 0003, 0009. Depends on: T013.
  - Verify: `task test:unit -- modules/object-storage; task test:unit -- modules/object-storage-user; task lint -- modules/object-storage; task lint -- modules/object-storage-user`: green; resources `ovh_cloud_project_storage`, `ovh_cloud_project_user`, `ovh_cloud_project_user_s3_credential`, `ovh_cloud_project_user_s3_policy` (provider docs cited in README); no attribute absent from 2.21.0 (P20).
  - Evidence: PR; `evidence/T014.md`; initial status `not-run`.

- [ ] T015 [US1] Write state-backend component and bootstrap stage tests in components/state-backend/tests/ and stages/bootstrap/tests/
  - Requirements: FR-004, FR-005, FR-008; ADRs: 0004, 0009. Depends on: T006, T014.
  - Verify: `task test:unit -- components/state-backend; task test:unit -- stages/bootstrap`: account bucket name from naming (`org`, kind bucket, role state) and one bucket per tenant input (`org`, tenant, kind bucket, role state), versioning enabled, mandatory labels; platform S3 user policy limited to these state buckets; stage outputs `state_bucket`, `tenant_state_buckets`, `state_region`, `state_endpoint`, `platform_s3_user_id` and only-sensitive credentials; stage has no backend/provider configuration. Stubs are behavioural red.
  - Evidence: PR; `evidence/T015.md`; initial status `not-run`.

- [ ] T016 [US1] Implement components/state-backend and stages/bootstrap
  - Requirements: FR-004, FR-005, FR-008; ADRs: 0004, 0009. Depends on: T015.
  - Verify: `task test:unit -- components/state-backend; task test:unit -- stages/bootstrap; task test:slice; task test:dependencies`: green; `test:slice` now discovers and passes every US1 directory.
  - Evidence: PR; `evidence/T016.md`; initial status `not-run`.

## US2 — Every in-scope stage planned offline with typed output contracts (P1)

Independent test: V002 for all slice directories, V003, V004.

- [ ] T017 [US2] Write output-contract tests in tools/internal/stacks/outputs_test.go and tests/fixtures/outputs/
  - Requirements: FR-005, SC-005; ADRs: 0004, 0017. Depends on: T016; 001/T003 capture admission.
  - Verify: `go -C tools test ./internal/stacks -run TestOutputs -count=1`: from a captured pinned `tofu output -json` of a provider-free fixture root (sensitive and plain outputs; command recorded) the envelope builder keeps plain values and drops sensitive ones; fixture envelopes per stage validate. Kept sensitive entry, secret-pattern key, unknown field, missing value, `null`/`""` capability and wrong `stage` are behavioural red against a pass-through stub.
  - Evidence: PR; `evidence/T017.md`; initial status `not-run`.

- [ ] T018 [US2] Implement the envelope builder and validator in tools/internal/stacks/outputs.go, schemas/outputs/*.schema.json and Taskfile.yml
  - Requirements: FR-005, SC-005; ADRs: 0004, 0017. Depends on: T017.
  - Verify: `task test:outputs`: T017 controls green; one schema per in-scope stage plus the envelope; guard G2 (envelope part): removing the sensitive filter fails. Creates `test:outputs`.
  - Evidence: PR; `evidence/T018.md`; initial status `not-run`.

- [ ] T019 [US2] Write IAM primitive module tests in modules/{iam-service-account,iam-policy,identity-group}/tests/
  - Requirements: FR-004, FR-010; ADRs: 0003, 0018. Depends on: T012.
  - Verify: `task test:unit -- modules/iam-service-account; task test:unit -- modules/iam-policy; task test:unit -- modules/identity-group`: OAuth2 client uses `CLIENT_CREDENTIALS`, secret only as sensitive output, `identity` URN exported; policy passes identities, resources, allow, optional conditions unchanged; group role defaults to `NONE`. Stubs are behavioural red.
  - Evidence: PR; `evidence/T019.md`; initial status `not-run`.

- [ ] T020 [US2] Implement modules/iam-service-account, modules/iam-policy and modules/identity-group
  - Requirements: FR-004, FR-010; ADRs: 0003, 0018. Depends on: T019.
  - Verify: `task test:unit -- modules/iam-service-account; task test:unit -- modules/iam-policy; task test:unit -- modules/identity-group; task lint -- modules/iam-policy`: green; resources `ovh_me_api_oauth2_client`, `ovh_iam_policy`, `ovh_me_identity_group` (docs cited); no `discard_client_secret` (2.22.0 only).
  - Evidence: PR; `evidence/T020.md`; initial status `not-run`.

- [ ] T021 [US2] Write account-baseline component and account-admin stage tests in components/account-baseline/tests/ and stages/account-admin/tests/
  - Requirements: FR-004, FR-010, FR-012; ADRs: 0009, 0018. Depends on: T020, T018.
  - Verify: `task test:unit -- components/account-baseline; task test:unit -- stages/account-admin`: admin client named by naming; its policy allows exactly `account:apiovh:iam/*`, `account:apiovh:me/*`, `publicCloudProject:apiovh:*` on the account and project URNs (AGENTS.md); outputs match the account-admin schema; secret only sensitive. Stubs are behavioural red.
  - Evidence: PR; `evidence/T021.md`; initial status `not-run`.

- [ ] T022 [US2] Implement components/account-baseline and stages/account-admin
  - Requirements: FR-004, FR-010, FR-012; ADRs: 0009, 0018. Depends on: T021.
  - Verify: `task test:unit -- components/account-baseline; task test:unit -- stages/account-admin; task test:dependencies`: green; the stage accepts an existing client for import (generated import block lives in the stack, T037).
  - Evidence: PR; `evidence/T022.md`; initial status `not-run`.

- [ ] T023 [US2] Write identity/ovh-native component and account-governance stage tests in components/identity/ovh-native/tests/ and stages/account-governance/tests/
  - Requirements: FR-004, FR-010, FR-013; ADRs: 0006, 0009, 0018. Depends on: T020, T014, T018.
  - Verify: `task test:unit -- components/identity/ovh-native; task test:unit -- stages/account-governance`: platform deployer policy covers `publicCloudProject:apiovh:*` on the tenant project URNs; tenant deployer policy lists only the P9 allowlist on its own project URN and no `account:apiovh:iam/` action; tenant S3 user policy covers only its tenant's state bucket (from `bootstrap` outputs); tenant group created with no members; outputs match schema. Guards G5, G6: a mutant widening the tenant resource to `*`, adding an IAM action or granting the account bucket or another tenant's bucket is red.
  - Evidence: PR; `evidence/T023.md`; initial status `not-run`.

- [ ] T024 [US2] Implement components/identity/ovh-native and stages/account-governance
  - Requirements: FR-004, FR-010, FR-013; ADRs: 0006, 0009, 0018. Depends on: T023.
  - Verify: `task test:unit -- components/identity/ovh-native; task test:unit -- stages/account-governance; task test:dependencies`: T023 controls and G5/G6 mutants green/red as specified.
  - Evidence: PR; `evidence/T024.md`; initial status `not-run`.

- [ ] T025 [US2] Write cloud-project and cloud-quota module tests in modules/{cloud-project,cloud-quota}/tests/
  - Requirements: FR-002, FR-004; ADRs: 0003, 0005, 0006. Depends on: T012.
  - Verify: `task test:unit -- modules/cloud-project; task test:unit -- modules/cloud-quota`: `adopt` mode manages `ovh_cloud_project` with `prevent_destroy` and `deletion_protection = true`; `reference` mode reads `data.ovh_cloud_project` and manages no project; both tag the project URN through `ovh_iam_resource_tags`; alerting only when enabled; quota resource only when enabled with `prevent_automatic_quota_upgrade = true`. Stubs are behavioural red.
  - Evidence: PR; `evidence/T025.md`; initial status `not-run`.

- [ ] T026 [US2] Implement modules/cloud-project and modules/cloud-quota
  - Requirements: FR-002, FR-004; ADRs: 0003, 0005, 0006. Depends on: T025.
  - Verify: `task test:unit -- modules/cloud-project; task test:unit -- modules/cloud-quota; task lint -- modules/cloud-project`: green. Guard G7 (module part): a mutant dropping `prevent_destroy` fails a test that inspects the configuration (static assertion via `lz-check deps` lifecycle scan or a tftest expectation; whichever T025 chose).
  - Evidence: PR; `evidence/T026.md`; initial status `not-run`.

- [ ] T027 [US2] Write project-factory component and project stage tests in components/project-factory/tests/ and stages/project/tests/
  - Requirements: FR-002, FR-004, FR-005; ADRs: 0004, 0005, 0006. Depends on: T026, T018.
  - Verify: `task test:unit -- components/project-factory; task test:unit -- stages/project`: tenant/environment labels from inputs; budget alert and quota toggles pass through; outputs `project_id`, `project_urn`, `regions` match the project schema. Stubs are behavioural red.
  - Evidence: PR; `evidence/T027.md`; initial status `not-run`.

- [ ] T028 [US2] Implement components/project-factory and stages/project
  - Requirements: FR-002, FR-004, FR-005; ADRs: 0004, 0005, 0006. Depends on: T027.
  - Verify: `task test:unit -- components/project-factory; task test:unit -- stages/project; task test:dependencies`: green.
  - Evidence: PR; `evidence/T028.md`; initial status `not-run`.

- [ ] T029 [US2] Write private-network module, network/island component and project-network stage tests in modules/private-network/tests/, components/network/island/tests/ and stages/project-network/tests/
  - Requirements: FR-004, FR-005; ADRs: 0004, 0017. Depends on: T012, T018.
  - Verify: `task test:unit -- modules/private-network; task test:unit -- components/network/island; task test:unit -- stages/project-network`: one network in the given region, one subnet with the given CIDR, DHCP on, no gateway resource; invalid CIDR and region outside the project's regions are rejected; outputs match schema and list the network and subnet as `unlabelled`. Stubs are behavioural red.
  - Evidence: PR; `evidence/T029.md`; initial status `not-run`.

- [ ] T030 [US2] Implement modules/private-network, components/network/island and stages/project-network
  - Requirements: FR-004, FR-005; ADRs: 0004, 0017. Depends on: T029.
  - Verify: `task test:unit -- modules/private-network; task test:unit -- components/network/island; task test:unit -- stages/project-network; task test:dependencies`: green; resources `ovh_cloud_project_network_private`, `ovh_cloud_project_network_private_subnet` (docs cited).
  - Evidence: PR; `evidence/T030.md`; initial status `not-run`.

- [ ] T031 [US2] Write runtime/managed-only component and runtime stage tests in components/runtime/managed-only/tests/ and stages/runtime/tests/
  - Requirements: FR-004, FR-005; ADRs: 0004, 0017. Depends on: T014, T018.
  - Verify: `task test:unit -- components/runtime/managed-only; task test:unit -- stages/runtime`: one labelled bucket; envelope `kind = managed-only`, `scope`, `readiness`, `pending_actions = []`, `capabilities` with `object-storage` and without `network`; inputs consume only `project` outputs. A stub publishing an empty `network` capability is behavioural red.
  - Evidence: PR; `evidence/T031.md`; initial status `not-run`.

- [ ] T032 [US2] Implement components/runtime/managed-only and stages/runtime
  - Requirements: FR-004, FR-005, FR-013, SC-001; ADRs: 0004, 0017. Depends on: T031, T016, T022, T024, T028, T030.
  - Verify: `task test:unit -- components/runtime/managed-only; task test:unit -- stages/runtime; task test:slice; task test:dependencies; task test:outputs`: green; `test:slice` covers every module, component and stage of the slice with nonzero test counts (V002 checkpoint).
  - Evidence: PR; `evidence/T032.md`; initial status `not-run`.

## US3 — Stacks generated from `deployments.yaml` and planned offline (P2)

Independent test: V005, V006.

- [ ] T033 [US3] Write manifest decoding tests in tools/internal/stacks/manifest_test.go and tests/fixtures/manifests/{sandbox,growth,invalid}/
  - Requirements: FR-006, SC-002; ADRs: 0004, 0005, 0007. Depends on: T002.
  - Verify: `go -C tools test ./internal/stacks -run TestManifest -count=1`: sandbox and growth (2 tenants × 2 environments × 2 regions) manifests decode with derived paths, state buckets, state keys, tags and edges matching independently written expectations; each invalid case of V005 (unknown field, duplicate key, duplicate id, scope violations, unsupported version, `account-fabric` row) is behavioural red against a permissive stub.
  - Evidence: PR; `evidence/T033.md`; initial status `not-run`.

- [ ] T034 [US3] Implement schemas/deployments.schema.json and tools/internal/stacks/{manifest,stages}.go
  - Requirements: FR-006, SC-002; ADRs: 0004, 0005, 0007. Depends on: T033.
  - Verify: `task test:stacks`: T033 controls green; strict decoding reuses `checks.DecodeStrict`; stage table matches data-model.md. Creates `test:stacks` (extended by T036, T038, T041).
  - Evidence: PR; `evidence/T034.md`; initial status `not-run`.

- [ ] T035 [US3] Write reconciler tests in tools/internal/stacks/reconcile_test.go using tests/fixtures/terramate/
  - Requirements: FR-007; ADRs: 0004, 0007. Depends on: T034, T007.
  - Verify: `go -C tools test ./internal/stacks -run TestReconcile -count=1`: on a scratch tree a missing stack is created through the pinned `terramate create` with the derived id, tags and `after`; a repeat run is a no-op; a directory without a row, a changed id and a changed dimension yield `UNSUPPORTED_CHANGE`; `account-fabric` yields `STAGE_NOT_IMPLEMENTED`; `--check` reports without writing. A stub that only creates is behavioural red.
  - Evidence: PR; `evidence/T035.md`; initial status `not-run`.

- [ ] T036 [US3] Implement the reconciler in tools/internal/stacks/reconcile.go, tools/cmd/lz-stacks/ and Taskfile.yml
  - Requirements: FR-007; ADRs: 0004, 0007. Depends on: T035.
  - Verify: `task test:stacks; task stacks:check`: T035 controls green; `stacks:reconcile` (host) and `stacks:check` (offline, scratch copy) created; with no manifest yet `stacks:check` reports `fail: no manifest` (expected until T039).
  - Evidence: PR; `evidence/T036.md`; initial status `not-run`.

- [ ] T037 [US3] Write generation tests in tools/internal/stacks/generate_test.go and tests/fixtures/manifests/sandbox/expected/
  - Requirements: FR-007, FR-008, FR-002; ADRs: 0004, 0007, 0009. Depends on: T036, T032.
  - Verify: `go -C tools test ./internal/stacks -run TestGenerate -count=1`: generating the sandbox fixture into scratch with pinned Terramate yields, per stage kind, files equal to the hand-written expectations: S3 backend with derived bucket (account or tenant) and key, `use_lockfile = true`, OVH endpoint flags and `encryption {}` (enforced for state and plan); local backend path variable for `account-admin`/`bootstrap`; provider block without credentials; the single stage call with literal source; typed input variables; labels `managed-in`/`instance`/`tenant`; import block only for adopt; offline test file with `mock_provider`. Missing `encryption`, an import on a non-adopt project and any `terraform_remote_state` are behavioural red.
  - Evidence: PR; `evidence/T037.md`; initial status `not-run`.

- [ ] T038 [US3] Implement Terramate configuration in terramate.tm.hcl and stacks/_lz/*.tm.hcl with `task stacks:generate` and `task test:stack-plans`
  - Requirements: FR-007, FR-008, FR-002, FR-013; ADRs: 0004, 0007, 0009. Depends on: T037.
  - Verify: `task test:stacks; task test:stack-plans`: T037 controls green; the growth fixture generated into scratch plans every stack under mocks with fixture inputs (SC-002); `test:stack-plans` reports zero discovery as `fail` until T039.
  - Evidence: PR; `evidence/T038.md`; initial status `not-run`.

- [ ] T039 [US3] Add stacks/deployments.yaml for demo/dev/GRA11 and the generated stacks under stacks/
  - Requirements: FR-006, FR-007, FR-008, SC-002; ADRs: 0004, 0007. Depends on: T038, T022; T009 result for the project mode (adopt unless P5 was refuted, then `reference`).
  - Verify: `task stacks:check; task test:stack-plans; task test:dependencies; task lint -- stacks/tenants/demo/dev/gra11/runtime`: six stacks, generated files fresh, every stack plans offline, all directories classified as generated instances; editing one generated file makes `stacks:check` fail (control, reverted).
  - Evidence: PR; `evidence/T039.md`; initial status `not-run`.

- [ ] T040 [US3] Write order and re-plan selection tests in tools/internal/stacks/selection_test.go
  - Requirements: FR-009; ADRs: 0004, 0007. Depends on: T034, T007.
  - Verify: `go -C tools test ./internal/stacks -run TestSelection -count=1`: order equals the topological order of derived edges and the captured `terramate list --run-order`; a changed producer digest selects its data consumers, an authority-only edge does not; a missing record selects; an unrelated tenant is excluded; a second lock holder for one tenant is refused. An `after`-only stub is behavioural red.
  - Evidence: PR; `evidence/T040.md`; initial status `not-run`.

- [ ] T041 [US3] Implement selection and the per-tenant lock in tools/internal/stacks/selection.go with `task stacks:order`
  - Requirements: FR-009; ADRs: 0004, 0007. Depends on: T040, T039.
  - Verify: `task test:stacks; task stacks:order -- all`: T040 controls green; prints the sandbox order `account-admin → account-bootstrap → account-governance → demo-dev-project → {network, runtime}` with reasons.
  - Evidence: PR; `evidence/T041.md`; initial status `not-run`.

## US4 — Scripted, re-runnable account bootstrap (P2)

Independent test: V008 offline; V009 owner session.

- [ ] T042 [US4] Write bootstrap tool tests in tools/internal/live/bootstrap_test.go with fake tofu and ovhcloud binaries in tools/internal/live/testdata/
  - Requirements: FR-010, FR-012, SC-005; ADRs: 0009, 0018. Depends on: T039.
  - Verify: `go -C tools test ./internal/live -run TestBootstrap -count=1`: empty fake account runs `admin`, `passphrase`, `state`, `verify` in order; a second run reports each phase `unchanged`; a partial state runs only missing phases; root AK/AS/CK are refused without `--fresh-account`; `root-bootstrap.env` is removed only after confirmed revocation; credential files are 0600 under the config dir. Guards G2, G3, G10, G11: a seeded secret on stdout/stderr, a 0644 file, a path inside the repo, a run inside `lz-offline` and an overwritten passphrase are each behavioural red against a stub.
  - Evidence: PR; `evidence/T042.md`; initial status `not-run`.

- [ ] T043 [US4] Implement `lz-live bootstrap` in tools/cmd/lz-live/, tools/internal/live/{bootstrap,files,redact}.go and the `bootstrap:account` Taskfile target
  - Requirements: FR-010, FR-012, SC-005; ADRs: 0009, 0018. Depends on: T042.
  - Verify: `task test:bootstrap`: T042 controls green with each named guard's mutant killed; creates `test:bootstrap` and the host target `bootstrap:account`.
  - Evidence: PR; `evidence/T043.md`; initial status `not-run`.

- [ ] T044 [US4] Owner session: bootstrap the current sandbox account twice with `task bootstrap:account`
  - Requirements: FR-008, FR-012, SC-004; ADRs: 0009. Depends on: T043, T010.
  - Verify: Owner session: first run imports `lz-sandbox-admin` into `account-admin` (no new client), creates the passphrase file, the account and tenant state buckets and `state.env`; second run reports every phase `unchanged` and a no-change plan; the state object in the bucket is not plaintext JSON; nothing secret on the terminal.
  - Evidence: PR (run id, approximate cost); `evidence/T044.md`; initial status `not-run`.

- [ ] T045 [US4] Owner session: bootstrap a fresh OVHcloud account with `task bootstrap:account -- --fresh-account`
  - Requirements: FR-012, SC-004; ADRs: 0009. Depends on: T044; a fresh account with one project (P22); blocked until one exists.
  - Verify: Owner session per quickstart *Fresh account*: admin client created, AK/CK revocation done and `root-bootstrap.env` removed, state backend working; a second run is `unchanged`. Absent fresh account → `blocked`, never pass.
  - Evidence: PR (run id, approximate cost); `evidence/T045.md`; initial status `not-run`.

## US5 — Live chain apply→destroy in the sandbox (Owner session, P3)

Independent test: V007 offline; V010 owner session.

- [ ] T046 [US5] Write live-lane tests in tools/internal/live/{chain,credentials,leftovers}_test.go using fakes and tests/fixtures/ovhcloud/
  - Requirements: FR-009, FR-010, FR-011, SC-005; ADRs: 0004, 0008, 0009, 0024. Depends on: T043, T041, T009.
  - Verify: `go -C tools test ./internal/live -run "TestChain|TestCredentials|TestLeftovers" -count=1`: fake chain applies in order, publishes envelopes, destroys `runtime`, `project-network`, `account-governance` in reverse order and retains `account-admin`, `bootstrap`, `project`; inventory written before destroy; captured clean listing passes the leftover check. Guards G1, G2, G7 (retained set), G8, G9, G10: platform credentials for a tenant stack, a seeded secret in any stream or file, destroying `project`, no destroy after an apply failure/SIGINT/SIGTERM, forward destroy order, a listing error or missing `ovhcloud` reported as pass, and a run inside `lz-offline` or from a `worktrees/` path are each behavioural red.
  - Evidence: PR; `evidence/T046.md`; initial status `not-run`.

- [ ] T047 [US5] Implement `lz-live plan|apply|destroy|chain` in tools/cmd/lz-live/ and tools/internal/live/, Taskfile `live:*` targets and mise.live.toml
  - Requirements: FR-009, FR-010, FR-011, SC-005; ADRs: 0004, 0008, 0009, 0024. Depends on: T046.
  - Verify: `task test:live-lane`: T046 controls green with each named guard's mutant killed; `ovhcloud` 0.15.0 pinned in `mise.live.toml` only; `task verify:toolchain` unchanged. Creates `test:live-lane` and host targets `live:plan`, `live:apply`, `live:destroy`, `live:chain`.
  - Evidence: PR; `evidence/T047.md`; initial status `not-run`.

- [ ] T048 [US5] Write L7 chain assertions in tools/internal/probes/live/chain/chain_test.go with observation fixtures in tests/live/chain/
  - Requirements: FR-004, FR-008, FR-010, FR-011, SC-003; ADRs: 0006, 0008, 0009. Depends on: T047.
  - Verify: `go -C tools test -tags live ./internal/probes/live/chain -run TestChainObservations -count=1` against recorded fake observations (the live run supplies real ones): versioned state bucket, encrypted state object, lock contention refused, mandatory tags on bucket and project URN, tenant IAM write denied, tenant S3 read of the account bucket or another tenant's bucket denied, `outputs.json` schema-valid. An observation set missing any of these is behavioural red; the `live` tag keeps them out of offline discovery.
  - Evidence: PR; `evidence/T048.md`; initial status `not-run`.

- [ ] T049 [US5] Owner session: run `task live:chain -- all` in the sandbox
  - Requirements: FR-004, FR-008, FR-009, FR-010, FR-011, SC-003; ADRs: 0004, 0008, 0009, 0024. Depends on: T048, T044, T039.
  - Verify: Owner session: six stacks applied in order with the right authority each; T048 assertions pass on real observations; ephemeral destroy completes from the trap; leftover check zero; total < 60 min; run id and approximate cost recorded. A failed assertion still destroys and reports `fail`.
  - Evidence: PR (run id, cost, leftover result); `evidence/T049.md`; initial status `not-run`.

## Polish and exit

- [ ] T050 Write the how-to in docs/how-to/run-the-first-slice.md and link it from README.md
  - Requirements: FR-011, FR-012; ADRs: 0013. Depends on: T047.
  - Verify: exempt — docs-only; content review against the implemented commands, the owner-only boundary and the postponed list.
  - Evidence: PR; `evidence/T050.md`; docs content review only.

- [ ] T051 Run the slice exit checks V001–V011 through harness/checks.yaml
  - Requirements: FR-001, FR-002, FR-003, FR-004, FR-005, FR-006, FR-007, FR-008, FR-009, FR-010, FR-011, FR-012, FR-013, FR-014, SC-001, SC-002, SC-003, SC-004, SC-005; ADRs: 0002, 0003, 0004, 0007, 0008, 0009, 0018. Depends on: T049, T050; T045 result (pass or `blocked`).
  - Verify: every offline V-command green with discovery counts; V009/V010 PR records present; V009's fresh-account part may be `blocked` (then SC-004 is partial and the spec cannot be `done`).
  - Aggregate ADR rationale: 0002 layout and layers; 0003 names and labels; 0004 stacks and outputs; 0007 Terramate and Taskfile; 0008 test layers and live hygiene; 0009 state and credentials; 0018 deployer identities.
  - Evidence: PR; `evidence/T051.md`; initial status `not-run`.

## Planned command creators

| Target | Creating task | Runs |
| --- | --- | --- |
| `task check:specs -- <spec-dir>` | T002 (extends 001/T007) | offline |
| `task test:unit -- <dir>` | T004 | offline |
| `task test:slice` | T004 | offline |
| `task test:dependencies` (purity rules) | 001/T009, extended T006 | offline |
| `task test:outputs` | T018 | offline |
| `task test:stacks` | T034, extended T036, T038, T041 | offline |
| `task stacks:check` | T036 | offline |
| `task stacks:reconcile` | T036 | host |
| `task stacks:generate` | T038 | host |
| `task test:stack-plans` | T038 | offline |
| `task stacks:order -- <instance\|all>` | T041 | offline |
| `task test:bootstrap` | T043 | offline |
| `task bootstrap:account [-- --fresh-account]` | T043 | host, owner |
| `task test:live-lane` | T047 | offline |
| `task live:plan\|apply\|destroy\|chain -- <instance\|all>` | T047 | host, owner |
| `lz-stacks`, `lz-live` binaries | T036, T043/T047 | — |

## Dependencies and execution order
Per-task `Depends on` fields are authoritative and form a DAG.
- Setup T001–T006 first; T007 and T008 can start in parallel with US1.
- US1 T011→T016; US2 T017–T032 (IAM, project, network, runtime branches parallel after T018/T012);
  US3 T033→T041 (needs T007, T032); US4 T042→T045; US5 T046→T049.
- Owner sessions: T009 (read-only, early — unblocks T039's project mode and T046's fixtures), T010,
  T044, T045, T049. Constitution 1.3.0 (D87) makes cost guards hygiene, so no owner task waits on a
  cost disposition; the trap destroy and leftover check (T046–T047) come before the first live chain.

## Parallel opportunities
After T012: T013/T019/T025 in parallel (distinct modules). After T018: T021, T023, T027, T029, T031
test tasks in parallel. T033 can start after T002. No parallel flag bypasses an owner gate.

## Completion
Rerun the V-commands and read their output; a missing binary, zero discovery or a simulation never
closes a task. Owner tasks close only with the PR record. A refuted premise records its fallback; an
unrun owner task stays `not-run` or `blocked`.
