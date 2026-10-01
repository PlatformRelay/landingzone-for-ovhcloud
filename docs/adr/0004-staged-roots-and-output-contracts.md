# ADR-0004: Stage modules, deployment instances, state ownership and the artefact transaction
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0003, ADR-0005, ADR-0009, ADR-0016, ADR-0021

## Context
Google FAST and example-foundation use staged roots with output contracts; monolithic single-state
landing zones are hard to scope and recover (Azure's `caf-enterprise-scale` was retired; the exact
reasons are not evidenced here). Three OpenTofu facts shape the design: a root invocation's `for_each`
over tenant files shares **that root's single state** (module instances cannot select their own
backend key, so a tenant loop is not state isolation and a failed run would lock every tenant);
`terraform_remote_state` exposes the whole state snapshot to its reader; and `moved` blocks do not
transfer objects between independent states. Project-local networks need their project to exist
first, so the dependency order must follow real references, not a numbering scheme.

## Options considered
- **One root, many modules** — simplest; monolithic state, one blast radius.
- **Stages as root templates, one root invocation per deployment instance**, each with exactly one
  state owner and backend key, exchanging versioned output artefacts.
- **Per-leaf-module states** — mechanical splitting; too many states, no ownership meaning.

## Decision
Stage modules composed into deployment instances; each instance is a root with one state.

**Stage modules** live in `stages/`: `bootstrap`, `account-governance`, `account-fabric` (shared
vRack and audit facilities), `project` (adoption or vending, quotas, budget alert, machine
identities, resource group), `project-network`, `runtime`, `observability`. A stage is a **child
module** that composes components: it declares `required_providers` and typed inputs (the effective
document, the consumed artefacts) and publishes typed outputs; it configures **no backend and no
provider credentials**. Stages are reusable code, not deployment instances.

**Deployment instances are the roots.** A platform-controlled manifest in the tenant repo,
`deployments.yaml`, enumerates every instance: `instance_id` (immutable) → stage module and version
→ tenant, environment, region → project id → backend bucket and key → runner authority (which
credential class may plan and apply) → the artefacts it consumes. Terramate materialises one stack
directory per instance (ADR-0007) holding the generated backend, provider configuration and the one
`module` call into the stage; the instance is the only place that may call a stage module, and
ADR-0002's "nothing imports `stages/`" is scoped to the library layers of the monorepo. **Each
instance has exactly one state owner and one backend key.** Tenant and runtime instances exist once
per tenant × environment (and per region where ownership requires it); account instances exactly
once. Tenant `for_each` inside a shared root is never used as state isolation. Tenant authors cannot
edit `deployments.yaml` through the routine self-service lane (ADR-0005, ADR-0021).

**Dependency order by real references, not a fixed number line:**
bootstrap → account-governance → account-fabric → project → project-network → runtime →
observability (workload streams). Audit sinks that the account needs before any project exist in
`account-fabric`, not in observability; a runtime never waits on the observability instance and vice
versa. Every edge names a versioned output artefact.

**Output artefacts, not state reads.** Each instance publishes `outputs.json` validated against a
schema in `schemas/`, written by the pipeline to an access-controlled location (the state bucket's
`artifacts/` prefix with its own permission, or the forge's artefact store). Consumers read **only**
the artefact; no instance reads another's state. Artefact changes follow the contract semver rules
(ADR-0010), which establish shape compatibility only. A manifest entry declares inputs consumed,
outputs published, the OVH API permissions the instance needs and the identity that applies it.

**Artefact generations and fencing** (separate state locks do not coordinate separate states, so
the transaction does):
- An output object is **immutable** and carries the producer instance id, a monotonically increasing
  deployment generation, the source revision, the effective-document digest and its own digest; it is
  written first and a **publication record** (`current` pointer to that generation) is written last.
  A consumer reads only generations with a publication record; a producer whose apply succeeded but
  whose publication failed has no new `current`, and the pipeline reports it as `blocked`, never
  silently serving the previous generation to new plans.
- A consumer's plan **binds the exact input artefact digests** it consumed (ADR-0005 decision
  binding). At apply time the runner re-reads each producer's `current` and **fences**: if a consumed
  digest is no longer the producer's current generation, or the producer is pending or failed, the
  apply aborts and the consumer is re-planned. Nothing ever applies against a superseded generation.
- The pipeline advances the dependency graph in **waves**: every instance in wave *k* is planned,
  gated, applied and has published before any instance in wave *k+1* is planned. A first deployment
  with no previous outputs therefore blocks consumers until producers have published; placeholders
  do not exist.
- The **affected set** of a run is derived from the manifest's artefact graph, not only from git:
  an instance is selected when its own inputs changed, when any consumed artefact's `current`
  changed since its last successful apply, or when a producer in the run published a new
  generation. Selection is distinct from ordering (ADR-0007).

**Cross-instance transfers** (moving an object between roots) are a tested procedure, not a `moved`
block: freeze both writers, preserve both snapshots, remove from one state and import into the
other, validate both plans, record the transfer in the instance manifest.

**Safe by default**: no instance deletes resources it does not own; deletion protection on state-,
identity- and network-bearing resources plus a decommission gate that stays effective when the
resource configuration is removed (ADR-0005); the plan-diff review shows destroys prominently.

## Consequences
- More backend keys; all generated by Terramate from `deployments.yaml` (ADR-0007), never hand-edited.
- Failure at one instance leaves every other instance untouched and unlocked.
- A diagram of state owners and writers is a required artefact of the generated documentation (ADR-0020).

## Counterpoints (kept even if overruled)
- Many small instances multiply pipelines and artefacts; Terramate's generation and change detection
  keep them mechanical, and the two-pipeline rule needs them anyway.
- Fixed stage numbers were easier to explain; rejected because they encoded a false dependency.

## Verification
- Spike: two tenants, two instances, two scoped backend authorities; tenant A cannot read or write
  B's state; project precedes project-network; artefact exchange works on GitHub and GitLab without
  remote-state access.
- Spike (producer/consumer transaction on one forge, the cheapest useful experiment): upstream-only
  change selects the consumer; publication failure blocks the consumer; a producer replaced between
  consumer plan and apply makes the apply abort; a fresh deployment with no previous outputs waits for
  the producer; an unrelated tenant is never selected.

## Review log
- 2026-10-01: round-2 external adversarial review applied.
