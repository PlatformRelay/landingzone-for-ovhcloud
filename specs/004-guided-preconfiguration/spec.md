# Feature Specification: Friendly, resumable repository preconfiguration
Created: 2026-10-01 · Status: draft · ADRs: 0001, 0002, 0003, 0005, 0007, 0008, 0011, 0013, 0019, 0021, 0023

**Active slice — D29:** The early written journey (T014), bounded renderer comparison
(T001) and minimal in-memory controller portion of T002–T003 are authorized. T014 is
documentation only; prototype implementation/execution waits for actual 001/T003 safety
and capture qualification and independent review of the exact experiment packet.
Hardened persistence and export, including synthetic implementations, remain deferred
until real profile schemas exist. Full terminal/save/resume integration, final guide T012
and aggregate T013 remain open. D61 widens eligible work without removing these gates.

## Why and scope
Help an operator understand choices and prepare a reviewable repository configuration through
a friendly terminal journey. Save valid progress and explain consequences/next steps. No cloud
calls, secret collection, procurement, deployment or configuration-file writes are included.
Only owned checkpoint/staging/evidence areas in data-model.md may change; inventory all other paths.
This is a setup helper through Task, not a general management CLI.
Those write areas and saved-state transitions describe the eventual product, not permission
for the in-memory prototype to save or export. The early journey produces prose only.

## Decisions and proposals
The terminal journey, friendly explanations, helper checks and resumability are approved direction.
Huh versus Gum is a bounded renderer comparison; exact version/platform qualification is owed.
Naming convention input is independent of the pending phase-001 module-interface decision.

## User Scenarios & Testing
### User Story 1 — Understand and choose (Priority: P1)
An operator selects a use case and sees reasoned suggested defaults, accurate customization progress,
why each choice matters, its resolved default and consequences, can inspect detail,
go back and edit, and sees an actionable refusal for an incompatible choice. Independent check: V001.
### User Story 2 — Leave and return safely (Priority: P1)
Save-and-exit retains valid progress; restart resumes the correct step. A changed schema or earlier
answer triggers revalidation, while corruption or a foreign session refuses resume. Check: V002.
### User Story 3 — Inspect and export a draft (Priority: P2)
Local checks report prerequisites honestly; the operator reviews a diff and exports a complete draft
bundle. No existing repository file or cloud object changes. Checks: V003–V005.

## Premises
| ID | Mechanism premise | Probe | Evidence status |
| --- | --- | --- | --- |
| P1 | Pinned renderer supports keyboard/help/cancel and plain mode on selected platforms | Real PTY captures from a minimal Huh/Gum prototype | UNVERIFIED; T001/T008 |
| P2 | Local checkpoint operations meet atomicity/permission and single-writer contract | Filesystem fault/concurrency controls on the named OS matrix | UNVERIFIED; T004/T005 |
| P3 | Versioned choices/schemas expose compatibility and clear consequences | Synthetic strict choice/schema fixtures initially; real integration as those creators land | UNVERIFIED; dependency gate |

## Requirements
- **FR-001**: MUST satisfy each clause below.
  - **C001.1**: Provide a warm concise introduction.
  - **C001.2**: Explain each choice, default provenance and consequence.
  - **C001.3**: Offer optional detail and next safe action instead of an opaque boolean questionnaire.
- **FR-002**: MUST satisfy each clause below.
  - **C002.1**: Support back/edit/save-and-exit/resume.
  - **C002.2**: Invalidate dependent answers after an earlier change.
  - **C002.3**: Reject unsupported choices with reasons.
- **FR-003**: MUST satisfy each clause below.
  - **C003.1**: Persist bounded allowlisted data atomically.
  - **C003.2**: Use owner-only permissions and repository/version binding.
  - **C003.3**: Enforce single writer.
  - **C003.4**: Reject corrupt, duplicate, foreign or symlinked data.
- **FR-004**: MUST satisfy each clause below.
  - **C004.1**: Revalidate against current schema/catalogue/prompt contents.
  - **C004.2**: Rerun bounded helpers on resume and before final review/export.
  - **C004.3**: Do not treat old completion flag or unchanged Git HEAD as readiness.
- **FR-005**: MUST satisfy each clause below.
  - **C005.1**: Run bounded trusted local read-only helpers.
  - **C005.2**: Reject command injection.
  - **C005.3**: Report missing/mismatched tools as blocked.
  - **C005.4**: Require no secrets, cloud authority or network.
- **FR-006**: MUST satisfy each clause below.
  - **C006.1**: Show effective choices, blockers and deterministic draft diff.
  - **C006.2**: Export a complete schema-valid bundle only after explicit review.
  - **C006.3**: Preserve existing repository files.
- **FR-007**: MUST satisfy each clause below.
  - **C007.1**: Share controller/validation between interactive and noninteractive modes.
  - **C007.2**: Provide keyboard and plain-terminal operation.
  - **C007.3**: Leave unsupported terminals/platforms unqualified.
- **FR-008**: MUST satisfy each clause below.
  - **C008.1**: Never request or persist secrets.
  - **C008.2**: Never execute answer/session-supplied code.
  - **C008.3**: Never infer cloud authority.
  - **C008.4**: Never present planned/experimental selections as verified.
  - **C008.5**: Reject sensitive free-text and unsupported fields.
- **FR-009**: MUST satisfy each clause below.
  - **C009.1**: Show a progress bar and accessible completed/required count for current conditional journey.
  - **C009.2**: Count only valid completed steps.
  - **C009.3**: Recompute after back/edit/resume or relevant branch change without claiming deployment readiness.
- **FR-010**: MUST satisfy each clause below.
  - **C010.1**: Ask the use case.
  - **C010.2**: Resolve versioned suggested defaults with per-field provenance and explanation.
  - **C010.3**: Preserve valid explicit overrides.
  - **C010.4**: Preview changed defaults after a use-case edit.
  - **C010.5**: Retain qualification and customer-action gates on unsupported/regulated selections.

The requirements above retain the full product contract. The active prototype exercises
only in-memory choice/help/back/edit/progress behavior; save/resume, helper integration and
export clauses stay owed to their later creators. T014 illustrates that behavior without
claiming any V-check or success criterion has passed.

Each numbered clause inherits its parent requirement’s V-check and creating tasks.
For every guarded clause, implementation records a distinct expected outcome and
valid/defect control; parent coverage alone cannot satisfy an untested child clause.

## Acceptance and predefined verification
All targets are **planned**, with creators in tasks.md. Evidence starts `not-run`, private under
`.local/evidence/004/`. Synthetic choices do not qualify a real profile or naming kind.

| Check | Requirements | Positive and rejection outcomes | Verify (planned) | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001, FR-002, FR-009, FR-010, SC-001 | Use-case defaults have provenance and preserve valid overrides; valid journey shows help/consequence and exact conditional progress; invalid choice refuses, back/edit changes only dependent answers/progress, regulated choices retain gates; human tone rubric reviewed | `task test:configure-flow; task verify:configure-journey` | `004/journey.json` |
| V002 | FR-003, FR-004, SC-002 | Save/restart retains valid progress; write interruption retains prior checkpoint; stale schema invalidates affected answers; corrupt/duplicate/foreign/symlink/racing writer rejects | `task test:configure-resume` | `004/resume.json` |
| V003 | FR-005, FR-008 | Pinned local tool control passes; missing/mismatch/timeout returns blocked; injection, hook, secret input or network attempt refuses | `task test:configure-checks` | `004/checks.json` |
| V004 | FR-006, FR-008, SC-002 | Reviewed valid draft exports complete manifest with deterministic diff; unreviewed/invalid/partial/symlinked destination refuses; existing repo inventory remains unchanged | `task test:configure-export` | `004/export.json` |
| V005 | FR-007, SC-001 | Supported PTY/plain and noninteractive inputs give matching effective data; cancel/resume and narrow terminal work; unsupported platform clearly blocked | `task verify:configure` | `004/terminal.json` |

## Success Criteria
- **SC-001**: Every choice in the exercised journey has visible help, default provenance and consequences; the human walkthrough reviews tone/clarity independently of passing controller tests (V001/V005).
- **SC-002**: All exercised interruption/resume/export cases preserve the last valid checkpoint and leave existing repository files unchanged (V002/V004).

## Edge cases
No TTY, narrow terminal, cancel mid-input, process death mid-write, disk full, two writers,
copied session, stale prompt/schema, duplicate keys, unknown answer, invalid earlier edit,
unsupported tuple, missing tool, long helper run, symlink destination, secret-like free text.

## Implementation surface
`tools/cmd/lz-configure/`, `tools/internal/configure/`, `schemas/configure-session.schema.json`,
`harness/configure/{steps,checks,limits}.yaml`, `tests/fixtures/configure/`, `Taskfile.yml`,
`docs/how-to/preconfigure-repository.md`. One tools Go module created by 001/T001; no second module.

## Dependencies and stop conditions
Phase 001 T001 pins tools; its T007 report/evidence contract supports local integration.
The checkpoint has its own strict schema; it does not require the naming module/interface choice.
Phase 001 T016 naming schemas are required at real-export integration. Only in-memory
synthetic choice vectors may precede real profile data; they do not authorize synthetic
checkpointing or export. T004–T005 and T010–T011 wait for real profile schemas; real export
also needs configuration/catalogue/naming contract creators and any dependent interface
decision. Preserve all fault, permission, concurrency and publication controls for that work.
A renderer comparison needs exact reviewed pins, finite bounds and actual 001/T003 safety/
capture qualification before implementation/execution in this lane. Missing platform
qualification blocks that platform's claim, not T014 prose. No invented runnable cloud configuration.
