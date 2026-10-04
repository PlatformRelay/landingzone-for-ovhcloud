# Landing Zone for OVHcloud

> [!IMPORTANT]
> **A private open-source project — not provided by OVHcloud.** This repository is developed
> privately and independently by PlatformRelay and licensed as open source. It is **not** an
> official OVHcloud product or offering, and it is **not** provided, endorsed, sponsored, reviewed
> or supported by OVHcloud. OVHcloud will not help you with it, and nothing here speaks for
> OVHcloud. "OVHcloud" and related names are trademarks of their owner and are used only to say
> which platform the code targets.

**Status: early foundation.** The offline check tooling (pinned toolchain, sandboxed runner,
report, traceability, dependency and static checks) exists; no landing-zone modules exist yet.
Each piece is merged only with its own verification evidence.

## Purpose and differentiators

Create an inspectable reference baseline for an OVHcloud landing zone: useful code, documented
tradeoffs and reproducible verification that future implementations can be compared against.
Learning and visible technical contributions are worthwhile outcomes; adoption is the desired upside.

The ambition is a solid, approachable baseline with substantive reasons to choose it:

- **Automatic installation documentation:** system maps, ownership, permissions with reasons,
  control coverage and change history, with source references and honest observation status.
- **Per-run/JIT deployment credentials:** scoped access, explicit issuer authority, expiry and
  measured revocation windows rather than a blanket promise of secret-free automation.
- **Policy-driven auto-merge:** routine tenant requests on both forges, with attributable decisions
  and a visible deployment/recovery lifecycle.
- **Adaptable naming and labelling:** organisation-specific ordering and metadata, resource-specific
  constraints, import preservation and policy checks.
- **Guided terminal setup:** friendly use-case questions, explained suggested defaults, helper
  checks, accurate customization progress and save/resume before exporting a reviewable draft.

These are design commitments, not delivered capabilities. Each increment makes a user outcome
visible and verifies its failure cases. The [product direction](docs/explanation/product-direction.md)
defines the baseline, the user experience and how experiments become defensible claims.

OVHcloud already publishes [network landing-zone examples](https://github.com/ovh/public-cloud-examples/tree/0ab581a39afbe61a2daa55b39238530f38665cc2/landing-zone).
Our [reference comparison](docs/reference/upstream-reference-map.md) maps reusable topology,
modules, IAM/backend examples and user journeys, together with the checks needed before adaptation.
The proposed value here is a qualified baseline and its operating workflows; those outcomes still
need implementation and evidence.

## What this will be

An open-source set of OpenTofu/Terraform modules, composable components, golden-path profiles and
pipeline templates for building a governed landing zone on OVHcloud Public Cloud: a declarative
tenant model with self-service merge requests gated by [assent](https://github.com/PlatformRelay/assent),
identity across the OVHcloud IAM, OpenStack and Kubernetes planes with a pluggable identity
provider, naming conventions, networking, observability, cost and quota guardrails, with testing at
every layer and documentation that is checked for rot.

It is opinionated at the seams (identity, state, pipeline, naming, guardrails, audit) and free in the
middle: several **golden paths** (solo, team on Managed Kubernetes, team on VMs, federated
organisation, regulated) are planned over one codebase. Supported combinations will be listed with
their verification scope; the design also treats several base stacks and IAM providers as first-class.

OVHcloud has no management-group hierarchy and no organisation-wide policy engine, so this is not a
port of Azure's CAF or AWS's Landing Zone Accelerator. What it does instead, and what it cannot do,
is part of the design and is documented up front.

## Layout

Monorepo. The structure and other design decisions are in
[`docs/adr/`](docs/adr/README.md). ADR-0002 is Accepted; other records retain their indexed status.

The [initial Spec Kit phases](specs/README.md) cover the offline foundation, transaction
rehearsals and protected platform feasibility. Their specs, plans and task lists are drafts;
acceptance checks are predefined, and implementation evidence is still `not-run`.
An additional [guided-preconfiguration workstream](specs/004-guided-preconfiguration/spec.md)
defines the requested terminal journey alongside those phases; its implementation is also planned.

The [first implementation guide](docs/how-to/first-implementation.md) explains the minimum
safety subset, local versus live qualification, evidence and stop conditions.
The current delivery scope covers all eligible work in specs 001–004 as dependencies and
required decisions are satisfied. Independently reviewed increments are published as
stacked PRs and merge once review and applicable checks pass. T001–T009 remain the minimum safety
prerequisite for dependent implementation. Guided setup's hardened persistence/export
remains deferred until real profile schemas exist; no qualification follows from activation.

The [known issues](docs/known-issues/README.md) record verified findings and accepted
limitations, including [C2's unqualified CI source admission](docs/known-issues/KI-001-ci-source-admission-unqualified.md),
its nonblocking private-development disposition and reassessment trigger.
