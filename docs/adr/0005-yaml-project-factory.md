# ADR-0005: YAML project factory as the primary interface
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0003, ADR-0004, ADR-0006

## Context
On OVHcloud a Public Cloud project is the unit of isolation, billing and quota, so "vending" projects is
the central landing-zone act. Project creation is a **billed order** with default low quotas
(_UNVERIFIED in detail_), so it is slower and less automatable than subscription vending elsewhere.
Google FAST (YAML factories with context interpolation), AWS LZA (seven YAML files, schema-validated) and
the STACKIT/meshcloud "project factory" pattern make data the interface. AWS LZA's lesson: a YAML schema
is a ceiling; users need an escape hatch.

## Options considered
- **HCL variables only** — typed, no extra tooling, painful at 50 projects.
- **One YAML file per project** processed by a `project-factory` component, with JSON-Schema validation.
- **One generic orchestrator module** expanding arbitrary YAML into module calls (OCI) — flexible, but a
  large hidden DSL and hard to test.

## Decision (proposed)
One YAML file per project under the user's `projects/` directory, consumed by `components/project-factory`.

- Schema in `schemas/project.schema.json`, validated in pre-commit and CI **before** `plan`.
- Fields stay narrow: identity of the project (via naming inputs, ADR-0003), environment, IAM
  assignments (groups/roles → policies), network attachment (vRack, private network, subnets), quota
  intents, logging/alerting baseline, budget alert intent, tags. Anything outside the schema uses an
  **escape hatch**: a `extensions/<project>.tf` file in the user's repo that receives the factory outputs.
- Context interpolation (`${env}`-style references to values defined elsewhere) is limited to a documented set
  (names, regions, shared network IDs). No expressions.
- Quota and order-based steps are **intents + checks**, not silent actions: where OVH requires a manual
  order or payment method, the factory emits a `pending_actions` output and the pipeline reports it.
- The factory never destroys a project on file removal unless `lifecycle.allow_destroy: true` is set on
  that file in a prior commit (two-step removal).

## Consequences
- Adding a project is a PR with one file; reviewers diff data, not HCL.
- Schema becomes a public API: versioned, with a changelog (ADR-0010).
- OVH order latency makes factory tests partly asynchronous; integration tests use a pre-ordered sandbox
  project pool rather than ordering per test (ADR-0008).

## Counterpoints
- YAML encourages "programming in YAML"; the narrow schema and the no-expressions rule resist this.
- Two-step destroy is more ceremony; it protects against an accidental `git rm` deleting billed infra.

## Verification
- Spike: order one project via `ovh_cloud_project`; measure latency, required billing state, quota
  defaults; confirm whether ordering is idempotent under `tofu apply` retries.

## Review log
_(empty)_
