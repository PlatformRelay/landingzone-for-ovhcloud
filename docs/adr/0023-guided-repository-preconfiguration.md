# ADR-0023: Friendly, resumable repository preconfiguration
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0001, ADR-0003, ADR-0005, ADR-0007, ADR-0008, ADR-0013, ADR-0019

## Context
The operator requests a Gum-like terminal journey that asks a series of questions to preconfigure
the repository: warm, useful explanations, helper checks, consequences of choices and saved progress.
This is an explicit extension of the onboarding design. A long sequence of unexplained fields or
an attractive prompt wrapped around unsafe writes does not deliver that experience.

## Options considered
- Shell plus [Gum](https://github.com/charmbracelet/gum): polished prompts and a small prototype;
  persistence, validation and command/file boundaries still need an implementation.
- Go plus [Huh](https://github.com/charmbracelet/huh): forms in the existing tools language, with a
  separate testable controller/persistence layer; a library dependency and terminal integration tests.
- A portal or general management CLI: more identity, hosting and lifecycle scope than onboarding needs.

## Decision
Add a guided **repository preconfiguration helper**, exposed through a planned `task configure`,
not a deployment engine. Prefer the Go/form approach for shared typed validation and resumable
state; a bounded prototype compares Huh with Gum before selecting exact pins. This ADR does not
claim either renderer or a supported terminal matrix has been qualified.
The feature is specified in [004-guided-preconfiguration](../../specs/004-guided-preconfiguration/spec.md).

### Friendly flow and consequences
- Begin with the goal and the promise: prepare reviewable configuration; setup does not deploy.
- Ask about the use case first: learning/evaluation or production, one team or tenant self-service,
  existing project/runtime and identity needs. Resolve a proposed set of defaults from versioned
  rules; show why each was suggested, preserve explicit overrides and preview changes when the
  use case changes. Presets guide choices; they never imply compliance, qualification or authority.
- Ask one meaningful group at a time: existing repository/project context; intended organisation
  shape and profile; naming/label preferences; identity/credential route; guardrails, observation
  and cost choices; review and export. Supported choices come from versioned data, not prompt strings.
- Every choice has a brief explanation, a visible default/provenance, a concrete consequence and
  an escape to detail. Examples show resource-name order. Refuse unsupported combinations with a
  reason and an actionable alternative. A design-only/experimental selection stays clearly draft.
- Back, edit, save-and-exit and resume are first-class actions. Revisit only answers invalidated by
  an earlier change. Use respectful direct language; preserve technical precision and avoid a
  wall of jargon, celebratory filler or unexplained yes/no questions.
- Show a progress bar and a plain-text completed/required-step count for the current conditional
  journey. Count only valid completed steps; skipped inapplicable groups are outside the denominator.
  Recompute after use-case/back edits and on resume, without presenting customization completion as
  deployment readiness. Explain when a changed choice adds questions; do not fake fixed progress.
- Show effective choices, changes to existing files, blockers and next actions before export.
  Plain/accessible terminal mode and noninteractive input use the same validation/controller.

### Read-only helper checks
The default journey has no cloud credentials or network calls. Checks inspect the repository,
installed/pinned tools and local schema/choice compatibility with bounded trusted commands.
Missing prerequisites are reported as blocked or actionable setup work, never a success.
No arbitrary command from an answer, repository hook or resume file executes. Any future protected
cloud preflight remains a separate explicit lane with the existing authority and sandbox gates.

### Save and resume
Save an allowlisted, versioned session under gitignored `.local/configure/`, with owner-only
permissions, atomic replacement and a single-writer guard. Retain valid answers, last completed
step, explicit/default answer provenance, default-rule references and source/schema/catalogue/prompt
content bindings. Do not request or persist secrets; credential
choices record references/setup actions only. Reject malformed, duplicate-key, foreign-repository,
unsupported-version, symlinked or invalidly bound session data. Revalidate saved answers on resume;
changed dependencies invalidate affected downstream answers rather than silently using stale values.
An interrupted write leaves the last valid checkpoint. Always rerun the cheap bounded helper
checks on resume and before final review/export; unchanged Git HEAD is not unchanged local content.
Session data has no authenticity or authorization claim: schema-valid same-user edits are revalidated
as new input. No extra session signing-key lifecycle is introduced.

### Reviewable output
Export a schema-validated **draft configuration bundle** to a controlled local staging directory.
Require an explicit review step; show a deterministic diff and complete bundle manifest. Preserve
existing files by default; applying that bundle to the repository is a separate reviewed action.
Owned writes are limited to checkpoint/temporary/staging files under `.local/configure/` and private
reports under `.local/evidence/004/`. Inventory every other path during verification, including
ignored files. Publish a complete bundle atomically; retry cannot bless partial or changed content.
Neither export nor a completed wizard authorizes cloud work, commits, auto-merge or procurement.
The naming module interface remains a joint decision; the wizard can collect convention data
without inventing that module's final input schema. A session is local convenience, never authority.

## Consequences
- Onboarding becomes an approachable way to explore the differentiators, with an inspectable result.
- The helper is real behavioral code: state transitions, stale-state handling, input validation,
  filesystem safety and terminal behavior need tests/evidence, not only attractive prompt snapshots.
- The no-CLI-product scope in ADR-0001 is refined: this Taskfile-backed configuration helper is in
  scope; a general cloud management CLI or portal remains out of scope.

## Counterpoints
- Another questionnaire can become friction; defaults, short groups, optional detail, backward
  navigation and noninteractive reuse keep it useful for experienced users too.
- Saved state increases the attack surface and can rot; strict decoding, bounded data, trusted
  versioned choices, local permissions and revalidation prevent it becoming an executable plan.
- A renderer can be polished while the journey is cold or confusing; test consequence/help coverage
  and conduct a bounded human walkthrough using the documented rubric.

## Verification
Phase 004 predefined checks cover invalid/back/edit flows, corruption and crash-safe resume,
read-only command limits, deterministic reviewed draft exports, non-TTY and supported-terminal
behavior, plus a human tone/consequence walkthrough. Exact pins precede real terminal captures.
Every non-doc task records valid/rejection and red/green evidence; no wizard implementation exists yet.

## Review log
- 2026-10-01: independent plan review CLEAN; optional provenance, refresh and write-scope clarifications applied.
