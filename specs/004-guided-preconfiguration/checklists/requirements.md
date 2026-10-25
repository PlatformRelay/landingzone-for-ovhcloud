# Specification quality checklist: Answers file to deployments.yaml
Purpose: check the spec set before the task loop · Created: 2026-10-25 (D127 refresh) · Spec: [spec.md](../spec.md)

## Content
- [x] Scope is D127's simplest form: a non-interactive answers file, exported to `deployments.yaml`,
  gated by the decoder behind `stacks:check`; no cloud call, credential or secret
- [x] The dissent is kept: what the answers file makes easier, measured (R2), and where it does not
- [x] Interactive journey, save/resume, presets and tool checks are deferred stories with triggers,
  not tasks
- [x] Every requirement is observable and has a check (V001–V007)
- [x] Every OVHcloud claim cites a source or is marked UNVERIFIED (P2)
- [x] One open owner choice, with a default (staging versus in place)
- [x] History D29, D61, D85, D113, D127 in the header

## Tasks
- [x] 9 open tasks plus closed T014; within the 8–14 target and the D127 leg cap
- [x] Every code task is red first, with given/when/then cases and the defect each exposes
- [x] Every offline Verify names a Task target the entry admits; the host-side runs are named
- [x] Every planned target names its creating task (T016, T020)
- [x] Guards G29–G32 each name a mutant
- [x] No task reads `~/.config/ovh-lz/`, runs a live target or writes `stacks/`
