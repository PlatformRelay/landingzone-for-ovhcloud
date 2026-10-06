# ADR-0004: Stacks, one state each, and outputs passed as files
- Status: Proposed
- Date: 2026-10-06
- Related: ADR-0003, ADR-0005, ADR-0007, ADR-0009, ADR-0016, ADR-0021

## Context
A landing zone held in one OpenTofu state is hard to scope and hard to recover: one failed apply
locks everything, and anyone who can read the state sees every tenant's data. Staged landing zones
elsewhere (Google's FAST, for example) split the work into separately applied roots that pass
outputs to each other. Three OpenTofu facts matter here:
- `for_each` over tenants inside one root still writes **one** state. It is not isolation.
- `terraform_remote_state` hands the reader the whole state snapshot, secrets included.
- `moved` blocks cannot move an object from one state into another.

Some things must exist before others (a project before its private network), so the order of
applies has to follow real dependencies.

## Options considered
- **One root, many modules.** Simplest, but one state, one lock and one blast radius.
- **One stack per deployment, each with its own state, passing outputs as files.** Chosen.
- **One state per leaf module.** Mechanical splitting; far too many states, none with a clear owner.

## Decision

### Stage modules are reusable code
`stages/` holds the stage modules: `bootstrap`, `account-governance`, `account-fabric` (shared vRack
and audit facilities), `project` (adoption or vending, quotas, budget alert, machine identities,
resource group), `project-network`, `runtime`, `observability`. A stage module is an ordinary child
module. It takes typed inputs and returns typed outputs. It has **no backend and no provider
credentials**, so it never holds state on its own.

### Each deployment is a stack with one state
A *deployment instance* is a Terramate stack (ADR-0007): a small directory holding a generated
backend block, a generated provider block and a single `module` call into one stage module.
- **One stack, one state, one backend key, one lock.** A failed apply in one stack leaves every
  other stack untouched and unlocked.
- Tenant and runtime stacks exist once per tenant × environment (and per region where ownership
  needs it); account-level stacks exist once.
- A `for_each` over tenants inside one stack is never used to separate tenants.

### `deployments.yaml` lists the stacks
The tenant repository holds `deployments.yaml`, one row per stack: an immutable `instance_id`, the
stage module and its version, tenant, environment and region, the OVHcloud project, the backend
bucket and key, which credential may plan and apply it, and which other stacks' outputs it reads.
The reconcile task (ADR-0007) creates and generates the stack directories from it; nobody writes a
backend block by hand. Tenants cannot change `deployments.yaml` through the routine self-service
lane (ADR-0005, ADR-0021).

### Stacks share outputs as files, never by reading state
After a successful apply, a stack publishes an `outputs.json` that is validated against a schema in
`schemas/`. The pipeline stores it in an access-controlled place (an `artifacts/` prefix in the
state bucket with its own permissions, or the forge's artefact store). A stack that needs another
stack's values reads **only** that file; no stack reads another stack's state. Changes to an outputs
schema follow the contract versioning rules (ADR-0010).

### Order follows real dependencies
bootstrap → account-governance → account-fabric → project → project-network → runtime →
observability. Each arrow exists because the later stack reads the earlier stack's outputs, not
because of a stage number. Audit sinks the account needs before any project exists belong to
`account-fabric`. A runtime never waits on observability, and unrelated tenants never wait on each
other.

### Which stacks a run touches
A run selects the stacks whose own files changed, plus every stack that reads outputs from a
selected stack (its consumers, transitively). Producers apply before their consumers in the same
run. A consumer whose upstream `outputs.json` changed since its last apply is re-planned, never
applied from a plan made before the change.

### Postponed until there is a need: the artefact transaction
Separate states have separate locks, so nothing stops a producer from applying again between a
consumer's plan and that consumer's apply. A full guard against that race was designed:
generation-numbered, immutable output files with a publication record; plans bound to the exact
input digests; an apply-time check that aborts on a superseded input; and wave-by-wave scheduling.
It is **postponed** (operator decision, 2026-10-06). It is custom machinery Terramate does not
provide, and the simple rule above (producers first, then re-plan consumers whose inputs changed)
covers ordinary runs. Revisit when one of these happens: two pipelines apply connected stacks
concurrently; a consumer applies against stale outputs; or the transaction spike below shows the
simple rule losing a change.

### Moving a resource between stacks
This is a tested procedure, not a `moved` block: stop both pipelines, keep both state snapshots,
remove the object from one state and import it into the other, check both plans, and record the
move in `deployments.yaml`.

### Safe by default
No stack deletes resources it does not own. State-, identity- and network-bearing resources have
deletion protection, plus a decommission gate that still works when the resource's configuration has
been removed (ADR-0005). The plan review shows destroys prominently.

## Consequences
- More backend keys, all generated from `deployments.yaml` (ADR-0007), never hand-edited.
- A failed apply affects only its own stack.
- The generated documentation includes a diagram of which stack owns which state (ADR-0020).
- Until the transaction is built, the pipeline must not apply connected stacks from two runs at
  once; one run at a time per tenant is the rule.

## Counterpoints (kept even if overruled)
- Many small stacks multiply pipelines and output files; Terramate's generation and change detection
  keep that mechanical.
- Fixed stage numbers were easier to explain; rejected because they encoded dependencies that do not
  exist.
- Postponing the transaction leaves the plan-then-apply race open between runs. The one-run-at-a-time
  rule closes it for a single pipeline; it does not protect against a second, manual apply.

## Verification
- Spike: two tenants, two stacks, two scoped backend permissions. Tenant A cannot read or write B's
  state; the project stack applies before project-network; outputs pass as files on GitHub and
  GitLab without any state read.
- Spike (cheap, on one forge; decides whether the transaction is needed): an upstream-only change
  selects the consumer; a producer changed between the consumer's plan and apply is caught by the
  re-plan rule, or the spike shows it is not; a first deployment with no outputs yet waits for the
  producer; an unrelated tenant is never selected.
