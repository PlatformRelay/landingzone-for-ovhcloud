---
references: []
last_verified: 558c76ab6af6a4b025d1f7c1834917721d524cfb
---

# Product direction: a useful reference baseline

Status: approved direction; architecture decisions remain Proposed and capabilities remain planned.
Canonical scope and positioning: [ADR-0001](../adr/0001-scope-positioning-and-unofficial-status.md).

## Why build it

Build a credible reference for what an OVHcloud landing zone can provide. Useful modules,
inspectable tradeoffs and reproducible checks make the work valuable even when another
implementation is adopted. The maintainer gains learning and visibility through technical
contributions. Adoption is an ambition; customer interviews or traction measurements are not
prerequisites for starting. Experiments should produce something runnable or a concrete lesson.

Published [OVHcloud network examples](../reference/upstream-reference-map.md) are a starting point.
Topology recipes and worked stories already exist. Reuse suitable parts, contribute general fixes
upstream, and demonstrate the additional operating and verification workflows here. More scope on
paper alone does not establish more value.

## Solid baseline, visible differentiators

Ownership/identity, tenant boundaries, safe deployment/recovery, operational visibility and
understandable adoption form the baseline. The differentiators make that baseline worth exploring:

| Differentiator | Visible benefit | Verification boundary |
| --- | --- | --- |
| Automatic installation docs | Inspect the system map, who may do what and why, and control coverage | Sources, stale/missing observations, rendered accuracy and private publication; ADR-0020 |
| Per-run/JIT credentials | Deployment uses bounded, scoped authority | Issuer authentication, granted scope, expiry, cleanup failure and already-issued-token revocation; ADR-0009/0018 |
| Policy-driven auto-merge | Routine requests progress without a platform-admin queue | Trusted ownership, bound decisions, races, visible deployment state and interrupted-run recovery; ADR-0005/0007 |
| Adaptable naming/labelling | Fit existing organisation conventions and locate the owner from a resource | Kind-specific constraints, ordering, import/name stability, authority-owned keys and metadata projection; ADR-0003 |
| Guided terminal setup | Answer use-case questions, understand defaults/consequences and save/resume with accurate progress | Conditional progress, strict checkpoint revalidation, read-only checks and reviewed draft export; ADR-0023 |

These are deliberate investments, not proven adoption drivers. Demonstrate them in complete
journeys, retain useful failures and improve what proves awkward. A comparison baseline exposes
both desired coverage and the verified subset, so another implementation can make an honest comparison.

## Easy to use and operable

Use profiles, typed data and the existing Taskfile/forge interfaces for a short happy path.
Resolve effective choices visibly before applying; expand into the technical detail on demand.
A failure explains its subject, observed/expected result, evidence and next safe action.
Keep merged intent, deployed resources and workload readiness visibly distinct. Recovery must
remain understandable when the happy path has stopped working.

Extensive tests make failures discoverable and behavior repeatable. Interfaces, authority maps,
durable states and recovery ownership make the system maintainable. Count each new dependency,
credential issuer, ledger and periodic job as operating work. The Taskfile-backed preconfiguration
helper is explicitly in scope; no extra portal, daemon or general management CLI is needed.

## Evidence and decisions

Every non-docs requirement/task has a predefined check with valid and rejection cases and an
evidence destination. Tool fixtures come from pinned tools; checks that discover nothing cannot
pass. Experiments can start with an explicitly unverified mechanism; supported claims need the
appropriate observed proof. The constitution (`.specify/memory/constitution.md`) defines this
contract, including the docs-only exemption. Writing this direction does not execute its future checks.

Challenge proposals with reasons and credible alternatives. A suggestion is not an order unless
the operator decides it. Separate approved direction, proposed interfaces and observed facts;
proceed on independent work while a joint choice is pending. Reopen a settled choice for new
evidence or an explicit request, rather than arguing by default.
