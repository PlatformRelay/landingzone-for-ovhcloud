<!-- Sync impact: constitution 1.2.0, 2026-10-01. Reference-baseline purpose,
substantive differentiators, usable operations and reasoned challenge added;
project overrides and command guidance synchronized. Docs-only work remains exempt.
Existing ADR statuses remain Proposed; naming interface proposals remain undecided. -->
# OVHcloud Landing Zone Accelerator constitution
Version: 1.2.0 · Ratified: 2026-10-01 · Last amended: 2026-10-01

## Purpose
Build an inspectable reference baseline for an OVHcloud landing zone through useful
experiments. Learning, visible technical contributions and a credible comparison point
are outcomes in their own right; adoption is a desired outcome, not a prerequisite for
starting. The maintainer's OVHcloud experience informs the design; official ownership
and support are governed separately by ADR-0001. Do not require customer research or
adoption evidence before building a bounded increment.

## I. Evidence for claims; experiments for unknowns
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
The trace is requirement → ADR → implementation path → check → evidence. ADR links
name the decisions used by that task, not the whole spec list. A blanket task list
without per-requirement rationale is rejected; phase aggregators explain each included decision. A future
command MUST name its creating task and be marked planned, not presented as runnable.
A docs-only task uses `Verify: exempt — docs-only` and needs no invented test; content
review and existing link/format checks remain appropriate. Executable examples,
generators, schemas, policies and workflow configuration are behavioural work even
when their output is documentation. Mixed tasks use the non-documentation rule.
Test-authoring tasks close with valid-case plus behavioural red evidence; implementation
tasks close with the same checks green. A missing binary or syntax error is not the red.

## III. Tests first; test the sensors
Write failing behaviour tests before implementation. Unit, contract, snapshot
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
Current funding is a one-off EUR 200 trial credit; the total exposure ceiling never
resets monthly and is not a provider hard cap. Unknown credit/billing headroom blocks admission.
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

## VII. Useful differentiators and approachable operations
Automatic installation documentation, per-run/JIT credentials, policy-driven auto-merge
and adaptable naming/labelling are intentional product differentiators. Each needs a
visible user outcome, a named failure/recovery case and predefined verification;
their existence does not establish adoption or security effectiveness. Preserve the
approved ambition while delivering runnable increments. Product output distinguishes
planned, implemented and verified capabilities and their scope.
Simple entry points, progressive detail, explained changes, stable diagnostics and
the next safe action make background complexity operable. A Taskfile-backed guided
preconfiguration helper offers explained use-case defaults, accurate customization progress
and safe save/resume (ADR-0023); it prepares reviewed drafts without deployment authority.
Tests cover user-visible
behaviour and failure paths as well as internals. Tests do not remove responsibility
for inspecting authority, persistent state, dependencies and recovery. No extra portal,
general cloud management CLI or hosted control plane is implied by this principle.

## VIII. Reasoned challenge and explicit decisions
A proposed interface or technology is a hypothesis until explicitly decided. Compare
credible alternatives against the intended outcome; push back with a concrete failure
mode, source, experiment or better alternative. Do not manufacture objections merely
to sound critical. Distinguish approved direction from an undecided implementation
idea, and record material tradeoffs and the operator's decision. Once chosen, execute
the decision; reopen it only for new evidence or an explicit request. Pending choices
gate their dependent implementation, while independent work proceeds.

## Governance and lifecycle
Specs use `draft`, `active`, `done`, `dropped`; draft plans are not deployment approval.
Active also requires operator disposition of constitution ratification and workflow
scaffolding location; the operator confirmed constitution 1.2.0 ratification and committed
workflow scaffolding on 2026-10-01.
Active requires observed premises (or an explicit justified waiver) and mapped checks;
a spike may activate only its bounded experiment. Done requires every check's evidence.
Dependencies that need operator setup remain `blocked` and name the prerequisite.
Adversarial review follows planning; a fresh technical review follows tasks;
cross-artifact analysis follows task generation. Reviews include the current decision log
and open operator decisions and state their commissioning and exact scope. Findings receive explicit disposition.
Implementation uses an isolated checkout, tests before behavioral code, one logical
change per conventional commit and linear history. Coordination and review records stay
local; committed material carries no authoring or review attribution. Amendments require a reason, version
bump and synchronized templates; weakening a gate needs its own justification line.
