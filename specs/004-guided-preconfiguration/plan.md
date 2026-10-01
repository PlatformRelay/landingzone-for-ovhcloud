# Implementation Plan: Guided preconfiguration
Date: 2026-10-01 · Spec: [spec.md](spec.md) · Status: draft

## Summary
Build one setup helper behind Task with a pure journey controller, strict versioned session,
bounded read-only checks and reviewed draft export. Keep renderer separate from decisions/state.
Versioned use-case rules propose explained defaults; derive progress from the current relevant
validated steps, including changes after back/edit and resume.

## Technical Context
Go under the `tools/` module (created by phase 001 T001); Huh library versus Gum subprocess prototype; exact pins
chosen before captures. Linux amd64 first, any other terminal/OS explicitly qualified later.
Local filesystem only, no secrets/telemetry/cloud cost. Helper timeouts and session size bounds
are explicit reviewed configuration; no arbitrary commands from repository/session/answers.

## Constitution Check
I: renderer/filesystem premises observed by bounded controls before claims. II–III: every FR/SC
maps to V001–V005 and every non-doc task has tests, expected outcomes and evidence. IV–V: setup
has no apply authority; sessions are untrusted data; persistence/export are guarded. VI: one helper,
no portal or broad CLI. VII: consequences and next actions tested, with a human tone review.
VIII: renderer comparison and pending naming interface are explicit gates, not assumed decisions.

## Alternatives and decisions
Gum is quick for prompt prototypes; Go/Huh fits the controller/state language. Compare real cancel,
description, plain-mode and narrow-terminal behavior before choosing the renderer. No general CLI
framework or daemon. Keep naming preference data independent of the unresolved naming call API.

## Project Structure
Paths in spec.md; model in data-model.md; command/interaction contracts in contracts/checks.md;
quickstart.md is a planned verification guide, not a claim the executable exists.

## Delivery phases
Tool/renderer capture → controller red/green → checkpoint red/green → trusted helper red/green →
real terminal integration → export red/green → guide → independent journey/evidence inspection.

## Verification strategy
Use independent synthetic choice vectors and actual pinned terminal/tool captures. Exercise every
guarded clause with a valid unusual case and behavioral mutation; malformed output/tool absence
does not qualify as red. Check no repository writes/cloud calls by observing controlled adapters
and filesystem inventory. Fault atomic writes and lock contention; replay stale/corrupt sessions.
Human review checks warm concise language, understandable consequences and safe recovery wording;
automation never supplies that review. Documentation-only guide authoring is exempt.

## Dependencies and stop conditions
Within-feature DAG is in tasks.md. Phase 001 supplies pinned tools, reporting and strict schema
integration; real profile/catalogue data is another creator gate. Fixture work can proceed without
those future live artefacts; a fixture result is marked as such. No applying the draft bundle,
cloud credential collection or deployment. Pending joint naming choice remains respected.

## Complexity tracking
Count one controller, session format, renderer and export format. Reuse existing validation/report
contracts; never merge terminal events with authorization decisions. Export a draft staging bundle
rather than introducing partial multi-file repository writes and a migration engine.
