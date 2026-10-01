# ADR-0016: Golden paths as profiles over one composition graph
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0001, ADR-0002, ADR-0004, ADR-0005, ADR-0017, ADR-0018

## Context
The operator wants several supported setups, not one opinionated landing zone: "most landing zones
are too opinionated; I'd like golden paths". Prior art ships a few fixed scenarios (Azure's
accelerator lists several; AWS LZA and OCI one shape each) or a fork-and-customise starting point
(Google example-foundation). Forks rot; fixed shapes exclude.
Several independent designs for this project arrived at
the same mechanism: a golden path is **data selecting variants over one shared composition graph**.

## Options considered
1. **One root module per golden path** — simple to read; paths drift into forks within months.
2. **Profiles (data) over one composition graph** — every path instantiates the same stages; the
   profile pins which component variant each family uses and which optional leaves are on.
3. **Terragrunt/Terramate include hierarchy** — DRY, but a second tool for every adopter.
4. **A generic orchestrator expanding YAML into module calls** — hidden DSL, hard to test.

## Decision
Option 2.

**Profile = golden path.** `profiles/<name>.yaml` (schema `schemas/profile.schema.json`) pins:
- `scale`: `solo | org` (one project vs. tenants × environments);
- `identity`: `native | saml` (ADR-0018);
- `runtime`: `kube-managed | vm-openstack | managed-only | hybrid-vrack` (ADR-0017);
- `network`: `island | hub-vrack`;
- `resilience`: `1az | 3az`;
- `compliance`: `none | hds-aligned` (ADR-0012);
- `observability`: `ldp | byo | none`;
- `features`: optional leaves (`bastion`, `okms`, `backup`, `waap-appliance`, …);
- defaults tenants inherit and which of them are `overridable`.

**Golden paths shipped in v1** (tested end to end; names fixed here, contents in `profiles/`):

| Profile | For | Pins | Deliberately leaves out |
|---|---|---|---|
| `solo` | one team, one account, one project per environment (`dev`, `prod`) | scale solo, native IAM, island network, runtime chosen at init | federation, hub, self-service |
| `team-kube` | product teams on Managed Kubernetes | runtime kube-managed, K8s OIDC to the IdP, LDP | VM bastion path, hub |
| `team-vm` | lift-and-shift, licensing-bound workloads | runtime vm-openstack, bastion, security-group intents | autoscaling, image pipeline |
| `org-federated` | an organisation with an IdP and self-serving tenants | scale org, SAML, tenant repo gated by assent, two-pipeline rule, break-glass | hub (optional), compliance |
| `regulated` | health-data workloads | `org-federated` + hds-aligned allowlists, 3az, OKMS state encryption, audit bucket | anything outside the allowlist |

`hybrid-vrack` (bare metal / Hosted Private Cloud attached over vRack) ships as **documented and
partially automated**, labelled so; SecNumCloud is a documented non-goal for v1 (ADR-0012).
A **negative paths** page lists what is not supported and why (multi-account, cross-tenant shared
network emulation, Windows domain join, …).

**Composability rules** (enforced by CI):
1. Profiles contain no HCL. Code lives in `modules/` and `components/`; the graph lives in `stages/`.
2. Every component family exposes variants behind one output contract (ADR-0017, ADR-0018); a stage
   selects the variant at exactly one point, from the profile.
3. Every supported tuple resolves to an **acyclic dependency graph**; optionality is conditional on
   the tuple (backup, identity or observability can be prerequisites in one profile and absent in
   another), not a universal "leaf" property.
4. **Supported catalogue**: `catalog/supported-combinations.yaml` (ADR-0022) lists the tuples that are
   tested with their status; a profile outside the catalogue fails validation. The profile dimensions
   alone admit hundreds of raw tuples; five named paths are presets over the catalogue and do not
   widen it. Untested combinations are refused, not "probably fine".
5. **Invariant tests per supported tuple**: shared security and ownership invariants (one state owner
   per instance, tenant isolation, deny-floor coverage, no placeholder outputs) are tested across each
   supported tuple. Runtime-specific resource sets are **not** required to include one another; a
   "superset" relation between profiles is not meaningful when `solo` may select a VM runtime.
6. **Profile transitions are named migration workflows** (`solo → org-federated`, `team-vm` adding
   federation) with state-address mapping, adoption and data-preservation tests; changing a profile
   field never by itself authorises a migration or changes state ownership.
7. A tenant file (ADR-0005) references exactly one profile and may override only `overridable` fields;
   the tenancy cardinality (one project per tenant × environment) is defined once, in ADR-0005.

## Consequences
- Adding a golden path is a data file, an entry in the capability matrix, an example, and an
  acceptance test; no new HCL unless a new variant is needed.
- The composition graph carries conditionals; readability is protected by rule 2 (one selection
  point per family) and by plan snapshots per profile (ADR-0008).
- The capability matrix is small on purpose: five tested paths is the sustainable maximum for one
  operator plus agents (opinion).

## Counterpoints (kept even if overruled)
- Large conditional graphs plan slowly and read badly; one root per path (option 1) is easier to
  explain. Rejected because forks are the failure mode the operator named; inclusion tests give
  option 1's clarity without its drift.
- Five named paths may still be read as "the five opinions". Mitigation: the profile schema is the
  real surface; the named paths are presets, and the matrix says which other combinations are tested.

## Verification
- Spike (ranked 12 by the review): implement two profiles on **both** designs — the shared graph and
  thin per-path roots sharing components — on identical fixtures; compare measured readability, plan
  time, state isolation, permission scope and migration results. Zero changed HCL lines is not the
  criterion. Reject any design that needs hidden backward dependencies.
- Spike: `override_module` wildcards (1.13) in wiring tests only.

## Review log
- 2026-10-01: round-2 external adversarial review applied.
