# Feature Specification: [FEATURE]
Created: [DATE] · Status: draft · ADRs: [IDS]
## Why and scope
[Reference-baseline contribution, learning objective and visible user outcome;
explicit exclusions. Adoption research is not a prerequisite.]
## Decisions and proposals
[Approved direction; undecided interface hypotheses; credible alternatives and
their concrete tradeoffs. Preserve pending joint decisions.]
## User Scenarios & Testing
### User Story 1 — [journey] (Priority: P1)
[Given/when/then, independent observable check; explained result, progressive
detail and next safe action. Include a failure/interruption/recovery scenario.]
## Premises
| ID | Mechanism premise | Probe | Evidence status |
| --- | --- | --- | --- |
| P1 | [claim] | [command or bounded procedure] | UNVERIFIED / observed / waived with decision |
## Requirements
- **FR-001**: [Externally observable behaviour]
## Acceptance and predefined verification
| Check | Requirements | Criterion, positive and negative outcomes | Verify | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001 | [outcomes] | [exact command; planned creator if absent] | [path] |
Every non-docs requirement and success criterion maps to a check. Docs-only entries
may use `exempt — docs-only`. Mark future targets planned and initial evidence not-run.
## Success Criteria
- **SC-001**: [Measurable completion, mapped above]
## Edge cases
[Absence, hostile input, partial failure, retry/concurrency]
## Implementation surface
[Paths and their purpose; no implementation code]
## Dependencies and stop conditions
[Exact gates; unobserved premise blocks dependent work]
