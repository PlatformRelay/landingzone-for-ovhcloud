# Tasks: 005-first-landing-zone-slice
Status: planned · Revised: 2026-10-06 (round-1 spec-set review, D88) · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md

All boxes are open. Acceptance targets are **planned** and do not exist yet. Test tasks close with a
valid control plus behavioural red against a compiling stub (for HCL: variables/outputs only, or a
resource without the asserted attribute); a missing tool, syntax error or outage is not red.
Implementation tasks close with the same controls green. **Exception**: an implementation task that
creates a discovery target before its first subject exists (T004, T036, T038) closes with that target
reporting `fail` on zero discovery — the expected, recorded result, never a `pass`; the task that
creates the first subject (T012, T039) turns it green. One logical commit per task.
Offline commands run through `lz-offline` (`<approved-absolute-path>/lz-offline --candidate <checkout>
-- task <target>`); `go -C tools test …` lines are isolated-child commands entered the same way.
Host-only targets (`stacks:reconcile`, `stacks:generate`, `bootstrap:account`, `live:*`) run in the
owner's main checkout on a reviewed commit, enforced by the host guard (T052–T053, D87). Tasks
marked **Owner session** in their title are started by the operator; agents prepare them and the
agent loop skips them.
Task ids are stable across revisions: tasks added by the round-1 revision (D88) carry ids T052–T062
and sit in the phase where they run; `Depends on` fields, not id order, give the execution order.
Mutation proof is required only where a Verify line names a guard G1–G15 (`contracts/checks.md`).
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

- [ ] T003 Write unit-runner tests in tools/internal/checks/unit_test.go and tests/check/fixtures/unit/{pass,fail,zero,skip,malformed}/
  - Requirements: FR-013; ADRs: 0008. Depends on: 001/T005, 001/T009.
  - Verify: `go -C tools test -tags offlinetools ./internal/checks -run TestUnit -count=1`: pinned `tofu test -json` through the 001 report adapter on the pass fixture is accepted; fail, zero-tests, skipped and malformed/truncated-stream fixtures and a directory list with zero entries are behavioural red against a stub that reports pass.
  - Evidence: PR; `evidence/T003.md`; initial status `not-run`.

- [ ] T004 Implement `task test:unit` and `task test:slice` in tools/internal/checks/unit.go, tools/cmd/lz-check/main.go and Taskfile.yml
  - Requirements: FR-013, SC-001; ADRs: 0008. Depends on: T003.
  - Verify: `go -C tools test -tags offlinetools ./internal/checks -run TestUnit -count=1; task test:slice`: T003 controls green; mirror-only `init -backend=false -lockfile=readonly` then `tofu test -json`; `test:slice` discovers library and stage directories from the dependency graph and reports `fail` with zero discovery until T012 creates modules/naming (header exception: expected and recorded, not a pass).
  - Evidence: PR; `evidence/T004.md`; initial status `not-run`.

- [ ] T005 Write purity-rule tests in tools/internal/checks/dependencies_test.go and tests/check/fixtures/dependencies/
  - Requirements: FR-003; ADRs: 0002, 0004. Depends on: 001/T009.
  - Verify: `go -C tools test ./internal/checks -run TestDependencies -count=1`: a generated stack with backend+provider calling a stage, a stage calling a component, a component calling a module and naming are accepted; a `backend` block or `provider` configuration block in a module/component/stage, `terraform_remote_state` in any directory, and a non-generated `.tf` under `stacks/` are behavioural red (currently unreported). Existing `mayUse` controls stay unchanged.
  - Evidence: PR; `evidence/T005.md`; initial status `not-run`.

- [ ] T006 Implement purity rules in tools/internal/checks/dependencies.go
  - Requirements: FR-003; ADRs: 0002, 0004. Depends on: T005.
  - Verify: `task test:dependencies`: T005 controls green with new rule ids (`LIBRARY_BACKEND`, `LIBRARY_PROVIDER_CONFIG`, `REMOTE_STATE`, `HANDWRITTEN_INSTANCE`) added to `DependencyRules`; `mayUse` diff is empty; the repository's own graph is green.
  - Evidence: PR; `evidence/T006.md`; initial status `not-run`.

## Live safety core (before any live probe)

Constitution V safety, not a cost gate: the host guard, child environment, deadline, inventory,
destroy-on-exit and leftover check exist and are mutation-tested before the first command that
loads credentials (T009) or creates a resource (T010).

- [ ] T052 Write host-guard, child-environment and account-binding tests in tools/internal/live/{guard,env,binding}_test.go with fake git and API fixtures in tools/internal/live/testdata/
  - Requirements: FR-010, FR-011, SC-005; ADRs: 0008, 0009, 0019. Depends on: 001/T009.
  - Verify: `go -C tools test ./internal/live -run "TestGuard|TestEnv|TestBinding" -count=1`: the owner's main checkout, clean, at the reviewed SHA reachable from `origin/main` is admitted before any credential file is opened. Guard G10: inside `lz-offline`, a linked worktree outside any `worktrees/` directory, a symlinked path to a linked worktree, a dirty tree (modified and untracked files), `HEAD` ≠ `--reviewed-sha`, a SHA not reachable from `origin/main` and a missing `live.env` are each refused with exit 3. Guard G12: with `OVH_CLIENT_ID`, `OVH_CLIENT_SECRET`, `AWS_ACCESS_KEY_ID` set in the caller's environment, the child environment of a tenant-authority run carries none of them. Guard G13: a credential whose `GET /me` account differs from `account.env`, and a manifest `org` differing from the bound one, are refused. Stubs that admit everything are behavioural red.
  - Evidence: PR; `evidence/T052.md`; initial status `not-run`.

- [ ] T053 Implement the host guard, child environment and account binding in tools/internal/live/{guard,env,binding,files}.go and the tools/cmd/lz-live entry point
  - Requirements: FR-010, FR-011, SC-005; ADRs: 0008, 0009, 0019. Depends on: T052.
  - Verify: `go -C tools test ./internal/live -run "TestGuard|TestEnv|TestBinding" -count=1`: T052 controls green with the G10, G12, G13 mutants killed (skip offline detection; accept a linked worktree; accept a dirty tree; skip the SHA check; inherit ambient `OVH_*`; skip the account comparison). `files.go` is the only credential-file writer (0600 files, 0700 directories under `~/.config/ovh-lz/`).
  - Evidence: PR; `evidence/T053.md`; initial status `not-run`.

- [ ] T054 Write run-core tests (deadline, incremental inventory, destroy-on-exit, leftover kind matrix) in tools/internal/live/{runner,inventory,leftovers}_test.go with synthetic listings in tests/fixtures/ovhcloud/synthetic/
  - Requirements: FR-011, FR-013; ADRs: 0008, 0024. Depends on: T053, T007 (P24 capture).
  - Verify: `go -C tools test ./internal/live -run "TestRunner|TestInventory|TestLeftovers" -count=1`: a fake `tofu apply -json` stream appends each `apply_complete` id to `inventory.jsonl` before the next event, and the P24 fallback (state list after each stack) is exercised too; destroy-on-exit fires on apply failure, SIGINT, SIGTERM and deadline expiry, in reverse order, continuing after one destroy fails and exiting non-zero; a clean synthetic listing for every matrix kind passes, with pagination (two pages) and parent-child listings (subnets per network, S3 credentials per user). Guards G8, G9: no destroy on error or deadline, forward order, a listing error, a missing `ovhcloud`, an unparseable listing, a created resource type outside the matrix, and one seeded leftover per kind (including one present in no state) reported as pass are each behavioural red against stubs. Synthetic listings follow the API response schemas in `kb/api/` until T009's captures replace them (T046).
  - Evidence: PR; `evidence/T054.md`; initial status `not-run`.

- [ ] T055 Implement the run core and `lz-live probe` in tools/internal/live/{runner,inventory,leftovers,deadline}.go, tools/cmd/lz-live/ and the `live:probe` and `test:live-lane` Taskfile targets
  - Requirements: FR-011, FR-013; ADRs: 0008, 0024. Depends on: T054.
  - Verify: `task test:live-lane`: T054 controls green with the G8, G9 mutants killed; `lz-live probe <root> [--plan-only]` runs a probe root through guard, child environment, deadline (default 45 min), inventory, destroy-on-exit and leftover check, with an encrypted local backend under `accounts/<account>/state/probes/` and a per-run passphrase held in memory. Creates `test:live-lane` (extended by T059, T047, T062) and the host target `live:probe`.
  - Evidence: PR; `evidence/T055.md`; initial status `not-run`.

## Premise probes

Independent test: premise status rows in spec.md updated with observations. Refutations are recorded
in research.md with the fallback; they never pass silently.

- [ ] T007 Capture pinned OpenTofu and Terramate behaviour for P4, P6, P16, P17 and P24 in tests/fixtures/{tofu-probes,terramate}/
  - Requirements: FR-007, FR-008, FR-009, FR-011; ADRs: 0007, 0009, 0011. Depends on: 001/T003 capture admission.
  - Verify: Procedure through `lz-offline` capture admission with the pinned binaries, recording each generating command and version: (P4) `tofu init`/`apply` of a provider-free root whose local backend path and PBKDF2 passphrase come from variables; wrong passphrase refuses to read state. (P6) `tofu test` with `mock_provider` where a root `import` block targets a nested module address. (P16) two applies with a changed `tags["lz:run-id"]` under `ignore_changes` keep the first value. (P17) `terramate create --id --tags --after`, `terramate generate`, `terramate list --run-order` on a scratch tree. (P24) `tofu apply -json` of a provider-free root with three `terraform_data` resources: one `apply_complete` event per resource with address and id. Each premise is marked observed or refuted in spec.md; a refutation updates research.md before T035/T037/T054.
  - Evidence: PR; `evidence/T007.md`; initial status `not-run`.

- [ ] T008 Write live probe roots in tests/live/probes/{project-import,alerting,quota,state-backend,iam,network,storage-iam}/ and their run sheet
  - Requirements: FR-004, FR-008, FR-010, FR-011; ADRs: 0005, 0008, 0009, 0018. Depends on: T004.
  - Verify: `task lint -- tests/live/probes/project-import` and the same for each probe root: L0 green; every root names its resources with the prefix `lzprobe-` and tag `lz:run-id`; no root declares a backend path inside the checkout (state goes to `accounts/<account>/state/probes/`, encrypted, via `lz-live probe`); `storage-iam` holds a probe tenant identity with the P9 allowlist and two tagged probe buckets for P25. The run sheet lists commands, expected observations, destroy step and the leftover listing per matrix kind. No apply here.
  - Evidence: PR; `evidence/T008.md`; initial status `not-run`.

- [ ] T009 Owner session: run read-only and plan-only probes P5, P10, P11, P18 and capture `ovhcloud` listings for every leftover kind in tests/fixtures/ovhcloud/
  - Requirements: FR-004, FR-011; ADRs: 0005, 0008, 0024. Depends on: T008, T055; owner, `sandbox.env`, `ovhcloud` 0.15.0.
  - Verify: Owner session through `task live:probe -- <root> --plan-only` (guard and child environment active): `tofu plan -generate-config-out` with an import block for the sandbox project (no apply) — pass when no replacement or order is planned, refute otherwise (→ `reference` mode in T039); `tofu plan` of alerting and quota roots; read-only `ovhcloud` (or fallback API) listings, each with version and command and ids sanitised, for every kind of the research R12 matrix: buckets per region, private networks, subnets per network, cloud project users, S3 credentials per user, OAuth2 clients, IAM policies, identity groups, IAM resource tags on the project URN — and a paginated listing where the account has enough entries, else recorded as not observed. A failed listing is recorded as refuted for that kind with its fallback, not skipped.
  - Evidence: PR (commands, outcomes); `evidence/T009.md`; initial status `not-run`.

- [ ] T010 Owner session: run create→destroy probes P1–P3, P7–P9, P12–P15 and optional P25 from tests/live/probes/ through `lz-live probe`
  - Requirements: FR-004, FR-008, FR-010, FR-011; ADRs: 0008, 0009, 0018. Depends on: T009, T055.
  - Verify: Owner session per run sheet, each probe through `task live:probe` (deadline, inventory, destroy-on-exit, leftover check active): versioned probe bucket as S3 backend with `use_lockfile` and encryption — the second plan starts only after the first one's lock object is listed and is refused with a lock error; state object not plaintext; probe OAuth2 client and IAM policy created and usable as identity; the P9 allowlist creates and destroys a probe network, subnet and bucket and is denied an IAM write (each extra action needed is recorded with its denial); private network + subnet in GRA11; tags with `:` on bucket and project URN; optional P25 tag-conditioned policy on two probe buckets; destroy all; leftover check over the matrix shows no `lzprobe-` leftover. Record approximate cost.
  - Evidence: PR (run id, cost, observations); `evidence/T010.md`; initial status `not-run`.

## US1 — Names, labels and the first stage, offline (P1)

Independent test: V001, V002 on the US1 directories, V003.

- [ ] T011 [US1] Write naming tests in modules/naming/tests/{unit,contract}.tftest.hcl (supersedes 001/T013 for the default-template scope)
  - Requirements: FR-001, FR-002, SC-001; ADRs: 0003, 0008. Depends on: T004.
  - Verify: `task test:unit -- modules/naming`: against a compiling stub (variables and outputs only) concrete names for every kind in the default template and in one reordered test template, and the five mandatory labels, are behavioural red; an explicit name override (import) is passed through unchanged after validation and an invalid override is rejected; `expect_failures` cover over-limit, forbidden character, doubled bucket punctuation, empty segment, unknown kind and mandatory-key override; a label-only change keeps the name.
  - Evidence: PR; `evidence/T011.md`; initial status `not-run`.

- [ ] T012 [US1] Implement modules/naming/{main,variables,outputs,versions}.tf, kinds.yaml and README.md (supersedes 001/T014 for the default-template scope)
  - Requirements: FR-001, FR-002, SC-001; ADRs: 0003. Depends on: T011.
  - Verify: `task test:unit -- modules/naming; task lint -- modules/naming; task test:dependencies`: T011 controls green; provider-free module; each `kinds.yaml` row cites its source or is marked UNVERIFIED and refused for cloud kinds without a limit. Guard G4: a mutant letting extra labels override `lz:tenant` fails.
  - Evidence: PR; `evidence/T012.md`; initial status `not-run`.

- [ ] T013 [US1] Write object storage module tests in modules/{object-storage,object-storage-protected,object-storage-user}/tests/
  - Requirements: FR-002, FR-004, FR-013; ADRs: 0003, 0008, 0009. Depends on: T012.
  - Verify: `task test:unit -- modules/object-storage; task test:unit -- modules/object-storage-protected; task test:unit -- modules/object-storage-user`: with `mock_provider "ovh"` the bucket gets the given name, region, `versioning` when requested and exactly the given tags; the protected variant always enables versioning and carries a literal `prevent_destroy` (asserted by the lifecycle scan of T026); the S3 user has `objectstore_operator`, the policy JSON allows only the given bucket ARNs; the credential secret is only a sensitive output. Stubs are behavioural red.
  - Evidence: PR; `evidence/T013.md`; initial status `not-run`.

- [ ] T014 [US1] Implement modules/object-storage, modules/object-storage-protected and modules/object-storage-user
  - Requirements: FR-002, FR-004, FR-013; ADRs: 0003, 0009. Depends on: T013.
  - Verify: `task test:unit -- modules/object-storage; task test:unit -- modules/object-storage-protected; task test:unit -- modules/object-storage-user; task lint -- modules/object-storage; task lint -- modules/object-storage-protected; task lint -- modules/object-storage-user`: green; resources `ovh_cloud_project_storage`, `ovh_cloud_project_user`, `ovh_cloud_project_user_s3_credential`, `ovh_cloud_project_user_s3_policy` (provider docs cited in README); no attribute absent from 2.21.0 (P20).
  - Evidence: PR; `evidence/T014.md`; initial status `not-run`.

- [ ] T015 [US1] Write state-backend component and bootstrap stage tests in components/state-backend/tests/ and stages/bootstrap/tests/
  - Requirements: FR-002, FR-004, FR-005, FR-008; ADRs: 0004, 0009. Depends on: T006, T014.
  - Verify: `task test:unit -- components/state-backend; task test:unit -- stages/bootstrap`: the component creates one protected, versioned bucket and the S3 users it is given, each with a policy limited to that bucket; the bootstrap stage creates only the account bucket (name from naming: `org`, kind bucket, role state) in `state_project_id` plus the platform S3 user; mandatory labels; outputs `state_bucket`, `state_project_id`, `state_region`, `state_endpoint`, `platform_s3_user_id`, `unlabelled[]` (S3 user, credential, policy) and only-sensitive credentials; no tenant bucket; stage has no backend/provider configuration. Stubs are behavioural red.
  - Evidence: PR; `evidence/T015.md`; initial status `not-run`.

- [ ] T016 [US1] Implement components/state-backend and stages/bootstrap
  - Requirements: FR-002, FR-004, FR-005, FR-008; ADRs: 0004, 0009. Depends on: T015.
  - Verify: `task test:unit -- components/state-backend; task test:unit -- stages/bootstrap; task test:slice; task test:dependencies`: green; `test:slice` now discovers and passes every US1 directory.
  - Evidence: PR; `evidence/T016.md`; initial status `not-run`.

## US2 — Every in-scope stage planned offline with typed output contracts (P1)

Independent test: V002 for all slice directories, V003, V004.

- [ ] T017 [US2] Write output-contract tests in tools/internal/stacks/outputs_test.go and tests/fixtures/outputs/
  - Requirements: FR-005, SC-005; ADRs: 0004, 0017. Depends on: T016; 001/T003 capture admission.
  - Verify: `go -C tools test ./internal/stacks -run TestOutputs -count=1`: from a captured pinned `tofu output -json` of a provider-free fixture root (sensitive and plain outputs; command recorded) the envelope builder keeps plain values and drops sensitive ones; fixture envelopes per stage (bootstrap, tenant-state, account-governance, project, project-network, runtime) validate. Kept sensitive entry, secret-pattern key, unknown field, missing value, `null`/`""` capability and wrong `stage` are behavioural red against a pass-through stub.
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

- [ ] T021 [US2] Write tenant-state stage tests in stages/tenant-state/tests/ (replaces the withdrawn account-admin stage, D88)
  - Requirements: FR-002, FR-004, FR-008, FR-013; ADRs: 0004, 0009. Depends on: T016, T018.
  - Verify: `task test:unit -- stages/tenant-state`: through `components/state-backend`, one protected, versioned tenant bucket named from naming (`org`, tenant, kind bucket, role state) in `state_project_id`; a tenant S3 user and a platform S3 user, each with a policy covering only that bucket; mandatory labels with `lz:tenant`; outputs match the tenant-state schema, list `unlabelled[]`, secrets only sensitive. Guard G6: a mutant granting the tenant S3 user the account bucket, another tenant's bucket or `*` is red. Stubs are behavioural red.
  - Evidence: PR; `evidence/T021.md`; initial status `not-run`.

- [ ] T022 [US2] Implement stages/tenant-state
  - Requirements: FR-002, FR-004, FR-008, FR-013; ADRs: 0004, 0009. Depends on: T021.
  - Verify: `task test:unit -- stages/tenant-state; task test:unit -- components/state-backend; task test:dependencies`: T021 controls green with the G6 mutant killed; the stage calls only `components/state-backend`.
  - Evidence: PR; `evidence/T022.md`; initial status `not-run`.

- [ ] T023 [US2] Write identity/ovh-native component and account-governance stage tests in components/identity/ovh-native/tests/ and stages/account-governance/tests/
  - Requirements: FR-002, FR-004, FR-010, FR-013; ADRs: 0006, 0009, 0018. Depends on: T020, T018.
  - Verify: `task test:unit -- components/identity/ovh-native; task test:unit -- stages/account-governance`: platform deployer policy covers `publicCloudProject:apiovh:*` on the tenant project URNs; tenant deployer policy lists exactly the P9 allowlist (research R6) on its own project URN — no `account:apiovh:iam/` action, no `region/storage/policy/create`, no wildcard action; tenant group created with no members; no S3 user (moved to `tenant-state`); outputs match schema and list OAuth2 clients, policies and the group in `unlabelled[]`. Guard G5: a mutant widening the tenant resource to `*`, adding an IAM action, `region/storage/*` or `region/storage/policy/create` is red.
  - Evidence: PR; `evidence/T023.md`; initial status `not-run`.

- [ ] T024 [US2] Implement components/identity/ovh-native and stages/account-governance
  - Requirements: FR-002, FR-004, FR-010, FR-013; ADRs: 0006, 0009, 0018. Depends on: T023.
  - Verify: `task test:unit -- components/identity/ovh-native; task test:unit -- stages/account-governance; task test:dependencies`: T023 controls green and the G5 mutants red as specified.
  - Evidence: PR; `evidence/T024.md`; initial status `not-run`.

- [ ] T025 [US2] Write cloud-project and cloud-quota module tests in modules/{cloud-project,cloud-quota}/tests/
  - Requirements: FR-002, FR-004; ADRs: 0003, 0005, 0006. Depends on: T012. Premise waiver (research R23): P5 is unqualified until T009; both modes are tested offline, no cloud contact.
  - Verify: `task test:unit -- modules/cloud-project; task test:unit -- modules/cloud-quota`: `adopt` mode manages `ovh_cloud_project` with `prevent_destroy` and `deletion_protection = true`; `reference` mode reads `data.ovh_cloud_project` and manages no project; both tag the project URN through `ovh_iam_resource_tags`; alerting only when enabled; quota resource only when enabled with `prevent_automatic_quota_upgrade = true`. Stubs are behavioural red.
  - Evidence: PR; `evidence/T025.md`; initial status `not-run`.

- [ ] T026 [US2] Implement modules/cloud-project and modules/cloud-quota
  - Requirements: FR-002, FR-004; ADRs: 0003, 0005, 0006. Depends on: T025. Premise waiver as T025; the sandbox mode is fixed in T039 from T009.
  - Verify: `task test:unit -- modules/cloud-project; task test:unit -- modules/cloud-quota; task lint -- modules/cloud-project`: green. Guard G7 (code part): a mutant dropping `prevent_destroy` from `modules/cloud-project` or `modules/object-storage-protected` fails a test that inspects the configuration (static lifecycle scan via `lz-check deps` or a tftest expectation; whichever T025 chose, applied to both modules).
  - Evidence: PR; `evidence/T026.md`; initial status `not-run`.

- [ ] T027 [US2] Write project-factory component and project stage tests in components/project-factory/tests/ and stages/project/tests/
  - Requirements: FR-002, FR-004, FR-005; ADRs: 0004, 0005, 0006. Depends on: T026, T018. Premise waiver as T025.
  - Verify: `task test:unit -- components/project-factory; task test:unit -- stages/project`: tenant/environment labels from inputs; budget alert and quota toggles pass through; outputs `project_id`, `project_urn`, `regions` match the project schema. Stubs are behavioural red.
  - Evidence: PR; `evidence/T027.md`; initial status `not-run`.

- [ ] T028 [US2] Implement components/project-factory and stages/project
  - Requirements: FR-002, FR-004, FR-005; ADRs: 0004, 0005, 0006. Depends on: T027. Premise waiver as T025.
  - Verify: `task test:unit -- components/project-factory; task test:unit -- stages/project; task test:dependencies`: green.
  - Evidence: PR; `evidence/T028.md`; initial status `not-run`.

- [ ] T029 [US2] Write private-network module, network/island component and project-network stage tests in modules/private-network/tests/, components/network/island/tests/ and stages/project-network/tests/
  - Requirements: FR-002, FR-004, FR-005; ADRs: 0004, 0017. Depends on: T012, T018.
  - Verify: `task test:unit -- modules/private-network; task test:unit -- components/network/island; task test:unit -- stages/project-network`: one network in the given region, one subnet with the given CIDR, DHCP on, no gateway resource; invalid CIDR and region outside the project's regions are rejected; outputs match schema and list the network and subnet as `unlabelled`. Stubs are behavioural red.
  - Evidence: PR; `evidence/T029.md`; initial status `not-run`.

- [ ] T030 [US2] Implement modules/private-network, components/network/island and stages/project-network
  - Requirements: FR-004, FR-005; ADRs: 0004, 0017. Depends on: T029.
  - Verify: `task test:unit -- modules/private-network; task test:unit -- components/network/island; task test:unit -- stages/project-network; task test:dependencies`: green; resources `ovh_cloud_project_network_private`, `ovh_cloud_project_network_private_subnet` (docs cited).
  - Evidence: PR; `evidence/T030.md`; initial status `not-run`.

- [ ] T031 [US2] Write runtime/managed-only component and runtime stage tests in components/runtime/managed-only/tests/ and stages/runtime/tests/
  - Requirements: FR-004, FR-005; ADRs: 0004, 0017. Depends on: T014, T018.
  - Verify: `task test:unit -- components/runtime/managed-only; task test:unit -- stages/runtime`: one labelled bucket through the unprotected `modules/object-storage`; envelope `kind = managed-only`, optional `slot`, `scope`, `readiness`, `pending_actions = []`, `capabilities` with `object-storage` and without `network`; inputs consume only `project` outputs. A stub publishing an empty `network` capability is behavioural red.
  - Evidence: PR; `evidence/T031.md`; initial status `not-run`.

- [ ] T032 [US2] Implement components/runtime/managed-only and stages/runtime
  - Requirements: FR-004, FR-005, FR-013, SC-001; ADRs: 0004, 0017. Depends on: T031, T016, T022, T024, T028, T030.
  - Verify: `task test:unit -- components/runtime/managed-only; task test:unit -- stages/runtime; task test:slice; task test:dependencies; task test:outputs`: green; `test:slice` covers every module, component and stage of the slice with nonzero test counts (V002 checkpoint).
  - Evidence: PR; `evidence/T032.md`; initial status `not-run`.

## US3 — Stacks generated from `deployments.yaml`, planned offline, outputs exchanged (P2)

Independent test: V005, V006, V012.

- [ ] T033 [US3] Write manifest decoding tests in tools/internal/stacks/manifest_test.go and tests/fixtures/manifests/{sandbox,growth,tenant-only,invalid}/
  - Requirements: FR-006, FR-008, SC-002; ADRs: 0004, 0005, 0007, 0017. Depends on: T002.
  - Verify: `go -C tools test ./internal/stacks -run TestManifest -count=1`: sandbox, growth (2 tenants × 2 environments × 2 regions, two runtime slots in one scope) and tenant-only (`spec.scope: tenant`, `external` producers) manifests decode with derived paths, state buckets, state keys, tags and edges (data and authority) matching independently written expectations. Each invalid case of V005 — unknown field, duplicate key, duplicate id, scope violations, `slot` on a non-runtime stage, two runtimes in one scope without distinct slots, unsupported version, `account-fabric` or `account-admin` row, a tenant row without its `tenant-state`, account rows in a tenant-only manifest, a consumed producer neither row nor `external`, `spec.state.project` equal to a tenant project without `sandbox.shared_state_project` — is behavioural red against a permissive stub.
  - Evidence: PR; `evidence/T033.md`; initial status `not-run`.

- [ ] T034 [US3] Implement schemas/deployments.schema.json and tools/internal/stacks/{manifest,stages}.go
  - Requirements: FR-006, FR-008, SC-002; ADRs: 0004, 0005, 0007, 0017. Depends on: T033.
  - Verify: `task test:stacks`: T033 controls green; strict decoding reuses `checks.DecodeStrict`; stage table matches data-model.md (including the reserved `account-admin` and the account-tenant scope). Creates `test:stacks` (extended by T036, T038, T041).
  - Evidence: PR; `evidence/T034.md`; initial status `not-run`.

- [ ] T035 [US3] Write reconciler tests in tools/internal/stacks/reconcile_test.go using tests/fixtures/terramate/
  - Requirements: FR-007; ADRs: 0004, 0007. Depends on: T034, T007.
  - Verify: `go -C tools test ./internal/stacks -run TestReconcile -count=1`: on a scratch tree a missing stack is created through the pinned `terramate create` with the derived id, tags and `after`; a repeat run is a no-op; a directory without a row, a changed id and a changed dimension yield `UNSUPPORTED_CHANGE`; `account-fabric` and `account-admin` yield `STAGE_NOT_IMPLEMENTED`; `--check` reports without writing. A stub that only creates is behavioural red.
  - Evidence: PR; `evidence/T035.md`; initial status `not-run`.

- [ ] T036 [US3] Implement the reconciler in tools/internal/stacks/reconcile.go, tools/cmd/lz-stacks/ and Taskfile.yml
  - Requirements: FR-007; ADRs: 0004, 0007. Depends on: T035.
  - Verify: `task test:stacks; task stacks:check`: T035 controls green; `stacks:reconcile` (host) and `stacks:check` (offline, scratch copy) created; with no manifest yet `stacks:check` reports `fail: no manifest` (header exception, expected until T039).
  - Evidence: PR; `evidence/T036.md`; initial status `not-run`.

- [ ] T037 [US3] Write generation tests in tools/internal/stacks/generate_test.go and tests/fixtures/manifests/{sandbox,tenant-only}/expected/
  - Requirements: FR-002, FR-007, FR-008; ADRs: 0004, 0007, 0009. Depends on: T036, T032. Premise waiver (research R23): backend options follow P2 as documented; T038 closes only on T010's result.
  - Verify: `go -C tools test ./internal/stacks -run TestGenerate -count=1`: generating the sandbox fixture into scratch with pinned Terramate yields, per stage kind, files equal to the hand-written expectations: S3 backend with derived bucket (account bucket for account and account-tenant stacks, tenant bucket otherwise) and key, `use_lockfile = true`, OVH endpoint flags and `encryption {}` (enforced for state and plan); local backend path variable under `accounts/<account>/state/` for `bootstrap` only; provider block without credentials; the single stage call with its source derived from `spec.stage_source`; one typed object variable per consumed producer stage; labels `managed-in`/`instance`/`tenant`; import block only for adopt; offline test file with `mock_provider`. The tenant-only fixture generates into a separate scratch repository whose stage source is a relative path outside it, and no account stack is generated. Missing `encryption`, an import on a non-adopt project, a `git` stage source (`STAGE_SOURCE_NOT_IMPLEMENTED` expected) rendered silently and any `terraform_remote_state` are behavioural red.
  - Evidence: PR; `evidence/T037.md`; initial status `not-run`.

- [ ] T038 [US3] Implement Terramate configuration in terramate.tm.hcl and stacks/_lz/*.tm.hcl with `task stacks:generate` and `task test:stack-plans`
  - Requirements: FR-002, FR-007, FR-008, FR-013; ADRs: 0004, 0007, 0009. Depends on: T037; T010 result for P1–P3 (a refutation revises FR-008, V005 and V010 before this task closes, research R5).
  - Verify: `task test:stacks; task test:stack-plans`: T037 controls green; the growth and tenant-only fixtures generated into scratch plan every stack under mocks with fixture inputs (SC-002); `test:stack-plans` reports zero discovery as `fail` until T039 (header exception).
  - Evidence: PR; `evidence/T038.md`; initial status `not-run`.

- [ ] T039 [US3] Add stacks/deployments.yaml for demo/dev/GRA11 and the generated stacks under stacks/
  - Requirements: FR-006, FR-007, FR-008, SC-002; ADRs: 0004, 0007. Depends on: T038, T022; T009 result for the project mode (adopt unless P5 was refuted, then `reference`); T010 result for the backend.
  - Verify: `task stacks:check; task test:stack-plans; task test:dependencies; task lint -- stacks/tenants/demo/dev/gra11/runtime`: six stacks (`account-bootstrap`, `account-governance`, `demo-state`, `demo-dev-project`, network, runtime), `spec.sandbox.shared_state_project: true` with a comment pointing at KD-1, generated files fresh, every stack plans offline, all directories classified as generated instances; editing one generated file makes `stacks:check` fail (control, reverted).
  - Evidence: PR; `evidence/T039.md`; initial status `not-run`.

- [ ] T060 [US3] Write output-exchange integration tests in tools/internal/stacks/adapter_test.go and tools/internal/live/publish_test.go with generated fixture roots
  - Requirements: FR-005, FR-009, FR-013; ADRs: 0004, 0007, 0017. Depends on: T038, T018.
  - Verify: `go -C tools test -tags offlinetools ./internal/stacks ./internal/live -run "TestAdapter|TestPublish|TestExchange" -count=1`: offline, on roots generated from the sandbox fixture under mocks, `project` applies, its `tofu output -json` becomes an envelope, the publisher writes it to a scratch bucket directory at `artifacts/<id>/outputs.json`, the adapter turns it into `project.tfvars.json`, and `runtime` plans with it; `bootstrap` (local state) publishes to the account bucket path; the consumer's record holds the consumed digest. A missing artefact (`blocked`), a malformed or schema-invalid one, one whose `instance_id` or `stage` belongs to another producer, and a sensitive value reaching the tfvars file are each behavioural red against a pass-through stub.
  - Evidence: PR; `evidence/T060.md`; initial status `not-run`.

- [ ] T061 [US3] Implement the publisher and the envelope-to-input adapter in tools/internal/live/publish.go, tools/internal/stacks/adapter.go and the `test:exchange` Taskfile target
  - Requirements: FR-005, FR-009, FR-013; ADRs: 0004, 0007, 0017. Depends on: T060.
  - Verify: `task test:exchange`: T060 controls green; G2 (exchange part): a mutant copying a sensitive output into the tfvars file is red. Creates `test:exchange`.
  - Evidence: PR; `evidence/T061.md`; initial status `not-run`.

- [ ] T040 [US3] Write order, selection and lock tests in tools/internal/stacks/{selection,lock}_test.go
  - Requirements: FR-009, FR-013; ADRs: 0004, 0007. Depends on: T034, T007.
  - Verify: `go -C tools test ./internal/stacks -run "TestSelection|TestLock" -count=1`: order equals the topological order of derived data and authority edges and the captured `terramate list --run-order`; selection per research R21: an upstream-only code change selects the producer and its transitive data consumers; an intermediate-only change selects it and its consumers but not its producers; a changed producer digest selects its data consumers; an authority-only edge orders but does not select; a missing record selects; a producer without an artefact makes its consumer `blocked`; an unrelated tenant is excluded. Guard G14: with the first lock held (the test acquires it and signals before starting the second holder), a second run for the same tenant, and a second run touching account stacks, are refused; two tenants' runs that both touch account stacks interleave without one destroying or revoking anything of the other. An `after`-only stub and a stub without locks are behavioural red.
  - Evidence: PR; `evidence/T040.md`; initial status `not-run`.

- [ ] T041 [US3] Implement selection and the account and tenant locks in tools/internal/stacks/{selection,lock}.go with `task stacks:order`
  - Requirements: FR-009, FR-013; ADRs: 0004, 0007. Depends on: T040, T039.
  - Verify: `task test:stacks; task stacks:order -- all`: T040 controls green with the G14 mutant (skip the account lock) killed; prints the sandbox order `account-bootstrap → {account-governance, demo-state} → demo-dev-project → {network, runtime}` with the reason for each selected stack.
  - Evidence: PR; `evidence/T041.md`; initial status `not-run`.

## US4 — Scripted, re-runnable account bootstrap (P2)

Independent test: V008 offline; V009 owner session.

- [ ] T042 [US4] Write bootstrap identify, passphrase, admin and revoke phase tests in tools/internal/live/bootstrap_admin_test.go with a fake OVH API and fake terminal in tools/internal/live/testdata/
  - Requirements: FR-010, FR-012, SC-005; ADRs: 0009, 0018. Depends on: T053.
  - Verify: `go -C tools test ./internal/live -run "TestBootstrapAdmin" -count=1`: phases run in the order `guard, identify, passphrase, admin` and the passphrase exists before any phase that writes encrypted state; on the current-sandbox fixture `admin` reports `unchanged` only when the credential works **and** the admin client and policy exist as expected, `fail` naming the difference on a drifted policy, `blocked` when the credential is rejected without `--fresh-account`; with `--fresh-account` the root keys are read from the fake terminal with echo off, the client and policy are created through the fake API, `sandbox.env` is written once, and `revoke` deletes the current credential; a previous account's `sandbox.env` moves to `accounts/<old>/sandbox.env` and none of its files is read afterwards. Guards G2, G3, G11, G13: root keys appearing in argv, a child environment, a file, stdout or stderr; a 0644 or in-repo credential file; an overwritten passphrase; a mismatched binding accepted are each behavioural red against a stub.
  - Evidence: PR; `evidence/T042.md`; initial status `not-run`.

- [ ] T043 [US4] Implement `lz-live bootstrap` identify, passphrase, admin and revoke in tools/internal/live/{bootstrap,rootkeys,ovhapi,redact}.go and the `bootstrap:account` Taskfile target
  - Requirements: FR-010, FR-012, SC-005; ADRs: 0009, 0018. Depends on: T042.
  - Verify: `task test:bootstrap`: T042 controls green with each named guard's mutant killed; the root-key API client signs requests in process (research R19). Creates `test:bootstrap` (extended by T057) and the host target `bootstrap:account`.
  - Evidence: PR; `evidence/T043.md`; initial status `not-run`.

- [ ] T056 [US4] Write bootstrap state, publish and verify phase tests and the partial-state matrix in tools/internal/live/bootstrap_state_test.go with fake tofu and ovhcloud binaries
  - Requirements: FR-008, FR-010, FR-012, SC-005; ADRs: 0009, 0018. Depends on: T043, T061.
  - Verify: `go -C tools test ./internal/live -run "TestBootstrapState|TestBootstrapPartial" -count=1`: empty fake account runs every phase in order; a second run reports each phase `unchanged`; each partial state of research R13 (each prefix of phases done; credential rejected; admin drifted; passphrase present but `state.env` missing; `state.env` present but bucket missing; bucket present but not in state → imported by name; binding for another account) runs only the missing phases or refuses as specified; `publish` writes the bootstrap envelope to the account bucket; `verify` plans `account-governance` and round-trips the lock; `state.env` is written only through `files.go`; bootstrap writes no deployer file. A bucket name taken by another account fails with the message naming `spec.org` (V008 negative). Guards G2, G3: a seeded secret in argv, a generated file, stdout or stderr, and a credential file inside the repository are behavioural red against a stub.
  - Evidence: PR; `evidence/T056.md`; initial status `not-run`.

- [ ] T057 [US4] Implement the bootstrap state, publish and verify phases in tools/internal/live/bootstrap_state.go
  - Requirements: FR-008, FR-010, FR-012, SC-005; ADRs: 0009, 0018. Depends on: T056.
  - Verify: `task test:bootstrap`: T042 and T056 controls green with each named guard's mutant killed.
  - Evidence: PR; `evidence/T057.md`; initial status `not-run`.

- [ ] T044 [US4] Owner session: bootstrap the current sandbox account twice with `task bootstrap:account`
  - Requirements: FR-008, FR-012, SC-004; ADRs: 0009. Depends on: T057, T010, T039.
  - Verify: Owner session with `--reviewed-sha`: first run binds the account (`account.env`), creates the passphrase file, reports `admin` `unchanged` (existing `lz-sandbox-admin` works and matches; no import, no new client), creates the account state bucket, `state.env` and the bootstrap artefact; the owner retires the one-off config's ownership of the admin client and policy with the printed `tofu state rm` commands (research R13); second run reports every phase `unchanged` and a no-change plan; the state object in the bucket is not plaintext JSON; nothing secret on the terminal.
  - Evidence: PR (run id, approximate cost); `evidence/T044.md`; initial status `not-run`.

- [ ] T045 [US4] Owner session: bootstrap a fresh OVHcloud account with `task bootstrap:account -- --fresh-account`
  - Requirements: FR-010, FR-012, SC-004; ADRs: 0009. Depends on: T044; a fresh account with one project (P22); blocked until one exists.
  - Verify: Owner session per quickstart *Fresh account*, with the previous account's files still present: root keys typed at the prompt, admin client and policy created (P23), previous `sandbox.env` moved into its account directory, root credential revoked, state backend working; a second run is `unchanged`. Absent fresh account → `blocked`, never pass.
  - Evidence: PR (run id, approximate cost); `evidence/T045.md`; initial status `not-run`.

## US5 — Live chain apply→destroy in the sandbox (Owner session, P3)

Independent test: V007 offline; V010 owner session.

- [ ] T058 [US5] Write live plan/apply tests (authority selection, retained-resource refusal, selection integration) in tools/internal/live/{apply,credentials,protect}_test.go
  - Requirements: FR-009, FR-010, FR-011, SC-005; ADRs: 0004, 0008, 0009. Depends on: T053, T041, T061.
  - Verify: `go -C tools test ./internal/live -run "TestApply|TestCredentials|TestProtect" -count=1`: `plan|apply -- all` acts on the selected set in order with each stack's authority; deployer credential files are written by `files.go` after `account-governance` and `tenant-state` applies; a consumer without its producer's artefact reports `blocked` (exit 2). From recorded `tofu show -json` plan fixtures, a delete or replace of any resource of a retained instance is refused before apply — fixtures: an `org` change renaming the state bucket, a tenant removed from `tenant-state`'s inputs, a resource block removed from configuration, a replaced project — and `destroy` of `account-bootstrap`, `demo-state`, `account-governance`, `demo-dev-project` or `all` is refused. Guards G1, G2, G7: platform credentials for a tenant stack, a seeded secret in the rendered plan file or a stream, a skipped plan-level refusal and a retained instance accepted by `destroy` are each behavioural red against stubs.
  - Evidence: PR; `evidence/T058.md`; initial status `not-run`.

- [ ] T059 [US5] Implement `lz-live plan|apply` in tools/internal/live/{apply,credentials,protect}.go and the `live:plan` and `live:apply` Taskfile targets
  - Requirements: FR-009, FR-010, FR-011, SC-005; ADRs: 0004, 0008, 0009. Depends on: T058.
  - Verify: `task test:live-lane`: T058 controls green with the G1, G2, G7 mutants killed. Creates host targets `live:plan`, `live:apply`.
  - Evidence: PR; `evidence/T059.md`; initial status `not-run`.

- [ ] T046 [US5] Write chain and destroy tests in tools/internal/live/chain_test.go using fakes and the captured listings in tests/fixtures/ovhcloud/
  - Requirements: FR-009, FR-010, FR-011, SC-005; ADRs: 0004, 0008, 0009, 0024. Depends on: T059, T055, T009.
  - Verify: `go -C tools test ./internal/live -run "TestChain|TestLeftoversCaptured" -count=1`: fake chain takes the account and tenant locks, applies six stacks in order, publishes envelopes, destroys `runtime` and `project-network` in reverse order and retains `account-bootstrap`, `demo-state`, `account-governance`, `demo-dev-project`; inventory grows per resource; the leftover parser accepts T009's captured listings for every kind (replacing the synthetic ones of T054) and finds one seeded leftover per kind. Guards G7, G8, G9: destroying a retained instance, no destroy after an apply failure/SIGINT/SIGTERM/deadline, forward destroy order, and a captured listing error reported as pass are each behavioural red.
  - Evidence: PR; `evidence/T046.md`; initial status `not-run`.

- [ ] T047 [US5] Implement `lz-live destroy|chain` in tools/cmd/lz-live/ and tools/internal/live/chain.go, the `live:destroy` and `live:chain` Taskfile targets and mise.live.toml
  - Requirements: FR-009, FR-010, FR-011, SC-005; ADRs: 0004, 0008, 0009, 0024. Depends on: T046.
  - Verify: `task test:live-lane`: T046 controls green with each named guard's mutant killed; `ovhcloud` 0.15.0 pinned in `mise.live.toml` only; `task verify:toolchain` unchanged. Creates host targets `live:destroy`, `live:chain`.
  - Evidence: PR; `evidence/T047.md`; initial status `not-run`.

- [ ] T048 [US5] Write L7 chain assertion and collector tests in tools/internal/probes/live/chain/chain_test.go and tools/internal/live/observe_test.go with observation fixtures in tests/live/chain/
  - Requirements: FR-004, FR-008, FR-010, FR-011, SC-003; ADRs: 0006, 0008, 0009. Depends on: T047.
  - Verify: `go -C tools test -tags live ./internal/probes/live/chain -run TestChainObservations -count=1; go -C tools test ./internal/live -run TestObserve -count=1` against recorded fake observations (the live run supplies real ones): versioned state buckets, encrypted state object, lock contention refused (second writer started after the lock object is listed), mandatory tags on bucket and project URN, tenant IAM write denied, tenant S3 read of the account bucket or another tenant's bucket denied, `outputs.json` schema-valid, and the KD-1 canary result. The collector creates the canary bucket with the bootstrap authority, calls the management API as the tenant deployer, and registers the canary for the trap. Guard G15: an observed canary deletion with `shared_state_project: true` reported as `pass`, the same observation without the flag reported as anything but `fail`, and a missing canary result are each behavioural red. An observation set missing any assertion is behavioural red; the `live` tag keeps them out of offline discovery.
  - Evidence: PR; `evidence/T048.md`; initial status `not-run`.

- [ ] T062 [US5] Implement the L7 observation collector and assertions in tools/internal/live/observe.go and tools/internal/probes/live/chain/
  - Requirements: FR-004, FR-008, FR-010, FR-011, SC-003; ADRs: 0006, 0008, 0009. Depends on: T048.
  - Verify: `task test:live-lane; go -C tools test -tags live ./internal/probes/live/chain -run TestChainObservations -count=1`: T048 controls green with the G15 mutants killed; `summary.json` and the final output line list known deviations observed; the chain exit code is unchanged by a `known-deviation`.
  - Evidence: PR; `evidence/T062.md`; initial status `not-run`.

- [ ] T049 [US5] Owner session: run `task live:chain -- all` in the sandbox
  - Requirements: FR-004, FR-008, FR-009, FR-010, FR-011, SC-003; ADRs: 0004, 0008, 0009, 0024. Depends on: T062, T044, T039.
  - Verify: Owner session with `--reviewed-sha`: six stacks applied in order with the right authority each; T048 assertions pass on real observations, with KD-1 reported as `known-deviation` (or `pass` with the note if not reproduced); ephemeral destroy completes from the trap; leftover check over the full kind matrix zero; total within the deadline (< 60 min); run id, approximate cost and the KD-1 result recorded. A failed assertion still destroys and reports `fail`.
  - Evidence: PR (run id, cost, leftover result, known deviations); `evidence/T049.md`; initial status `not-run`.

## Polish and exit

- [ ] T050 Write the how-to in docs/how-to/run-the-first-slice.md and link it from README.md
  - Requirements: FR-011, FR-012; ADRs: 0013. Depends on: T047, T057.
  - Verify: exempt — docs-only; content review against the implemented commands, the owner-only boundary, KD-1 and the postponed list.
  - Evidence: PR; `evidence/T050.md`; docs content review only.

- [ ] T051 Run the slice exit checks V001–V012 through harness/checks.yaml
  - Requirements: FR-001, FR-002, FR-003, FR-004, FR-005, FR-006, FR-007, FR-008, FR-009, FR-010, FR-011, FR-012, FR-013, FR-014, SC-001, SC-002, SC-003, SC-004, SC-005; ADRs: 0002, 0003, 0004, 0007, 0008, 0009, 0018. Depends on: T049, T050; T045 result (pass or `blocked`).
  - Verify: every offline V-command green with discovery counts; V009/V010 PR records present; V009's fresh-account part may be `blocked` (then SC-004 is partial and the spec cannot be `done`); V010 carries KD-1 as a recorded known deviation, and the exit record states that tenant state isolation is not demonstrated.
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
| `task test:exchange` | T061 | offline |
| `task stacks:order -- <instance\|all>` | T041 | offline |
| `task test:bootstrap` | T043, extended T057 | offline |
| `task bootstrap:account [-- --fresh-account]` | T043 | host, owner |
| `task test:live-lane` | T055, extended T059, T047, T062 | offline |
| `task live:probe -- <probe> [--plan-only]` | T055 | host, owner |
| `task live:plan\|apply -- <instance\|all>` | T059 | host, owner |
| `task live:destroy\|chain -- <instance\|all>` | T047 | host, owner |
| `lz-stacks`, `lz-live` binaries | T036, T053 (extended T055, T043, T059, T047) | — |

## Dependencies and execution order
Per-task `Depends on` fields are authoritative and form a DAG.
- Setup T001–T006 first; T007, T008 and T052 can start in parallel with US1.
- Live safety core T052→T053→T054→T055 (T054 needs T007's P24 capture) before any owner probe.
- Probes: T008 + T055 → T009 → T010.
- US1 T011→T016; US2 T017–T032 (IAM, tenant-state, project, network, runtime branches parallel after
  T018/T012); US3 T033→T039 (needs T007, T032; T038 needs T010's result, T039 T009's and T010's),
  then T060→T061 and T040→T041; US4 T042→T043→T056→T057→T044→T045; US5 T058→T059→T046→T047→T048→T062→T049.
- Owner sessions: T009 (read-only, after the run core — unblocks T039's project mode and T046's
  captured listings), T010, T044, T045, T049. Constitution 1.3.0 (D87) makes cost guards hygiene,
  so no owner task waits on a cost disposition; the run core (T052–T055) is tested before the first
  credential load and the chain cleanup (T046–T047) before the first live chain.

## Parallel opportunities
After T012: T013/T019/T025 in parallel (distinct modules). After T018: T021, T023, T027, T029, T031
test tasks in parallel. T033 can start after T002; T052 after 001/T009. No parallel flag bypasses an
owner gate.

## Completion
Rerun the V-commands and read their output; a missing binary, zero discovery or a simulation never
closes a task. Owner tasks close only with the PR record. A refuted premise records its fallback; an
unrun owner task stays `not-run` or `blocked`.
