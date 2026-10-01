# Feature Specification: Friendly, resumable repository preconfiguration
Created: 2026-10-01 · Status: draft · ADRs: 0001, 0002, 0003, 0005, 0007, 0008, 0011, 0013, 0019, 0021, 0023

## Why and scope
Help an operator understand choices and prepare a reviewable repository configuration through
a friendly terminal journey. Save valid progress and explain consequences/next steps. No cloud
calls, secret collection, procurement, deployment or configuration-file writes are included.
Only owned checkpoint/staging/evidence areas in data-model.md may change; inventory all other paths.
This is a setup helper through Task, not a general management CLI.

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
- **FR-001**: MUST provide a warm, concise introduction and per-choice explanation/default provenance/consequence, optional detail and next safe action; no opaque boolean questionnaire.
- **FR-002**: MUST support back/edit/save-and-exit/resume and invalidate downstream answers when an earlier dependency changes; unsupported choices reject with reasons.
- **FR-003**: MUST persist bounded allowlisted session data atomically with owner-only permissions, repository/version binding and a single-writer guard; corrupt/duplicate/foreign/symlinked data rejects.
- **FR-004**: MUST revalidate saved answers against current schema/catalogue/prompt contents and rerun cheap bounded helper checks on resume and before final review/export; a prior completion flag or unchanged Git HEAD is never current readiness proof.
- **FR-005**: MUST run bounded trusted local read-only helper checks, reject command injection and report missing/mismatched tools as blocked; no secrets/cloud/network required.
- **FR-006**: MUST show effective choices, blockers and deterministic draft diff, then export a complete schema-valid bundle only after explicit review; preserve existing repository files.
- **FR-007**: MUST share controller/validation between interactive and noninteractive modes and provide keyboard/plain-terminal operation; unsupported terminal/platform stays explicitly unqualified.
- **FR-008**: MUST never request/persist secrets, execute answer/session-supplied code, infer cloud authority or present planned/experimental selections as verified; sensitive free-text and unsupported fields reject.
- **FR-009**: MUST show a progress bar and accessible completed/required-step count for the current conditional journey, counting only valid completed steps; back/edit/resume and newly relevant branches recompute it without claiming deployment readiness.
- **FR-010**: MUST ask about the use case and resolve versioned suggested defaults with per-field provenance/explanation; explicit overrides persist unless invalid, use-case edits preview affected defaults, and unsupported/regulated selections retain their qualification/customer-action gates.

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
`docs/how-to/preconfigure-repository.md`. One tools Go module, created by phase 001 T001; no second module.

## Dependencies and stop conditions
Phase 001 T001 pins tools; its T007 report/evidence contract supports local integration.
The checkpoint has its own strict schema; it does not require the naming module/interface choice.
Phase 001 T016 naming schemas are required at real-export integration. Synthetic fixture work
may precede real profile data, explicitly
without readiness claims. Real exports wait for the required schema/profile/naming contract
creators and any joint interface choice; no invented runnable cloud configuration. A renderer
comparison may use credential-free preparation; protected execution remains isolated. Missing
platform qualification blocks that platform's claim, not prose or independent fixture work.
