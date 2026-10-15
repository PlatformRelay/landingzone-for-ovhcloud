# Tasks: 006-harden-first-slice
Status: planned · Created: 2026-10-15 · Revised: 2026-10-15 (spec-set review round 1; D115; test and implementation tasks split) · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md

All boxes are open. New targets (`test:update-lane`, `test:release`) are **planned** until their
creator closes.

**Test tasks and implementation tasks.** Every behaviour story lists its test tasks before its
implementation tasks. A test task names its cases as given / when / then, each with the defect it
would expose; a fresh session writes the tests from spec.md, data-model.md's *Fixed signatures* and
the task text (not from plan.md's rationale), against a stub whose body is "not implemented" where
the symbol is new, and closes on a verified red: each new test fails for its stated reason. A
compile error, a missing symbol or a missing binary is not that red. Positive controls (the valid
case) and pins of behaviour that already holds pass from the start; each is marked as such in its
case, and every other case must fail for its stated reason. The tests a test task writes
are fixed points for the implementation task that follows; that task closes when they pass.

**Red windows.** A story's tests stay red on their target until the story's last implementation
task closes. An implementation task closes when the rows of its own test task are green and the only
red rows on the target belong to later implementation tasks of the same story; its evidence names
them. A story starts its test tasks only after the previous story sharing its target has closed its
implementation tasks (US1 → US2 on `test:live-lane`). The update-lane tests (`tools/internal/checks`)
and the docs tests (`tools/cmd/lz-check`) also run under `test:traceability`, so that target is red
during the US5 and US6 windows; nothing is pushed inside a red window.

**Commands.** The entry admits only `lz-offline --candidate <checkout> -- task <target> [-- <path>]`,
so every offline Verify names a Task target that exists, or that the same task or an earlier one
creates, and runs it through the entry (`<approved-absolute-path>/lz-offline --candidate <checkout>
-- task <target>`). Two host steps run on the workstation outside the entry and need no credential:
the `hostlimits` test (T029, T032) and the peak measurement (T032). **No task in this spec needs the
owner, a credential, the network or an OVHcloud call** (SC-002). The owner choices behind T010,
T016, T031, T034–T038 are decided (D115, 2026-10-15). Mutation proof is required where a Verify
names a guard G16–G23 (`contracts/checks.md`).
Evidence: the Verify output is summarised in the PR description; `evidence/<TID>.md` in this
directory points at the PR and names the red and green commits.

## Phase 1: Setup

- [x] T001 Write the Taskfile spec-tracing test `TestTaskfileTracesEverySpec` in tools/cmd/lz-check/main_test.go — closed 2026-10-15, evidence: evidence/T001.md
  - Requirements: FR-022; ADRs: 0008, 0019. Depends on: none.
  - Cases (given / when / then → defect exposed):
    - given today's Taskfile.yml / when the test parses the `check:specs` default `SPEC_DIRS` / then every spec directory owning registry keys (`NNN/…`, or spec 001's unprefixed keys) is listed → exposes spec 006 missing from the default trace.
    - given today's Taskfile.yml / when it parses the spec arguments of the `ci-source` command in `test:foundation-ci` / then the same set is listed → exposes CI admitting a 006 target without reading spec 006.
    - given the registry / when it reads the `test:foundation-ci` inputs / then they include the `tasks.md` of every spec `ci-source` reads → exposes a stale digest when 005's or 006's task boxes change.
  - Verify: `task test:traceability`: the three cases fail on today's tree for the stated reasons.
  - Evidence: `evidence/T001.md`; PR; initial status `not-run`.

- [x] T002 Trace spec 006 by default in Taskfile.yml and add the 005 and 006 tasks.md to the `test:foundation-ci` inputs in tools/harness/checks.yaml — closed 2026-10-15, evidence: evidence/T002.md
  - Requirements: FR-022; ADRs: 0008, 0019. Depends on: T001.
  - Verify: `task test:traceability; task test:foundation-ci; task check:specs`: T001's cases green; `task check:specs` with no argument prints `TRACE_OK` for 001, 005 and 006; `ci-source` reads spec 006.
  - Evidence: `evidence/T002.md`; PR; initial status `not-run`.

- [x] T003 Write the Go-dependency registry test `TestRegistryInputsCoverGoDeps` in tools/cmd/lz-check/main_test.go — closed 2026-10-15, evidence: evidence/T003.md
  - Requirements: FR-012; ADRs: 0008, 0019. Depends on: T002.
  - Cases (given / when / then → defect exposed):
    - given each registered check whose Taskfile command runs Go / when the test runs `go list -deps` on the packages it names (premise P1, observed through the entry and recorded) / then every in-module directory is covered by the check's scope or inputs → exposes `test:stacks` missing `tools/internal/report` and other unread packages.
    - given `test:traceability` / when its inputs are read / then they name `tools/harness/checks.yaml` → exposes registry edits not refreshing that check.
    - given a registered check without a Taskfile target and with an open creator (`test:update-lane`, `test:release`) / when the test runs / then it is listed `planned` and skipped → exposes a false failure on planned checks.
    - given a check whose creator is ticked and whose target is missing / when the test runs / then it fails naming the check → exposes a closed creator that never created its target.
  - Verify: `task test:traceability`: the first two cases fail on today's registry; the third passes and the fourth fails against a fixture registry. If P1 is refuted, the fallback of research R11 is recorded.
  - Evidence: `evidence/T003.md`; PR; initial status `not-run`.

- [x] T004 Add the missing Go reads to tools/harness/checks.yaml — closed 2026-10-15, evidence: evidence/T004.md
  - Requirements: FR-012; ADRs: 0008, 0019. Depends on: T003.
  - Verify: `task test:traceability`: T003's cases green; removing any added input turns its row red again.
  - Evidence: `evidence/T004.md`; PR; initial status `not-run`.

- [x] T047 Pin the task lists `test:traceability`'s judge reads in its registry inputs, test first in tools/cmd/lz-check/main_test.go (found in T004's review) — closed 2026-10-15, evidence: evidence/T047.md
  - Requirements: FR-012; ADRs: 0008, 0019. Depends on: T004.
  - Cases (given / when / then → defect exposed):
    - given the registry / when the test lists the files the traceability judge reads (every `specs/*/tasks.md` it opens, `main_test.go` near line 1025) / then each is in `test:traceability`'s inputs → exposes a ticked task box that leaves the check's evidence digest unchanged.
  - Verify: `task test:traceability`: the case fails before the inputs are added and passes after; dropping one added input turns its row red.
  - Evidence: `evidence/T047.md`; PR; initial status `not-run`.

## Phase 2: User Story 1 — Secrets stay out of live-lane errors and records (P1)
Independent test: V001. Test tasks T005–T010, then implementation tasks T011–T016.

- [x] T005 [US1] Write quiet-binding-error tests in tools/internal/live/hardening_test.go — closed 2026-10-15, evidence: evidence/T005.md
  - Requirements: FR-003; ADRs: 0009, 0021. Depends on: T004.
  - Cases (given / when / then → defect exposed):
    - given a fake token endpoint answering 302 with an unparseable `Location` that contains the client secret / when `Bind` runs / then its error and `lz-live`'s stderr contain neither the secret nor the URL and name the call and the status class → exposes the `%w` transport path (`binding.go:54`), since Go's client quotes an unparseable `Location`.
    - given a fake `GET /auth/details` doing the same / when `Bind` runs / then the same holds → exposes the same path on the second call.
    - given a fake answering 200 with a body that fails to decode and contains the secret / when `Bind` runs / then the error does not contain it → exposes the `%w` decode path (`binding.go:63`).
    - given a well-behaved fake / when `Bind` runs / then it binds → exposes an over-eager fix that breaks binding.
  - Verify: `task test:live-lane`: the first three cases fail on today's code for the stated reasons; the fourth passes.
  - Evidence: `evidence/T005.md`; PR; initial status `not-run`.

- [x] T006 [US1] Write leftover-token redaction tests in tools/internal/live/chain_test.go — closed 2026-10-15, evidence: evidence/T006.md
  - Requirements: FR-004; ADRs: 0009, 0021. Depends on: T004.
  - Cases (given / when / then → defect exposed):
    - given a fake listing that returns the bearer token as a resource id / when the chain's leftover check runs / then no file of the run record (`listings/*.json`, `leftovers.json`, logs) and no captured stream contains it, and the listing is recorded with the id redacted → exposes the token never reaching the redactor.
    - given the token inside a longer id / when the same runs / then the token part is redacted → exposes an exact-match-only fix.
    - given the token split across two writes of a stream / when the same runs / then neither write shows it → exposes registration after the stream started.
    - given a `LeftoverCheck` with `Red` set / when the first listing is requested / then `Red` already holds the token → exposes registration after the first listing.
  - Verify: `task test:live-lane`: all four cases fail on today's code.
  - Evidence: `evidence/T006.md`; PR; initial status `not-run`.

- [x] T007 [US1] Write credential-set writer tests in tools/internal/live/files_test.go — closed 2026-10-15, evidence: evidence/T007.md
  - Requirements: FR-005; ADRs: 0009. Depends on: T004.
  - Cases (given / when / then → defect exposed), per data-model *Credential set*:
    - given an empty account directory / when `WriteCredentialSet` writes the `account-governance` set / then `sets/<instance>/gen-<run_id>/` holds every file at mode 600 and `current` names it → exposes a writer that skips the generation or the pointer.
    - given a previous set / when the writer stops after `SetStepFirstFile` / then a fresh reader loads the previous set whole and the next writer removes the partial generation → exposes files written into live paths.
    - same after `SetStepFiles` and after `SetStepDirSync` → exposes a switch before the generation is durable.
    - same after `SetStepPointerTmp` (just before the rename) → exposes a pointer replaced in place.
    - given the writer stops after `SetStepSwitch` / when a fresh reader loads / then it gets the new set whole → exposes a reader that still prefers the old set.
    - given three earlier generations / when a write completes / then only the current and the previous remain → exposes unbounded growth or removal of the previous generation before the switch.
  - Verify: `task test:live-lane`: every case fails against the stub `WriteCredentialSet`.
  - Evidence: `evidence/T007.md`; PR; initial status `not-run`.

- [x] T008 [US1] Write credential-set reader and migration tests in tools/internal/live/credentials_test.go — closed 2026-10-15, evidence: evidence/T008.md
  - Requirements: FR-005; ADRs: 0009. Depends on: T004.
  - Cases (given / when / then → defect exposed):
    - given `current` naming a missing directory, `../x`, or a name not matching `gen-…` / when `CredentialSetPath` resolves / then it refuses with `CREDENTIAL_POINTER_INVALID` → exposes a pointer that escapes the set directory.
    - given today's flat files and no `current` / when `LoadCredentials` runs / then it reads the flat files → exposes a broken migration.
    - given flat files and a `current` / when `LoadCredentials` runs / then it reads the generation → exposes flat files winning after the switch.
    - given a set write after a `tenant-state` apply / when `LoadCredentials` loads the tenant's state credentials / then they come from that tenant's set → exposes one shared pointer for all writers.
    - given any write / when it completes / then `account.env`, the passphrase, `sandbox.env` and the bootstrap `state.env` are byte-identical → exposes a writer moving the binding.
  - Verify: `task test:live-lane`: every case except the flat-file read fails against the stub.
  - Evidence: `evidence/T008.md`; PR; initial status `not-run`.

- [x] T009 [US1] Write scratch-home tests in tools/internal/live/env_test.go and tools/internal/live/bootstrap_state_test.go — closed 2026-10-15, evidence: evidence/T009.md
  - Requirements: FR-007; ADRs: 0009, 0021. Depends on: T004.
  - Cases (given / when / then → defect exposed):
    - given a sentinel `HOME` in the test process / when `Command` builds a child with an explicit home / then the child's `HOME` is that home → exposes `os.Getenv("HOME")` in `env.go:26-29`.
    - given the sentinel / when the bootstrap phases `state`, `publish` and `verify` start their children / then none sees the sentinel → exposes an unpinned bootstrap path.
    - given the sentinel / when probe, plan, apply, destroy and chain start children / then none sees it → exposes a regression on paths fixed today.
  - Verify: `task test:bootstrap; task test:live-lane`: the first case fails against the stub of the new `Command` signature; the other two are pins of behaviour that holds today.
  - Evidence: `evidence/T009.md`; PR; initial status `not-run`.

- [x] T010 [US1] Write companion allow-list tests in tools/internal/live/probe_identity_test.go — closed 2026-10-15, evidence: evidence/T010.md
  - Requirements: FR-006; ADRs: 0009, 0021. Depends on: T004.
  - Cases (given / when / then → defect exposed):
    - given `TF_VAR_x`, a complete OAuth2 pair, a complete S3 pair / when `admitCompanionName` and `companionEnv` judge them / then they are admitted → exposes an allow-list that is too narrow.
    - given `FOO`, `LZ_ANYTHING`, `PATH`, `HOME`, `TF_CLI_ARGS`, `OVH_ENDPOINT` / then each is refused with `COMPANION_NAME_REFUSED <name>` → exposes the deny-list letting unforeseen names through.
    - given `tf_var_x`, `Tf_Var_x` / then refused → exposes case-insensitive matching.
    - given `TF_VAR_state_passphrase`, `TF_VAR_state_path`, `TF_VAR_run_id`, `TF_VAR_project_id` in any case / then refused → exposes the run's own variables being overridable.
    - given a half OAuth2 or S3 pair / then refused → exposes incomplete identities.
    - inverting the `passed/neighbours` subtest (`probe_identity_test.go:1158-1168`): given `HOMEPAGE`, `XPATH`, `TMPDIRS`, `OVH_ENDPOINT_URL`, `ovh_endpoint_url`, `MY_OVH_ENDPOINT`, `MY_TF_CLI_ARGS`, `MY_TF_LOG`, `XTF_LOG_PATH`, `TFLOG`, `TF`, `tf_var_tf_plugin_cache_dir`, `Tf_Var_Tf_Log` / then refused, while `TF_VAR_run_id_suffix`, `TF_VAR_project_ids`, `TF_VAR_state_passphrase_hint`, `TF_VAR_tf_cli_args`, `TF_VAR_tf_log_path`, `TF_VAR_state_path_hint`, `TF_VAR_state` stay admitted → exposes neighbours admitted by prefix.
  - Verify: `task test:live-lane`: the refusal cases for unforeseen, lower-case and neighbour names fail on today's deny-list; the reserved-name and cleanup rows of `TestProbeIdentityReservedNames` stay green.
  - Evidence: `evidence/T010.md`; PR; initial status `not-run`.

- [x] T011 [US1] Route binding errors through `quiet` in tools/internal/live/binding.go — closed 2026-10-15, evidence: evidence/T011.md
  - Requirements: FR-003, SC-001; ADRs: 0009, 0021. Depends on: T005.
  - Verify: `task test:live-lane`: T005's cases green. Guard G16: the `%w` mutant and a header-quoting mutant are killed. FR-003's "names the call, the status class and the next action" had no case in T005: add one here (given a hostile binding answer / when `Bind` fails / then the error names the call, `3xx` or the transport class, and the next action → exposes a quiet but unactionable error), red before the fix.
  - Evidence: `evidence/T011.md`; PR; initial status `not-run`.

- [x] T012 [US1] Register the leftover token with the run's redactor in tools/internal/live/leftovers.go and tools/internal/live/chain.go — closed 2026-10-15, evidence: evidence/T012.md
  - Requirements: FR-004, SC-001; ADRs: 0009, 0021. Depends on: T006.
  - Verify: `task test:live-lane`: T006's cases green. Guard G17: never-register and register-late mutants are killed.
  - Evidence: `evidence/T012.md`; PR; initial status `not-run`.

- [ ] T013 [US1] Implement `WriteCredentialSet` in tools/internal/live/files.go
  - Requirements: FR-005, SC-001; ADRs: 0009. Depends on: T007.
  - Verify: `task test:live-lane`: T007's cases green. Guard G18 writer mutants (files into live paths before the switch; switch before fsync; previous generation removed before the switch) are killed.
  - Evidence: `evidence/T013.md`; PR; initial status `not-run`.

- [ ] T014 [US1] Resolve credential sets in tools/internal/live/credentials.go and write them from tools/internal/live/apply.go
  - Requirements: FR-005, SC-001; ADRs: 0009. Depends on: T008, T013.
  - Verify: `task test:live-lane`: T008's cases green; `credentialFiles` (`apply.go:597-608`) writes through `WriteCredentialSet`. Guard G18 reader mutant (accept a `current` naming no sibling `gen-` directory) is killed.
  - Evidence: `evidence/T014.md`; PR; initial status `not-run`.

- [ ] T015 [US1] Take the child's home explicitly in tools/internal/live/env.go and its callers
  - Requirements: FR-007, SC-001; ADRs: 0009, 0021. Depends on: T009.
  - Verify: `task test:bootstrap; task test:live-lane`: T009's cases green; `hardening_test.go` follows the new signature. Guard G20 bootstrap-path mutant is killed.
  - Evidence: `evidence/T015.md`; PR; initial status `not-run`.

- [ ] T016 [US1] Replace the companion deny-list with `admitCompanionName` in tools/internal/live/runner.go
  - Requirements: FR-006, SC-001; ADRs: 0009, 0021. Depends on: T010.
  - Verify: `task test:live-lane`: T010's cases green. Guard G19 mutants are killed.
  - Evidence: `evidence/T016.md`; PR; initial status `not-run`.

## Phase 3: User Story 2 — Every live verb is bounded, inventoried and cleans up in time (P1)
Independent test: V002. Test tasks T017–T021, then implementation tasks T022–T026. Starts after
T016 (shared `test:live-lane`).

- [ ] T017 [US2] Write bounded-verb tests in tools/internal/live/apply_test.go and tools/cmd/lz-live/lane_test.go
  - Requirements: FR-001; ADRs: 0008, 0024. Depends on: T016.
  - Cases (given / when / then → defect exposed), against fake `tofu` children:
    - given an `apply` of an ephemeral instance with `Deadline` 0 / when `runLane` runs / then the deadline is `DefaultDeadline` → exposes the entry's deadline 0 (`tools/cmd/lz-live/main.go:173`).
    - given that `apply` / when two resources are created / then `inventory.jsonl` holds both ids, appended as each completes → exposes the missing inventory outside the chain.
    - given that `apply` / when it ends / then stdout holds `LZ_RUN <run_id> apply <outcome> inventory=<path>` and the cost reminder → exposes the missing summary.
    - given `plan` / when it ends / then the summary without inventory and without the reminder → exposes a wrong summary shape.
    - given `destroy` / when it ends / then the summary and the reminder → exposes A22.
    - given a fake `tofu` that hangs under `plan`, under `apply`, and under `destroy`, each with a short `Deadline` / when the verb runs / then it stops at the deadline with outcome `fail` and a non-zero exit → exposes an unbounded verb.
    - given the hang under `apply` / when the deadline fires / then the inventory is kept and no retained instance is destroyed → exposes a destructive deadline.
    - given SIGINT during `apply` / then the same as the deadline → exposes an unsafe interruption.
  - Verify: `task test:live-lane`: every case fails on today's code; the chain's rows stay green.
  - Evidence: `evidence/T017.md`; PR; initial status `not-run`.

- [ ] T018 [US2] Write destroy-budget tests in tools/internal/live/chain_test.go and tools/internal/live/runner_test.go
  - Requirements: FR-002; ADRs: 0008, 0024. Depends on: T016.
  - Cases (given / when / then → defect exposed):
    - given `destroyBudget` / when called with 0, with a run deadline below `DefaultDeadline`, and with one above / then it returns `DefaultDeadline`, `DefaultDeadline` and the longer value → exposes a run deadline below `DefaultDeadline` giving a destroy budget below `DefaultDeadline` (`chain.go:226-228,285-287`).
    - given a chain with `--deadline` below `DefaultDeadline` whose apply fails, on a fake clock / when destroy-on-exit runs / then it has `DefaultDeadline` and completes the reverse destroy → exposes the short destroy in the chain.
    - given the probe core with the same deadline / then the same → exposes it in `runner.go:830-832,921-923`.
    - given a destroy that exceeds its budget / then `fail`, with inventory, state and passphrase kept for `--cleanup` → exposes a silent pass.
  - Verify: `task test:live-lane`: the first three cases fail on today's code; the fourth passes today and pins the behaviour.
  - Evidence: `evidence/T018.md`; PR; initial status `not-run`.

- [ ] T019 [US2] Write retry-budget tests in tools/internal/live/chain_test.go
  - Requirements: FR-008; ADRs: 0008, 0024. Depends on: T016.
  - Cases (given / when / then → defect exposed):
    - given `newRetryBudget(end, reserve)` / when `admit` is asked for a wait that leaves at least the reserve, and one that leaves less / then true, then false → exposes a wait taken with no time left for the read and listings (`chain.go:616`).
    - given three retained instances each needing one retry / when the leftover check reads them / then all finish within one budget → exposes a fresh backoff per instance (`chain.go:713-717`).
    - given a refused wait / when the check reports / then `blocked waiting-for=<instance>`, never `pass` → exposes a late pass.
  - Verify: `task test:live-lane`: every case fails against the stub.
  - Evidence: `evidence/T019.md`; PR; initial status `not-run`.

- [ ] T020 [US2] Write KD-1 canary removal tests in tools/internal/live/observe_test.go
  - Requirements: FR-009; ADRs: 0008, 0024. Depends on: T016.
  - Cases (given / when / then → defect exposed):
    - given a fake create that answers 500 after creating the canary / when the collector runs / then the canary is removed → exposes the trap registered only after success (`observe.go:455-458`).
    - given that answer and a failing removal / then the canary is reported as a leftover → exposes a silent orphan.
    - given a create answering 409 for a name that existed before the run / then nothing is removed and the assertion reports `blocked` → exposes adoption of someone else's bucket.
  - Verify: `task test:live-lane`: the first two cases fail on today's code; the third is a pin if today's code already refuses adoption, otherwise it fails too (recorded either way); `TestObserveCanaryTrap` stays green.
  - Evidence: `evidence/T020.md`; PR; initial status `not-run`.

- [ ] T021 [US2] Write Object Lock assertion tests in tools/internal/live/s3check_test.go and tools/internal/live/observe_test.go
  - Requirements: FR-010; ADRs: 0008, 0009. Depends on: T016.
  - Cases (given / when / then → defect exposed):
    - given a fake S3 answering `GetObjectLockConfiguration` with governance mode and a default retention / when `S3Checker.ObjectLock` and `judgeObjectLock` run / then `pass` → exposes a parser that misreads the AWS shape.
    - given disabled, absent, compliance mode, or no retention / then `fail` → exposes a lenient judge.
    - given a read error / then `blocked` → exposes a read error read as `pass`.
    - given the chain's L7 assertions / when they run on two state buckets / then `state-bucket-object-lock` appears once per bucket → exposes the assertion missing from the chain.
  - Verify: `task test:live-lane`: every case fails against the stub. The live observation (premise P5) is out of scope.
  - Evidence: `evidence/T021.md`; PR; initial status `not-run`.

- [ ] T022 [US2] Bound and inventory plan, apply and destroy in tools/internal/live/apply.go, tools/internal/live/chain.go, tools/cmd/lz-live/lane.go and tools/cmd/lz-live/main.go
  - Requirements: FR-001, SC-001; ADRs: 0008, 0024. Depends on: T017.
  - Verify: `task test:live-lane`: T017's cases green; the chain's summary and inventory rows unchanged. Guard G23 mutants are killed.
  - Evidence: `evidence/T022.md`; PR; initial status `not-run`.

- [ ] T023 [US2] Give destroy-on-exit `destroyBudget` in tools/internal/live/chain.go and tools/internal/live/runner.go
  - Requirements: FR-002, SC-001; ADRs: 0008, 0024. Depends on: T018, T022.
  - Verify: `task test:live-lane`: T018's cases green; the destroy stays detached from the run's cancellation. Guard G21 mutant is killed.
  - Evidence: `evidence/T023.md`; PR; initial status `not-run`.

- [ ] T024 [US2] Share one `retryBudget` across retained instances in tools/internal/live/chain.go and tools/internal/live/runner.go
  - Requirements: FR-008; ADRs: 0008, 0024. Depends on: T019, T023.
  - Verify: `task test:live-lane`: T019's cases green.
  - Evidence: `evidence/T024.md`; PR; initial status `not-run`.

- [ ] T025 [US2] Register the canary trap before the create in tools/internal/live/observe.go
  - Requirements: FR-009; ADRs: 0008, 0024. Depends on: T020.
  - Verify: `task test:live-lane`: T020's cases green.
  - Evidence: `evidence/T025.md`; PR; initial status `not-run`.

- [ ] T026 [US2] Read and assert Object Lock in tools/internal/live/s3check.go and tools/internal/live/observe.go
  - Requirements: FR-010; ADRs: 0008, 0009. Depends on: T021, T025.
  - Verify: `task test:live-lane`: T021's cases green; with T022–T026 closed the target is green.
  - Evidence: `evidence/T026.md`; PR; initial status `not-run`.

## Phase 4: User Story 4 — Checks re-run when what they read changes (P2)
Independent test: V003, V004 (T001–T004 above).

- [ ] T027 [US4] Write unreconciled-row and stage-lock tests in tools/internal/stacks/generate_test.go
  - Requirements: FR-011; ADRs: 0007, 0011. Depends on: none.
  - Cases (given / when / then → defect exposed):
    - given a manifest row without a reconciled stack / when `Generate` runs / then `*GenerateError{Code: "UNRECONCILED_ROW"}` and nothing written → exposes half-generated trees.
    - given a reconcile that creates a stack / when `Generate` runs / then the stack carries its stage's `.terraform.lock.hcl` → exposes a stack without a lock.
    - given two stages whose locks pin different `ovh/ovh` versions or hashes / when `CheckStageLocks` runs / then a `STAGE_LOCK_MISMATCH` finding names both → exposes racing plugin caches.
    - given the sandbox manifest / then all green and `TestRepositoryStackLocksMatchStages` unchanged → exposes a fix that breaks the real tree.
  - Verify: `task test:stacks`: the first three cases fail against the stubs.
  - Evidence: `evidence/T027.md`; PR; initial status `not-run`.

- [ ] T028 [US4] Refuse unreconciled rows, copy stage locks and compare them in tools/internal/stacks/generate.go and `lz-stacks check`
  - Requirements: FR-011; ADRs: 0007, 0011. Depends on: T027.
  - Verify: `task test:stacks; task stacks:check`: T027's cases green; the real tree passes.
  - Evidence: `evidence/T028.md`; PR; initial status `not-run`.

## Phase 5: User Story 3 — The offline entry bounds memory and processes (P2)
Independent test: V005, V006. Test tasks T029–T031, then implementation tasks T032–T034.

- [ ] T029 [US3] Write entry-limit tests in tools/cmd/lz-offline/limits_test.go and tools/cmd/lz-offline/limits_host_test.go
  - Requirements: FR-013; ADRs: 0008, 0021. Depends on: none.
  - Cases (given / when / then → defect exposed):
    - given limits and a bwrap argv / when `scopeArgv` builds the command / then it carries `MemoryMax`, `TasksMax`, the unit and the unchanged bwrap argv → exposes a dropped limit or an altered sandbox.
    - given a missing `systemd-run`, an unreachable user manager, or controllers lacking `memory` or `pids` / when `limitsUnavailable` runs / then it names the reason and the entry never starts the child → exposes a start without limits.
    - given a fake cgroup directory whose `memory.max` or `pids.max` differs from the request / when `checkScope` runs / then it refuses → exposes limits silently not applied.
    - given `memory.events` with `oom_kill 1`, and `pids.events` with `max 1` / when `limitTripped` reads them / then `memory`, then `tasks` → exposes a limit kill reported as an ordinary failure.
    - host, outside the entry (premise P2): given a child allocating twice the limit, and a fork storm past the task limit / when run under the entry with limits / then each is stopped within 10 s with `RESOURCE_LIMIT memory` or `RESOURCE_LIMIT tasks`, a valid child passes and `--die-with-parent` still ends the tree → exposes limits that do not hold on a real host.
  - Verify: `task test:runtime-image` through the entry and, on the workstation, `go -C tools test -tags hostlimits -run TestHostLimits ./cmd/lz-offline`: every case fails against the stubs and a stub entry that starts the child without limits.
  - Evidence: `evidence/T029.md`; PR; initial status `not-run`.

- [ ] T030 [US3] Write runtime-preparation tests in tools/internal/bundle/prepare_test.go with fixture archives in tools/internal/bundle/testdata/prepare/
  - Requirements: FR-014; ADRs: 0002, 0021. Depends on: none.
  - Cases (given / when / then → defect exposed):
    - given an archive with a symlink escaping the root, and one with a hard link escaping it / when `Project` extracts / then it refuses → exposes a link escape.
    - given an archive whose digest differs from its pin / when `VerifyArchive` runs / then it refuses → exposes an unverified input.
    - given valid fixture archives / when `Prepare` runs / then `resources.json` is accepted by `bundle.Admit` → exposes a shape the entry cannot read.
    - given an artefact `Prepare` uses that `checks.Packages` lacks / then `Prepare` refuses → exposes a second pin table.
  - Verify: `task test:runtime-image`: every case fails against the stubs.
  - Evidence: `evidence/T030.md`; PR; initial status `not-run`.

- [ ] T031 [US3] Write provenance-statement tests in tools/cmd/lz-pack/provenance_test.go
  - Requirements: FR-017; ADRs: 0010, 0021. Depends on: none.
  - Cases (given / when / then → defect exposed):
    - given a `provenanceInput` / when `provenance` renders twice / then the bytes are identical, keys sorted, no timestamp → exposes non-reproducible statements.
    - given the two reproducibility packs / then their statements are identical and the subjects are the packed manifest and layer digests → exposes a subject that is not the image.
    - given the statement / then materials name the source commit, `resources.json` and the entry digest, and the predicate type is SLSA provenance v1 → exposes missing materials.
  - Verify: `task test:runtime-image`: every case fails against the stub.
  - Evidence: `evidence/T031.md`; PR; initial status `not-run`.

- [ ] T032 [US3] Apply memory and task limits in tools/cmd/lz-offline/limits.go and tools/cmd/lz-offline/main.go, with limits set from measured peaks
  - Requirements: FR-013, SC-001, SC-003; ADRs: 0008, 0021. Depends on: T029.
  - Verify: `task test:runtime-image` and, on the workstation, `go -C tools test -tags hostlimits -run TestHostLimits ./cmd/lz-offline`: T029's cases green. Host step (premise P4): each CI target is run through the installed entry inside a measuring `systemd-run --user --scope`, its `memory.peak` and `pids.peak` are recorded, the constants are twice the largest peak (research R5), and every CI target passes through a locally built entry under them. Guard G22 mutants are killed. If P2 is refuted, the fallback of research R5 is implemented and FR-013 revised first. Publication and the CI pin are out of scope.
  - Evidence: `evidence/T032.md`; PR; initial status `not-run`.

- [ ] T033 [US3] Port runtime preparation to tools/internal/bundle/prepare.go with a `prepare` subcommand in tools/cmd/lz-pack/main.go, move the pins `prepare.py` holds alone into `checks.Packages` in tools/internal/checks, and remove tools/cmd/lz-offline/prepare.py
  - Requirements: FR-014; ADRs: 0002, 0021. Depends on: T030, T032.
  - Verify: `task test:runtime-image`: T030's cases green; `checks.Packages` is the one pin table; `prepare.py` gone and referenced only in history; the existing lz-pack and lz-offline rows unchanged.
  - Evidence: `evidence/T033.md`; PR; initial status `not-run`.

- [ ] T034 [US3] Write `provenance.json` beside each packed image in tools/cmd/lz-pack/provenance.go and tools/cmd/lz-pack/main.go
  - Requirements: FR-017; ADRs: 0010, 0021. Depends on: T031, T033.
  - Verify: `task test:runtime-image`: T031's cases green.
  - Evidence: `evidence/T034.md`; PR; initial status `not-run`.

## Phase 6: User Story 5 — Dependencies, releases and provenance have an offline lane (P3)
Independent test: V006 (T034), V007, V008. Test tasks T035–T036, then implementation tasks T037–T038.

- [ ] T035 [US5] Capture the pinned provider's schema into tests/fixtures/provider-schema/ through a `capture:provider-schema` target, and write update-lane tests in tools/internal/checks/updatelane_test.go with fixtures in tests/check/fixtures/updatelane/ behind a new `test:update-lane` target in Taskfile.yml
  - Requirements: FR-015, SC-004; ADRs: 0011, 0021. Depends on: T004.
  - Cases (given / when / then → defect exposed), each against `CheckUpdateLane`:
    - given a fixture configuration covering every pin source of data-model *Update-lane configuration* / then no finding → exposes false positives.
    - given a pin source without a rule naming its file / then `UPDATE_UNCOVERED` → exposes an unwatched pin.
    - given `automerge: true` anywhere / then `UPDATE_AUTOMERGE` → exposes unreviewed bumps.
    - given a runtime-mirrored tool outside the `needs-entry-revision` group / then `UPDATE_RUNTIME_GROUP` → exposes a bump that desynchronises the entry.
    - given the provider group without the snapshot path / then `UPDATE_PROVIDER_GROUP` → exposes an unreviewable provider bump.
    - given a snapshot version differing from the lock pin, or a snapshot without its command record / then `SCHEMA_VERSION`, `SCHEMA_RECORD` → exposes a stale or unprovenanced snapshot.
    - given malformed JSON / then `UPDATE_SYNTAX` → exposes a crash read as a pass.
    - given the repository itself (no configuration yet) / then `UPDATE_UNCOVERED` → the red that T037 turns green.
  - Verify: `task test:update-lane` (created here): every case fails against the stub, for its stated rule. The snapshot for `ovh/ovh` 2.21.0 is captured by the `capture:provider-schema` target through the entry against the mirror (premise P7; on refutation, on the host with the same pinned binaries, recorded as such), with its command record. Manager names are never judged (research R8). This task registers `test:update-lane` (creator 006/T035).
  - Evidence: `evidence/T035.md`; PR; initial status `not-run`.

- [ ] T036 [US5] Write release-train manifest tests in tools/internal/release/manifest_test.go with schemas/release-manifest.schema.json behind a new `test:release` target in Taskfile.yml
  - Requirements: FR-016, SC-006; ADRs: 0010, 0022. Depends on: none.
  - Cases (given / when / then → defect exposed), on a fixture repository the test builds with the runtime's `git` (the entry's snapshot carries no `.git`), holding conforming and non-conforming subjects and one train tag:
    - given a known commit and version / when `Manifest` renders twice / then the bytes equal the golden output both times and validate against the schema → exposes non-determinism or schema drift.
    - given version `2026.13.0` / then `RELEASE_VERSION` → exposes a malformed version accepted.
    - given the version the fixture tag carries / then `RELEASE_VERSION_TAKEN` → exposes a reused train version.
    - given a non-conforming subject since the tag / then it is listed under `unclassified` → exposes dropped commits.
    - given a working-tree change / then the output is unchanged → exposes a working-tree read.
    - given any render / then `tested` is empty and no tag or file is created → exposes an unverified claim or a side effect.
  - Verify: `task test:release` (created here): every case fails against the stub. This task registers `test:release` (creator 006/T036).
  - Evidence: `evidence/T036.md`; PR; initial status `not-run`.

- [ ] T037 [US5] Add the update lane in tools/internal/checks/updatelane.go and .github/renovate.json
  - Requirements: FR-015, SC-004; ADRs: 0011, 0021. Depends on: T035.
  - Verify: `task test:update-lane`: T035's cases green and the committed configuration passes; T003's registry test stays green for `test:update-lane`.
  - Evidence: `evidence/T037.md`; PR; initial status `not-run`.

- [ ] T038 [US5] Render the release-train manifest in tools/internal/release/manifest.go and tools/cmd/lz-release/main.go
  - Requirements: FR-016, SC-006; ADRs: 0010, 0022. Depends on: T036.
  - Verify: `task test:release`: T036's cases green. Rendering the real repository is a read-only host step with no credentials (quickstart), not a control.
  - Evidence: `evidence/T038.md`; PR; initial status `not-run`.

## Phase 7: User Story 6 — Docs and spec text say what is true (P3)
Independent test: V009, V010. Test tasks T039–T040, then implementation tasks T041–T042, then the
docs-only tasks T043–T044.

- [ ] T039 [US6] Write `verified_against` and superseded-ADR gate tests in tools/cmd/lz-check/docs_test.go
  - Requirements: FR-018; ADRs: 0013. Depends on: T004.
  - Cases (given / when / then → defect exposed), through `lz-check -root <fixture> docs`:
    - given a page whose `verified_against` equals the pin, and one two minors behind / then pass → exposes a threshold stricter than ADR-0013.
    - given a page three minors behind its pin / then `VERIFIED_AGAINST_STALE` → exposes a stale page passing.
    - given a page naming a tool with no pin / then `VERIFIED_AGAINST_UNKNOWN_TOOL` → exposes unchecked claims.
    - given a how-to naming `tofu` without `verified_against` / then `VERIFIED_AGAINST_MISSING` → exposes unstamped pages.
    - given a file outside `docs/adr/` citing a Superseded ADR / then `SUPERSEDED_ADR` → exposes references to withdrawn decisions.
  - Verify: `task docs:check`: every failing case fails on today's `docs.go`; existing `TestDocs` rows unchanged.
  - Evidence: `evidence/T039.md`; PR; initial status `not-run`.

- [ ] T040 [US6] Write repository-map tests in tools/cmd/lz-check/docs_test.go
  - Requirements: FR-019; ADRs: 0002, 0013. Depends on: T039.
  - Cases (given / when / then → defect exposed), through `lz-check -root <fixture> docs`:
    - given a map naming a deleted path under an admitted top-level name / then `MAP_PATH_GONE` → exposes a stale map.
    - given an admitted top-level directory present in the checkout and absent from the map / then `MAP_MISSING_DIR` → exposes an incomplete map.
    - given map paths outside the admitted names (`.gitignore`, `.sonarcloud.properties`) / then they are skipped and counted in `MAP_UNJUDGED=<n>`, never failed → exposes false failures on what the entry cannot see.
  - Verify: `task docs:check`: the first two cases fail on today's `docs.go`; the third fails for want of the count.
  - Evidence: `evidence/T040.md`; PR; initial status `not-run`.

- [ ] T041 [US6] Add the `verified_against` and superseded-ADR gates to tools/cmd/lz-check/docs.go, `verified_against` to the pages under docs/how-to/ and docs/reference/ that name a tool command, and the gates' reads to the `docs:check` inputs in tools/harness/checks.yaml
  - Requirements: FR-018, SC-005; ADRs: 0013. Depends on: T040.
  - Verify: `task docs:check`: T039's cases green (T040's rows stay red until T042, named in the evidence); the real pages pass the gates; `docs:check`'s inputs gain `mise.toml`, `mise.live.toml` and the lock files the gate reads, with a `TestRegistryInputsCoverReads` row that fails when one is removed; every edited page has its `references` blobs and `last_verified` updated (D102).
  - Evidence: `evidence/T041.md`; PR; initial status `not-run`.

- [ ] T042 [US6] Add the repository-map check to tools/cmd/lz-check/docs.go and bring docs/reference/repository-map.md in line with what the entry admits
  - Requirements: FR-019; ADRs: 0002, 0013. Depends on: T041.
  - Verify: `task docs:check`: T040's cases green and the target green; the map's reads join `docs:check`'s inputs with a `TestRegistryInputsCoverReads` row.
  - Evidence: `evidence/T042.md`; PR; initial status `not-run`.

- [ ] T043 [US6] Update docs/reference/verification-status.md, docs/known-issues/KI-001-ci-source-admission-unqualified.md and docs/known-issues/KI-002-offline-entry-memory-process-limits.md to what is true
  - Requirements: FR-020; ADRs: 0013. Depends on: T022, T023, T026, T032, T042.
  - Verify: exempt — docs-only; content review against T049 run 4 (`specs/005-first-landing-zone-slice/evidence/T049.md:16`, results table) and the code after T022, T023, T026 and T032: run-4 observations replace "UNVERIFIED until that run"; the plan/apply/destroy and destroy-budget lines describe FR-001/FR-002; KI-001 drops its claims that `.github/workflows`, `pipelines/github`, `tools/internal/checks` and `Taskfile.yml` are absent and lists its references; KI-002 records the limits, the unpublished-entry state and its new status. `task docs:check` stays green (blobs and `last_verified` updated).
  - Evidence: `evidence/T043.md`; PR; docs content review only.

- [ ] T044 [US6] Apply the open spec 005 wording fixes in specs/005-first-landing-zone-slice/spec.md and specs/005-first-landing-zone-slice/tasks.md and re-pin docs/reference/verification-status.md
  - Requirements: FR-021; ADRs: 0013. Depends on: T043.
  - Verify: exempt — docs-only; content review: FR-009 gains "a run lock is held only while its holder lives; runs on different workstations are serialised by the state lock only" (evidence T040.md:94, T041.md:89); P9's action list names `object/get` (T097.md:101); tasks.md line 78 says "dedicated owner clone" (T072.md:84; the T071 Verify stays as D92 history); `verification-status.md`'s blob for spec.md is updated in the same commit; `task check:specs` and `task docs:check` stay green.
  - Evidence: `evidence/T044.md`; PR; docs content review only.

## Final phase: Exit

- [ ] T045 Admit the closed 006 targets `test:update-lane` and `test:release` to CI in pipelines/github/foundation-source.json and .github/workflows/foundation.yml
  - Requirements: FR-022; ADRs: 0008, 0019. Depends on: T037, T038.
  - Verify: `task test:foundation-ci`: the source lists both targets, the workflow is regenerated with `generate:foundation-ci` and equals the rendering, and `ci-source` admits both because their creators are ticked; red before (`ci-source` refuses a target missing from the workflow, or with an open creator).
  - Evidence: `evidence/T045.md`; PR; initial status `not-run`.

- [ ] T046 Run the exit checks V001–V010 through tools/harness/checks.yaml
  - Requirements: FR-001, FR-002, FR-003, FR-004, FR-005, FR-006, FR-007, FR-008, FR-009, FR-010, FR-011, FR-012, FR-013, FR-014, FR-015, FR-016, FR-017, FR-018, FR-019, FR-020, FR-021, FR-022, SC-001, SC-002, SC-003, SC-004, SC-005, SC-006; ADRs: 0008, 0009, 0010, 0011, 0013, 0021. Depends on: T001–T045.
  - Verify: `task check:specs -- specs/006-harden-first-slice; task test:live-lane; task test:bootstrap; task test:stacks; task stacks:check; task test:traceability; task test:runtime-image; task test:update-lane; task test:release; task docs:check; task test:foundation-ci`: all green with discovery counts; the host limit test's record from T032 is present; every out-of-scope row still names its trigger; no Verify of this spec needs a credential, the network or the owner (SC-002).
  - Aggregate ADR rationale: 0008 test layers, live hygiene and traceability; 0009 credentials and state; 0010 release train; 0011 providers and the update lane; 0013 docs gates; 0021 supply chain and authorisation boundaries.
  - Evidence: `evidence/T046.md`; PR; initial status `not-run`.

## Planned command creators
| Command | Creator | Where |
| --- | --- | --- |
| `task test:update-lane` | T035 (green T037) | offline |
| `task capture:provider-schema` | T035 | offline (capture) |
| `task test:release` | T036 (green T038) | offline |
| `go -C tools test -tags hostlimits -run TestHostLimits ./cmd/lz-offline` | T029 | host, no credential |
| `lz-pack prepare` | T033 | host, preparation round |
| `lz-release manifest` | T038 | host, read-only, no credential |

## Dependencies and execution order
Per-task `Depends on` fields are authoritative and form a DAG.
- Setup: T001 → T002 → T003 → T004.
- US1 (after T004): tests T005, T006, T007, T008, T009, T010; then T005 → T011, T006 → T012,
  T007 → T013, T008 + T013 → T014, T009 → T015, T010 → T016.
- US2 (after T016, shared `test:live-lane`): tests T017–T021; then T017 → T022, T018 + T022 → T023,
  T019 + T023 → T024, T020 → T025, T021 + T025 → T026.
- US4: T027 → T028.
- US3: tests T029, T030, T031; then T029 → T032, T030 + T032 → T033, T031 + T033 → T034.
- US5: tests T035 (after T004), T036; then T035 → T037, T036 → T038.
- US6: T039 (after T004) → T040 → T041 → T042; T043 after T022, T023, T026, T032 and T042; T044 after T043.
- Exit: T037 + T038 → T045; T046 after all.
No owner gate. MVP boundary: US1 and US2 (the priority-1 items) are useful on their own.

## Parallel opportunities
Inside one story the test tasks may be written in parallel when they touch different files
(US1: T005, T006, T009, T010 and T007/T008 as a pair; US2: T017, T019 with T018 after it on
`chain_test.go`, T020/T021 on `observe_test.go` in sequence). Implementation tasks that share a file
run in sequence (`chain.go`: T012 → T022 → T023 → T024; `apply.go`: T014 → T022; `runner.go`:
T016 → T023; `observe.go`: T025 → T026; `tools/cmd/lz-offline/`: T032 → T033; `lz-pack/main.go`:
T033 → T034; `docs.go`: T041 → T042). US4, US3 and US5 use their own targets and may run beside
US1/US2; US5 and US6 share the red window of `test:traceability` and should not overlap.

## Completion
Rerun the V-commands and read their output; a missing binary, zero discovery or a simulation never
closes a task. The spec stays `draft` until T046 closes; the publication of the limited entry, the
live observation of P3 and P5 and every out-of-scope row stay open with their triggers.
