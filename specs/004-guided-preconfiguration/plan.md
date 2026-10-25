# Implementation Plan: Answers file to deployments.yaml
Branch: worktree of `main` per task · Date: 2026-10-01 · Refreshed: 2026-10-25 (D127) · Spec: [spec.md](spec.md) · Status: draft

## Summary
Turn a short answers file into a complete `stacks/deployments.yaml` with one credential-free Go
command. The command decodes the answers strictly, derives every default and instance row, passes
the result through the decoder behind `stacks:check`, explains any refusal in plain words, shows a
dry-run diff against today's manifest and the consequences of the choices, and writes the result
to local staging. The operator copies it and runs the existing stack commands. The interactive
journey, save/resume, presets and tool checks are deferred stories with triggers (spec). Decisions
and measurements: [research.md](research.md).

## Technical context
- **Language/version**: Go 1.27.1 (tools module, vendored; `tools/go.mod` `go 1.27.0`), Task
  3.53.1 (`mise.toml`). No new dependency; strict decoding reuses `checks.DecodeStrict` and
  `stacks.DecodeManifest`.
- **Runtime**: the offline entry (`lz-offline`) for `test:configure` and for `configure:check` on fixtures; host-side,
  credential-free runs of `configure:check` and `configure:export` on an operator's file under
  `.local/`.
- **Testing**: Go tests against fixtures under `tests/fixtures/answers/` and the committed manifests;
  a temporary root per test for writes, with an inventory before and after; a source test for
  forbidden imports and calls.
- **Cost**: none. No cloud call, no credential, no live run.
- **Constraints**: agents never read `~/.config/ovh-lz/`; no live target is part of this spec.

## Constitution check
Before research and after design.

| Principle | Status | Note |
| --- | --- | --- |
| I Evidence, UNVERIFIED | pass | P1 read in code (believed, probed by T017); P2 cites the kb mirror and is UNVERIFIED beyond `gra`; P3 (the value for a second operator) is UNVERIFIED until T022 |
| II Traceability | pass | `004/` registry keys; 004 in the `check:specs` default and in `ci-source` (spec-set commit; T015 verifies); every planned target names its creator (T016, T020) |
| III Tests first | pass | every code task closes on a red commit (tests failing for their stated reason against a compiling stub) before its green commit; cases are given/when/then with the defect each exposes |
| III Mutation proof | pass (scoped) | guards G29–G32 (`contracts/checks.md`) |
| IV One owner, explicit authority | pass | the command derives a draft; `stacks/deployments.yaml` stays platform-controlled (ADR-0005); the copy is the operator's reviewed action (ADR-0023) |
| V Fail closed | pass | a refused manifest is never printed or written; a failed write keeps the previous staging file; unreachable codes report a defect, never success |
| V Cost | pass | no cloud work |
| VI Smallest increment | pass | D127's simplest form; four layers deferred with triggers; about 15 review legs |
| VII Planned vs implemented; approachable operations | pass | targets planned until T016/T020; plain messages with next actions; the how-to states where the tool does not help |
| VIII Decisions | pass | D127 with the dissent kept in spec *Why and scope* and research R2; one open owner choice with a default |

## Alternatives and decisions
- **Placement** (R1): new `configure` package and `lz-configure` (chosen) vs a verb of `lz-stacks`.
- **Rows** (R3): derived by one rule (chosen) vs one answer per row.
- **Gate** (R4): `DecodeManifest` in-process, then `stacks:check` after the copy (chosen) vs calling
  `stacks:check` on staging (it judges the candidate tree).
- **Messages** (R5): catalogue keyed by code with a completeness test (chosen) vs answers-level copies
  of the cross-row rules (drift).
- **Format** (R6): flow YAML (chosen) vs block YAML (a dependency decision).
- **Destination** (R7): staging (default) vs in place (open owner choice).
- Cheapest discriminating experiment for P3: T022, one second reader with the how-to, no code beyond
  US1.

## Project structure
```
specs/004-guided-preconfiguration/
  spec.md  plan.md  research.md  data-model.md  quickstart.md  tasks.md
  contracts/checks.md  checklists/requirements.md  evidence/ (created by tasks)
schemas/answers.schema.json                                 (new)
tools/internal/configure/  answers.go export.go messages.go diff.go notes.go (+ tests)   (new)
tools/cmd/lz-configure/    main.go main_test.go             (new)
tests/fixtures/answers/    sandbox.yaml sandbox-two-tenants.yaml invalid/ (new)
docs/how-to/prepare-deployments-from-answers.md            (new)
```

## Delivery phases
1. Setup: T015 (trace of spec 004, written in the spec-set commit).
2. US1 decode and export: T016 (answers decoder, creates `test:configure`), T017 (derivation, gate,
   round trip).
3. US2 plain messages: T018.
4. US3 diff and notes: T019.
5. US1 command: T020 (`lz-configure`, staging write, `configure:check`, `configure:export`).
6. Docs: T021 (how-to, walkthrough, README rows). Validation: T022 (second reader, owner-gated).
7. Polish: T023 (state of the spec, exit checks).

## Verification strategy
V001–V007 in spec.md and `contracts/checks.md`. Offline: `task test:configure`, `task check:specs`,
`task test:traceability`, `task test:foundation-ci`, `task docs:check`, each through the entry;
`task configure:check -- tests/fixtures/answers/sandbox.yaml` through the entry; `configure:export`
host-side on a fixture. A code task's red commit is a behavioural failure against a stub whose new
functions return "not implemented"; its green commit passes the same tests unchanged; G29–G32 each
kill a mutant. The round trip (T017) compares values, not bytes and not `Manifest` (research R4).
Docs tasks run `task docs:check`; T022's record is reviewed by hand.

## Dependencies and live change ordering
- Spec 005's decoder and schema and spec 007's two-tenant fixture are on `main`.
- T015 first. T016 → T017 → T018 → T019 → T020 (T018–T020 share `tools/internal/configure` and its
  target; they run in order). T020 → T021 → T022 (owner-gated) → T023.
- No live change. No timebox or cleanup authority is needed. Claim withdrawn on refutation: P1
  refuted (a value-equal export does not reconcile to the same stacks) → FR-003's gate claim is
  withdrawn and the copy step needs its own check; P3 refuted (T022 finds the answers file no
  easier) → D-US4 and later stay closed and the spec records it.

## Complexity tracking
One package, one command, one schema, two Task targets plus a test target, three fixture sets. No
new dependency, no state beyond one staging file, no authority. The review budget (D127: about 15
strong legs) is the binding constraint: two legs for the spec set, about one per code task (T016–T020),
one for the docs and two for the branch. Deferred: D-US4 to D-US7 and the in-place write (spec).
