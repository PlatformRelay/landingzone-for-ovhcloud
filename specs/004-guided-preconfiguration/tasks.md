# Tasks: 004-guided-preconfiguration — answers file to deployments.yaml
Status: planned · Created: 2026-10-01 · Refreshed: 2026-10-25 (D127) · Input: spec.md, plan.md, research.md, data-model.md, contracts/checks.md

**History.** T001–T013 of the earlier terminal/save/resume plan are retired unstarted by the D127
re-scope; their ids are not reused. T014 (the early written journey, D29) stays closed. New tasks
start at T015. The deferred stories D-US4 to D-US7 of spec.md have no tasks; each gets its own
tasks only when its trigger fires.

All open boxes are planned. `test:configure` is created by T016, `configure:check` and
`configure:export` by T020; until then they are not in `Taskfile.yml`.

**Red first.** Every code task (T016–T020) lands as two commits: a red commit with the task's tests
and a compiling stub whose new functions return "not implemented", where each new test fails for
its stated reason; then the green commit, which passes the same tests unchanged. A compile error, a
missing symbol or a missing fixture is not that red. Cases marked *pin* hold from the start. A fresh
session writes the tests from spec.md, data-model.md and the task text, not from plan.md's
rationale.

**Commands.** The entry admits only `lz-offline --candidate <checkout> -- task <target> [-- <path>]`,
so every offline Verify names a Task target and runs it through the entry, from `$HOME`. The entry's
snapshot skips `.local/`, so `configure:export` runs host-side (`mise exec -- task …`); it needs no
credential and no network. No task runs a live target or reads `~/.config/ovh-lz/`.
Mutation proof is required where a Verify names a guard G29–G32 (`contracts/checks.md`).
Evidence: the Verify output is summarised in the PR description; `evidence/<TID>.md` in this
directory points at the PR and names the red and green commits.

## Early written journey (closed, D29)

- [x] T014 Write the early journey in docs/explanation/guided-setup-walkthrough.md — closed 2026-10-02 (documentation authoring, D29)
  - Requirements: FR-009; ADRs: 0013, 0023. Depends on: none.
  - Verify: exempt — docs-only; independent content review of the narrative; links and whitespace checked. It describes the interactive journey, which D127 defers (D-US4); T021 marks it so.
  - Evidence: `docs/explanation/guided-setup-walkthrough.md`; independent content and boundary review APPROVE on 2026-10-02; no behavioural evidence claimed.

## Phase 1: Setup

- [ ] T015 Verify the trace of spec 004 in Taskfile.yml and tools/harness/checks.yaml (written in the spec-set commit)
  - Requirements: FR-010; ADRs: 0008, 0019. Depends on: none.
  - Verify: `task test:traceability; task test:foundation-ci; task check:specs`: `TestTaskfileTracesEverySpec` green; `task check:specs` with no argument prints `TRACE_OK` for 001, 004, 005, 006 and 007; `ci-source` reads spec 004; `test:foundation-ci`'s and `test:traceability`'s inputs name `specs/004-guided-preconfiguration/tasks.md`. If `ci:foundation`'s recorded runs go stale because a target's inputs changed, record it; re-recording `pipelines/github/foundation-runs.json` is the owner's host step, not this task's.
  - Evidence: `evidence/T015.md`; initial status `not-run`.

## Phase 2: User Story 1 — A second operator exports a manifest from a short answers file (P1)
Independent tests: V001, V002, V005. T016 → T017; T020 completes the story after US2 and US3.

- [ ] T016 [US1] Decode answers files strictly in tools/internal/configure/answers.go with schemas/answers.schema.json, tests in tools/internal/configure/answers_test.go and tests/fixtures/answers/, and the `test:configure` target in Taskfile.yml
  - Requirements: FR-001, FR-008; ADRs: 0005, 0021, 0023. Depends on: T015.
  - Cases (given / when / then → defect exposed), fixtures as data-model.md *Answers file* fixes them:
    - given `tests/fixtures/answers/sandbox.yaml` (11 values) and `sandbox-two-tenants.yaml` (15) / when decoded / then every field is read and defaults are not yet applied → exposes a decoder that drops or invents fields.
    - given each case of `tests/fixtures/answers/invalid/cases.json` (duplicate key at top, row and nested level; case variant `Tenants`; unknown field; missing `org`; `null` `forge`; wrong `apiVersion`; wrong `kind`; tenant name with a capital; region `gra11`; CIDR without prefix; `vlan_id` 4001; `vlan_id` without `cidr`; `project_mode` `order`; a second document; a trailing comment; block YAML) / when decoded / then refused with exactly its code and field path → exposes a lenient decoder (G29).
    - given 64 KiB of valid answers plus one byte / then `ANSWERS_TOO_LARGE`; given exactly 64 KiB / then decoded → exposes an off-by-one or a missing bound.
    - given a symlink to the sandbox answers / when read through the package's file reader / then `ANSWERS_NOT_REGULAR` → exposes a followed symlink.
    - given the schema file / when each valid and invalid fixture is checked against `schemas/answers.schema.json` in the test / then the schema and the decoder agree on accept or refuse for every fixture → exposes schema drift.
    - given the package's source / when parsed / then it imports no `net`, `net/http` or `os/exec` and calls no `os.Getenv`, `os.LookupEnv`, `os.Environ` or `os.UserHomeDir` → exposes a path to the network or the environment.
  - Verify: `task test:configure` (creates the target: `go -C tools test -count=1 ./internal/configure ./cmd/lz-configure`, the second package added by T020): red commit with every case except the *pin* failing for its reason; green commit passes them; G29 mutants each turn a case red.
  - Evidence: `evidence/T016.md`; initial status `not-run`.

- [ ] T017 [US1] Derive the manifest and gate it through the decoder in tools/internal/configure/export.go, tests in tools/internal/configure/export_test.go
  - Requirements: FR-002, FR-003, SC-001; ADRs: 0005, 0008, 0023. Depends on: T016.
  - Cases (given / when / then → defect exposed):
    - given the sandbox answers / when exported / then the bytes are value-equal to `stacks/deployments.yaml` (comments removed, JSON values compared) and pass `stacks.DecodeManifest` → exposes a wrong default, a missing toggle or a row in the wrong order.
    - given the two-tenant answers / when exported / then value-equal to `tests/fixtures/manifests/sandbox-two-tenants/deployments.yaml` → exposes rows not grouped per tenant.
    - given the same answers twice / then identical bytes → exposes map-order output.
    - given `shared_state_project` false / then no `sandbox` block is written; given true / then `{"shared_state_project": true}` → exposes a `false` block the decoder refuses.
    - given a region without `cidr` / then no `network` key and no network row, the runtime row kept → exposes a network row without a network.
    - given overrides of `state_endpoint`, `state_ref`, `project_ref`, `project_mode`, `vlan_id` and both toggles / then each appears in the manifest; given a key data-model marks fixed (`scope`, `stage_source`) / then `UNKNOWN_FIELD` from T016's decoder → *pin*; exposes a widened override set.
    - given answers whose derived manifest the decoder refuses (two regions with VLAN 0; `project_ref` `STATE` without the flag; two environments with one `project_ref`; a region name of 60 letters and a digit) / when exported / then a refusal with the decoder's code and no bytes returned → exposes output after a refusal (G30).
    - given the value counts of R2 / when the fixtures and manifests are counted / then 11, 15, 41 and 67 → exposes a fixture that grew.
  - Verify: `task test:configure`: red commit, then green; G30 mutants each turn a case red.
  - Evidence: `evidence/T017.md`; initial status `not-run`.

## Phase 3: User Story 2 — A mistake is explained in plain words (P1)
Independent test: V003.

- [ ] T018 [US2] Explain every refusal through the catalogue in tools/internal/configure/messages.go, tests in tools/internal/configure/messages_test.go
  - Requirements: FR-004, SC-002; ADRs: 0013, 0019. Depends on: T017.
  - Cases (given / when / then → defect exposed):
    - given the `Code*` constants parsed from `tools/internal/stacks/manifest.go` and the codes of `tools/internal/configure` / when the catalogue is checked / then each has a plain sentence and a `Next:` action → exposes a missing entry (G32).
    - given each invalid answers fixture and each refused derivation of T017 / when formatted / then exactly one line `configure: <CODE>: <detail>. <sentence>. Next: <action>` with the expected code and detail → exposes a raw decoder error printed as is.
    - given an unreachable decoder code / then its sentence says it is a defect of the tool → exposes a message that blames the answers.
    - given a temporary copy of the catalogue with one entry removed / when the completeness check runs / then it fails naming that code → exposes a check that passes on an empty catalogue.
  - Verify: `task test:configure`: red commit, then green; G32 mutants each turn a case red.
  - Evidence: `evidence/T018.md`; initial status `not-run`.

## Phase 4: User Story 3 — The maintainer sees what would change (P2)
Independent test: V004.

- [ ] T019 [US3] Print the dry-run diff and the consequence notes from tools/internal/configure/diff.go and tools/internal/configure/notes.go, tests in tools/internal/configure/diff_test.go and tools/internal/configure/notes_test.go
  - Requirements: FR-005, FR-006; ADRs: 0013, 0023. Depends on: T018.
  - Cases (given / when / then → defect exposed):
    - given the sandbox manifest as current and the two-tenant export / then four `+ row` lines for `lab`, each `created by stacks:reconcile`, then `diff: 4 added, 0 removed, 0 changed, …` → exposes a row-blind diff.
    - given the reverse / then four `- row` lines, each `refused by stacks:reconcile (UNSUPPORTED_CHANGE)` → exposes removals presented as harmless.
    - given a changed CIDR of `demo/dev/GRA11` / then one value line and no row line → exposes a value change shown as a row change.
    - given no current manifest / then every row `+`; given a current manifest that does not decode / then `CURRENT_MANIFEST_INVALID` and no diff → exposes a diff against garbage.
    - given the same inputs twice / then identical output → exposes unordered output.
    - given the two-tenant export / then the notes in data-model order: the `account.env` names `LZ_PROJECT_ID_STATE`, `LZ_PROJECT_ID_DEMO_DEV`, `LZ_PROJECT_ID_LAB_DEV`, no value; the one-project note; the KD-1 note; the mode note naming both environments `reference`; the VLAN note; the `org` note; the Object Lock note → exposes a missing or misordered note.
    - given answers without the flag and without any `cidr` / then no KD-1 note and no VLAN note → exposes notes printed regardless of their trigger.
  - Verify: `task test:configure`: red commit, then green.
  - Evidence: `evidence/T019.md`; initial status `not-run`.

## Phase 5: User Story 1 — the command (P1)

- [ ] T020 [US1] Add the lz-configure command with its staging write in tools/cmd/lz-configure/main.go, tests in tools/cmd/lz-configure/main_test.go, and the `configure:check` and `configure:export` targets in Taskfile.yml
  - Requirements: FR-005, FR-006, FR-007, FR-008, SC-003; ADRs: 0021, 0023. Depends on: T019.
  - Cases (given / when / then → defect exposed), each in a temporary root holding a copy of `stacks/deployments.yaml`, with an inventory of every path before and after:
    - given the two-tenant answers / when `check` runs / then the diff, the notes and `configure: ok`, exit 0, and the inventory unchanged → exposes a write during `check` (G31).
    - given the same / when `export` runs / then only `.local/configure/deployments.yaml` is new (file 0600, directory 0700), value-equal to the two-tenant fixture, and the output ends with the copy step and `task stacks:reconcile`, `task stacks:generate`, `task stacks:check` → exposes a write elsewhere or missing next steps.
    - given a symlinked `.local`, `.local/configure` or target / when `export` runs / then `STAGING_SYMLINK`, nothing written → exposes a followed symlink (G31).
    - given an existing staging file and an injected rename failure / when `export` runs / then `STAGING_WRITE` and the old file byte-identical, no temporary file left → exposes an in-place write (G31).
    - given an invalid answers fixture / when `check` or `export` runs / then one refusal line, exit 1, the inventory unchanged → exposes output after a refusal.
    - given a missing argument or an unknown verb / then usage, exit 2 → *pin* once the stub parses flags.
    - given the command's source / then it imports no `net`, `net/http` or `os/exec` and calls no `os.Getenv`, `os.LookupEnv`, `os.Environ` or `os.UserHomeDir` → exposes a path to the network or the environment.
  - Verify: `task test:configure; task configure:check -- tests/fixtures/answers/sandbox.yaml` through the entry (diff `none`, notes, `configure: ok`), then host-side `mise exec -- task configure:export -- tests/fixtures/answers/sandbox.yaml` (writes the staging file only; remove it after): red commit, then green; G31 mutants each turn a case red.
  - Evidence: `evidence/T020.md`; initial status `not-run`.

## Phase 6: Documentation and validation

- [ ] T021 Write the second-operator how-to docs/how-to/prepare-deployments-from-answers.md and align docs/explanation/guided-setup-walkthrough.md, docs/README.md, schemas/README.md and specs/README.md
  - Requirements: FR-009; ADRs: 0013, 0023. Depends on: T020.
  - Scope: the how-to starts from a commented copy of `tests/fixtures/answers/sandbox.yaml`, adds a tenant, shows a refusal and its fix, the diff, the notes, the export, the copy and the three stack commands, and ends in `task configure:check`. It carries spec.md's "makes easier" and "does not help" lists. The walkthrough states that the interactive journey is deferred (D-US4) and points to the how-to.
  - Verify: `task docs:check`: green, with the new page's header and references current.
  - Evidence: `evidence/T021.md`; initial status `not-run`.

- [ ] T022 [US1] Owner gate: a second reader, given only docs/how-to/prepare-deployments-from-answers.md, writes a two-tenant answers file and records the attempt in specs/004-guided-preconfiguration/evidence/T022.md
  - Requirements: SC-004; ADRs: 0013, 0023. Depends on: T021; a reader who has not edited `stacks/deployments.yaml` (the owner names one, or records that none is available and the task closes `blocked`).
  - Verify: `task configure:check -- <answers>` on the reader's file, host-side: `configure: ok`; the record names the time taken, each refusal met and whether its message was enough, and each step that was harder than editing the manifest would have been. The record decides D-US4's first trigger (spec.md).
  - Evidence: `evidence/T022.md`; initial status `not-run`.

## Final phase: Polish

- [ ] T023 Record spec 004's state in specs/README.md and specs/004-guided-preconfiguration/spec.md and run the exit checks
  - Requirements: FR-010; ADRs: 0008, 0019. Depends on: T022 closed or recorded `blocked`.
  - Verify: `task check:specs; task test:traceability; task test:foundation-ci`: green, with `TRACE_OK` for 004; the spec's status and the specs table row say what was built, what T022 found and which deferred story, if any, fired.
  - Evidence: `evidence/T023.md`; initial status `not-run`.

## Dependencies & execution order
- T015 → T016 → T017 → T018 → T019 → T020 → T021 → T022 → T023. The code tasks share
  `tools/internal/configure` and `test:configure`, so they run in order; nothing is pushed inside a
  red window.
- External gate: T022 needs a second reader (owner).
- MVP boundary: T016, T017 and T020 (an export that passes the decoder and lands in staging). T018
  and T019 carry the value for a second operator; T022 measures it.

## Parallel opportunities
- None among the code tasks (one package, one target). T021's prose can be drafted while T020 is in
  review.

## Completion
V001–V007 evidence green, or T022 recorded `blocked` with its reason; G29–G32 each with a killed
mutant; the deferred stories untouched unless their trigger fired, which T023 records.
