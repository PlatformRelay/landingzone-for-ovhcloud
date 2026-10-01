# ADR-0021: Threat model, authorisation boundaries and supply chain
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0004, ADR-0005, ADR-0006, ADR-0007, ADR-0009, ADR-0018, ADR-0019

## Context
The auto-merge lane, the agent workflow, the extension mechanism and the sandbox each need a trust
line, and when each draws its own they disagree. This ADR names the actors, the boundaries and the
permitted bypasses once, and the other ADRs reference it.

## Decision

### Actors
Tenant authors (self-service requests) · platform maintainers · AI agents authoring changes ·
compromised or misconfigured CI runners · publishers of dependencies (providers, actions, images,
tools) · OVHcloud account owner and admins with console rights · external attackers holding a
leaked credential.

### Boundaries (what crosses, what never crosses)
| Boundary | Rule |
|---|---|
| **Data vs. code** | Tenant files are data evaluated by the gate and rendered by platform code. Tenant HCL executes only in extension roots with that tenant's scoped credentials (ADR-0005). Provider and data-source execution is code execution; "read-only credentials" do not make untrusted plans safe. |
| **Request vs. authority** | Authority for a decision comes from the trusted base revision and platform-controlled files, never from the request being decided. Policy, schema, CI, owner and deployment-manifest changes use a protected lane with human approval. |
| **Authoring vs. execution** | Agents and contributors author in an environment with no cloud authority and no network to cloud endpoints in offline mode. Protected runners execute immutable, approved candidates with the minimum credential class for that instance (ADR-0009 matrix). Moving code to a protected branch does not make it trustworthy; the approval binding (ADR-0005) and the credential scope do. |
| **Evaluator vs. implementer** | Sensors, oracles, rubrics, capability grants and suppressions cannot be weakened through the routine lane; such changes require protected review and a justification line (ADR-0019). |
| **Forks** | Fork pull requests receive no credentials, no secrets and no cache writes used by privileged runs; no `pull_request_target` checkout of fork code. |
| **Cloud admin** | A console admin can bypass forge policy. The deny-floor (ADR-0006) bounds the catastrophic actions; the honesty page states the residual. |
| **Tenant ↔ tenant** | Separate instances, state owners, buckets, credentials and networks (ADR-0004, ADR-0017); a tenant credential must fail to read or write another tenant's state or resources (security test suite). |

### Permitted bypasses (named, not hidden)
Break-glass user (ADR-0018); bootstrap/order identity in the bootstrap runbook; escrow holders for
recovery (ADR-0009); each logged and alerting.

### Supply chain and evidence integrity
- Actions, includes and images pinned by digest; providers verified through the lockfile with
  hashes for the supported platforms; tool versions pinned in `mise.toml` and updated through a
  reviewed lane (no "newest everywhere").
- Every release has an immutable manifest (ADR-0010/0022) recording the full source closure,
  schemas, policies, toolchain and tested combinations; provenance attestation for release artefacts.
- Evidence packets (ADR-0019) bind observations to source revision, input digest, toolchain and
  environment; a missing observation is a gap, never a pass.
- Private artefacts: plan JSON, state, cassettes and logs are never published raw; sanitised
  summaries only.

### Security test suite
`tests/security/` holds the authority and cross-tenant abuse cases: self-authorising requests, forged
approvals, stale candidates, tenant-to-tenant state and network access, credential reuse after
offboarding, fork escalation, extension escape. These run on every change to policy, pipeline or
identity code and before every release.

## Consequences
- Several earlier ADRs became simpler once this boundary list existed (ADR-0005, 0007, 0019).
- The suite is a real cost; it is the test of the single claim the project makes most often.

## Counterpoints (kept even if overruled)
- A full threat model (STRIDE per component) would be more thorough; rejected for v1 as
  documentation that would outrun implementation. This ADR fixes the boundaries; detail follows the
  vertical slice.

## Verification
- The spike list of ADR-0005 and ADR-0009 doubles as the first security-suite content.

## Review log
- 2026-10-01: created from the round-2 external adversarial review.
