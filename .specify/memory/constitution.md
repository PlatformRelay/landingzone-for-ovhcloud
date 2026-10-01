<!-- Sync impact: new constitution 1.0.0, 2026-10-01. Project overrides added for
spec, plan and tasks templates; command guidance aligned. Docs-only work is exempt
from automated-test requirements. Existing ADR statuses remain Proposed. -->
# OVHcloud Landing Zone Accelerator constitution
Version: 1.0.0 · Ratified: 2026-10-01 · Last amended: 2026-10-01

## I. Evidence before implementation
A spec MUST name its load-bearing host, tool and API premises, a probe and evidence
status before implementation. An unobserved mechanism is `UNVERIFIED`, never a fact.
A feasibility spike may start with an unobserved premise only when proving or refuting
it is the explicit deliverable; downstream implementation stays gated on that result.
Proposed ADRs guide drafts; only the operator changes their status to Accepted.
Claims about cloud behaviour need primary sources and environment-specific probes.

## II. Predefined verification and traceability
Every non-documentation spec requirement and success criterion MUST map to an
acceptance check before implementation. Every non-documentation task MUST carry:
`Requirements:`, `Depends on:`, `Verify:` (a command or bounded observable procedure),
the expected positive and negative outcome, and `Evidence:` (where results go).
The trace is requirement → ADR → implementation path → check → evidence. A future
command MUST name its creating task and be marked planned, not presented as runnable.
A docs-only task uses `Verify: exempt — docs-only` and needs no invented test; content
review and existing link/format checks remain appropriate. Executable examples,
generators, schemas, policies and workflow configuration are behavioural work even
when their output is documentation. Mixed tasks use the non-documentation rule.
Test-authoring tasks close with valid-case plus behavioural red evidence; implementation
tasks close with the same checks green. A missing binary or syntax error is not the red.

## III. Tests first; test the sensors
Write behaviour tests before or alongside implementation. Unit, contract, snapshot
and policy layers run without cloud credentials or cloud network access. Real-provider
and apply tests have separate discovery roots and protected targets. A gate MUST
reject a concrete defect for every guarded clause, retain a valid unusual case, and
reject zero discovery, parse errors, crashes and missing evidence. Tool-output fixtures
MUST be captured from the exact pinned version with the generating command recorded;
hand-written output is not proof of the parser. Never substitute the subject under test.
Regression fixes record behavioural red before and green after. Mutation survivors
block the claimed coverage until fixed or independently justified as equivalent.

## IV. One owner; explicit authority
Library modules are plain OpenTofu; components compose modules; child stages compose
components. Deployment instances are roots, each with one owner and unique state key.
Terramate materialises and generates instances; selection and ordering are separately
qualified. Consumers use published output artefacts, never another instance's state.
Tenant data cannot confer authority or edit its evaluator. Approvals bind the candidate,
base, policy, schema, effective input, instance, toolchain and consumed artefact digests.
Forks and authoring sessions receive no cloud authority. Privileged execution uses
protected immutable candidates; evaluators and suppressions require independent review.

## V. Fail closed; recover without the failed dependency
No missing observation, stale approval, unknown critical value, pending publication,
cleanup error or skipped check may be reported as pass. Reports distinguish `pass`,
`fail`, `blocked`, `not-run`, `review-required` and docs exemption. Saved plans, state,
credentials, raw plan JSON and raw live logs are private; commit only sanitised evidence.
Cloud work requires a dedicated sandbox, scoped cleanup authority, external resource-id
inventory, bounded leases and runtime, tested reaper and known maximum exposure.
Unknown prices, stale budget data or unhealthy cleanup block admission, not cleanup.
The approved ceiling is EUR 200/month, an exposure ceiling, not a provider hard cap.
No project purchase or destructive live probe is authorized merely by writing a task.
Key-loss recovery requires a separately decryptable backup and an independent escrow
package. Replication alone is not recovery; promotion must fence the old writer.

## VI. Small increments and honest support
Prove the cheapest useful slice first. Do not build every profile, TACO or sensor before
its consumer exists. Five golden paths remain the product direction; a rehearsal does
not qualify a supported tuple. Generators follow two real examples of their artefact
kind. Full ADR-0019 AgentEx scope is due with the first vertical slice, not silently cut.
OpenTofu >=1.13 is supported; Terraform claims require a passing version-specific job.
Tool and provider pins are exact, reviewed and recorded with evidence. Local installed
versions are observations, not permission to lower the minimum. No telemetry.

## Governance and lifecycle
Specs use `draft`, `active`, `done`, `dropped`; draft plans are not deployment approval.
Active requires observed premises (or an explicit justified waiver) and mapped checks;
a spike may activate only its bounded experiment. Done requires every check's evidence.
Dependencies that need operator setup remain `blocked` and name the prerequisite.
Independent adversarial review follows planning; a fresh technical review follows tasks;
cross-artifact analysis follows task generation. Findings receive explicit disposition.
Work follows workspace AGENTS.md: isolated worktree, lane claim, test-first engineering,
atomic conventional commits and linear history. Amendments require a reason, version
bump and synchronized templates; weakening a gate needs its own justification line.
