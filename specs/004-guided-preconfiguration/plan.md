# Implementation Plan: Guided preconfiguration
Date: 2026-10-01 · Spec: [spec.md](spec.md) · Status: draft

**Active slice — D29:** Deliver T014's early written journey independently. Prepare the
requirements for T001's bounded renderer experiment and the in-memory T002–T003 portion;
implementation/execution waits for actual 001/T003 safety/capture qualification and exact
experiment review. Persistence/export, including synthetic versions, wait for real profile
schemas. D61 does not waive this deferral. T012 remains the final guide after T009/T011.

## Summary
Build one setup helper behind Task with a pure journey controller, strict versioned session,
bounded read-only checks and reviewed draft export. Keep renderer separate from decisions/state.
Versioned use-case rules propose explained defaults; derive progress from the current relevant
validated steps, including changes after back/edit and resume.

## Technical Context
Go under the `tools/` module created by 001/T001; Huh library versus Gum subprocess prototype; exact pins
chosen before captures. Linux amd64 first, any other terminal/OS explicitly qualified later.
Local filesystem only, no secrets/telemetry/cloud cost. Helper timeouts and session size bounds
are explicit reviewed configuration; no arbitrary commands from repository/session/answers.

## Constitution Check
I: renderer/filesystem premises observed by bounded controls before claims. II–III: every FR/SC
maps to V001–V005 and every non-doc task has tests, expected outcomes and evidence. IV–V: setup
has no apply authority; sessions are untrusted data; persistence/export are guarded. VI: one helper,
no portal or broad CLI. VII: consequences and next actions tested, with a human tone review.
VIII: renderer comparison and the naming module final field names are explicit gates, not assumed decisions.

## Alternatives and decisions
Gum is quick for prompt prototypes; Go/Huh fits the controller/state language. Compare real cancel,
description, plain-mode and narrow-terminal behavior before choosing the renderer. No general CLI
framework or daemon. Keep naming preference data independent of the unresolved naming call API.

## Project Structure
Paths in spec.md; model in data-model.md; command/interaction contracts in contracts/checks.md;
quickstart.md is a planned verification guide, not a claim the executable exists.

## Delivery phases
T014 written journey → independent task-boundary and prose review. This branch has no runtime
dependency and cannot close T012 or qualify V001–V005.

After actual 001/T003 safety/capture qualification and exact experiment review:
T001 bounded renderer comparison → T002–T003 in-memory choice/controller red/green.
Keep terminal observations scoped to the measured renderer/platform. An in-memory cancel
discards answers; it must not imply a checkpoint or resume feature.

Later: real profile schemas → checkpoint red/green; trusted helper work also needs the
actual 001/T007 report contract. Checkpoint + helper integration → full terminal integration →
export red/green with real schema/catalogue/naming creators → final T012 guide → T013 aggregate.
The full controls remain in tasks.md; prototype completion never closes their deferred clauses.

## Next prototype packet (requirements only)
After the safety prerequisite is available, fix exact Huh/Gum package/library pins and
Linux amd64 terminal dimensions/input events. Review finite elapsed-time, process/descendant,
output and answer limits, source/input/build/destination/command bindings and capture admission
before running the experiment. Measure help, cancel, back, plain/no-TTY and narrow behavior;
retain unsupported-platform and wrong/missing-pin refusals and actual pinned captures.
No mock terminal output can supply qualification. Record missing evidence as not-run.

For the in-memory controller, prepare independent vectors for explicit versus suggested equal
values, changed use-case defaults, valid override retention, dependent invalidation, conditional
counts and unsupported/regulated gates. Use tests first against a compiling subject after the
safety gate; code, terminal captures and Task integration are not part of T014. Task integration
also requires the real trusted entry. No save/resume, synthetic exporter or second product CLI.

## Verification strategy
Use independent synthetic choice vectors and actual pinned terminal/tool captures. Exercise every
guarded clause with a valid unusual case and behavioral mutation; malformed output/tool absence
does not qualify as red. Check no repository writes/cloud calls by observing controlled adapters
and filesystem inventory. Fault atomic writes and lock contention; replay stale/corrupt sessions.
Human review checks warm concise language, understandable consequences and safe recovery wording;
automation never supplies that review. Documentation-only guide authoring is exempt.

## Dependencies and stop conditions
Within-feature DAG is in tasks.md. Phase 001 supplies pinned tools, reporting and strict schema
integration; real profile/catalogue data is another creator gate. Only the authorized in-memory
choice fixtures can precede that gate; synthetic persistence/export stays deferred. No applying the draft bundle,
cloud credential collection or deployment. The selected scalar naming interface is respected; its final field names await diagnosis.

## Complexity tracking
Count one controller, session format, renderer and export format. Reuse existing validation/report
contracts; never merge terminal events with authorization decisions. Export a draft staging bundle
rather than introducing partial multi-file repository writes and a migration engine.
