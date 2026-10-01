# Naming and labelling: interface exploration and behavioral contract

Status: **per-resource pure helper with shared context selected** on 2026-10-01.
Detailed fields/module splitting need concrete-consumer diagnosis before 001/T013 onward;
those tasks remain outside the authorized foundation increment. Flexible conventions and verified behavior are
approved direction; a configuration vector is a hypothesis, not an instruction. This document
elaborates [ADR-0003](../adr/0003-layered-taxonomy-and-module-naming.md). No module exists yet.

## Separate the questions

A reusable organisation/context object avoids repeating coordinates and policy. That does not
require a module to know the whole resource inventory. Independently choose the unit of a call,
the input shape, the name algorithm and metadata projections. A provider-free helper may calculate
strings and maps; it neither reserves a name nor proves the caller's authority.

| Interface | Strength | Cost or failure mode | Position |
| --- | --- | --- | --- |
| One logical resource per pure module call; shared context and local request | Explicit identity/override, ordinary caller `for_each`, small contract | Repeated calls; cross-call collisions need a scope-level check | Selected; callers own cross-call collision checks |
| Batch map keyed by stable logical request IDs | One call, central within-batch duplicate detection | Mixed kind constraints, precedence rules, partial invalid inputs and larger error surface | Viable alternative; compare real call sites |
| Catalogue of candidate names by kind | Convenient discovery and constraint metadata | Candidate by kind does not identify two instances or establish availability | Useful auxiliary view; insufficient as the sole resource API |
| Naming provider or external generator | Rich language/data tooling and central execution | Extra executable dependency and lifecycle; policy and HCL can diverge | Defer unless a concrete pure-module limitation warrants it |

The selected scalar result is a name plus canonical metadata and explicit applicable projections.
A batch result would be keyed by logical request ID, never just kind or generated name. Resource
kind, human resource name/role and immutable logical ID are distinct. Final field names, module
split and any batch wrapper remain open; no pseudocode here is a released input schema.

## What established implementations teach us

- [Cloud Posse null-label](https://github.com/cloudposse/terraform-null-label) shows reusable context,
  configurable element order, case, delimiter and hash-backed shortening. Its context/individual
  input precedence is a feature with cognitive cost; we should define one visible resolution order
  rather than inherit every knob. A shortened hash remains a collision risk.
- [Azure's archived naming module](https://github.com/Azure/terraform-azurerm-naming) offers candidate
  names and constraints by resource type. Its migration notice explicitly preserves legacy behavior;
  an upgrade can otherwise change names.
- The successor [Azure Verified Naming Utility](https://github.com/Azure/terraform-azure-avm-utl-naming)
  demonstrates JSON catalogues with sourced overrides, configurable templates, numbered instances
  and explicit incomplete validation. Candidate generation and name availability are separate.
  Catalogue provenance and a preserved renderer are useful independently of our call interface.
- [azurecaf](https://github.com/aztfmod/terraform-provider-azurecaf) demonstrates a provider approach
  with type rules, affixes and cleanup. This is a credible alternative; its extra provider and
  normalization choices require justification in a small, provider-free first module.
- Kubernetes distinguishes [labels/selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/)
  from annotations and supplies [recommended application labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/).
  Preserve those semantics rather than inventing one universal tag format.

References inspected 2026-10-01. These are design examples, not OVHcloud constraint evidence.
Exact tool/module pins and captured outputs are required before behavioral fixture qualification.

## Flexibility with explicit boundaries

Use organisation-controlled naming/label data with strict decoding before HCL object conversion.
Typed HCL inputs are useful contracts but are not an unknown-field rejection oracle. Keep
organisation policy, deployment ownership and a resource-local request separate. Resolve defaults
once and expose their provenance. Tenant input cannot change protected policy, limits or
authorisation-owned values; merge and plan policy enforce that authority boundary.

Ordered named segments support kind first/last and human resource name first/last. Separators,
case, per-segment abbreviations, optional coordinates, padded ordinals and evidenced kind overrides
are useful candidates. Prefer a bounded segment vocabulary to arbitrary expression/template code
for the first interface. Reject missing required segments, unsupported rules and illegal output;
normalization must be explicit and diagnosable, never silently rename an imported object.

Catalogue rows carry min/max length and its measurement unit, character/end rules, reserved names,
uniqueness scope and metadata applicability, with source/date/evidence status. Unknown constraints
produce an experimental candidate and block a supported-cloud claim. Synthetic fixtures test the
algorithm only. No generic conservative regex qualifies all OVH products.

Names use stable identity coordinates. Mutable profile, release, owner or cost metadata cannot
rename them. Import overrides remain exact and validated against the applicable API constraint,
with a reviewed legacy exception where required; they are never cleaned or abbreviated implicitly.
Name-affecting template, abbreviation, catalogue and algorithm revisions must be pinned together.
An update shows an explicit migration diff; a version integer alone cannot freeze mutable data.

Shortening reserves an explicit deterministic suffix derived from canonical, unambiguous raw
immutable identity before lossy normalization/abbreviation; expose the
unshortened candidate and shortening reason. Reject an insufficient length budget and same-scope
duplicates after normalization/shortening. A scope-level check covers all selected callers, not
just one batch. A hash is collision resistance, not guaranteed uniqueness or global availability.
Platform/provider checks qualify actual availability separately. Unknown name/security inputs
must block the authoritative apply preflight; deferred HCL validation is not proof of plan-time refusal.

## Metadata is a separate contract

Canonical metadata preserves full values: managed-by, owning repository/path, immutable deployment
instance and release, plus the organisation's declared hierarchy, owner, cost and classification
keys. Separate required, optional and platform-owned keys; unknown keys and tenant edits to
authorisation keys reject. Never put secrets, personal contact data or mutable lifecycle values
into generated names; metadata has its own data-scope policy.
Reject attempted protected-key injection even when its value matches the current value. Changing
an authorisation key/namespace requires a reviewed authority migration and corresponding policy
updates, rather than an ordinary formatting override.

Project metadata into the actual target format: OVH IAM tags where supported, OpenStack metadata
or tags where its resource schema permits them, Kubernetes labels **and** annotations, or an
external inventory record when the API carries none. Report location/applicability explicitly.
No invented tags argument, generic field rename or silent dropping of required metadata is allowed.
There is one writer per metadata object; non-system tag coexistence stays unverified until probed.

For Kubernetes, label values have a restricted character set and length. The repository/path
belongs in an annotation even when short; invalid release values do too. Selector labels are an
explicit stable subset and never include mutable release/owner values. Standard application labels
are projected only from known application fields, with the actual management tool recorded;
infrastructure kind is not automatically an application name. No silent key/value rewriting that
changes selector or authorisation meaning. Validate key namespaces, reserved prefixes, target
counts/lengths and projection collisions against their own constraints.

## Predefined checks for the selected interface

These checks extend phase 001's existing acceptance contract; every command remains planned.
Creators T013/T014 cover naming, T015/T016 projections/schema/policy. Private evidence remains under
`.local/evidence/001/`; strict input checks also use T016/V006. Interface-dependent work waits for
concrete-consumer diagnosis. Pure prose explaining it needs content review, not an invented test.

| Behavior | Positive control | Rejection/stability control | Check |
| --- | --- | --- | --- |
| Organisation order and policy | Two independently authored kind/resource-first and last vectors | Missing segment, bad rule, typo/unknown field and conflicting overrides | V004/V005/V006 |
| Resource identity | Two same-kind resources with distinct stable logical IDs | Same-scope duplicate; reordered/inserted requests preserve existing IDs and names | V004 |
| Per-kind constraints | Exactly-at-limit and valid unusual names | Too short/long, illegal boundary/charset/reserved name, unknown kind | V004 |
| Deterministic shortening | Repeated input has the same candidate and suffix | Insufficient budget, normalization collision and injected hash collision | V004 |
| Import and upgrade | Exact imported override; pinned old recipe survives upgrade | Silent cleanup, changed recipe/default or metadata update cannot silently rename | V004 |
| Label authority | Declared hierarchy and required ownership metadata | Each missing required key, unknown key, forged protected value and conflicting writer | V005 |
| Target projection | Real target schema preserves complete metadata in its proper location | Short illegal K8s label, long value, projection collision, missing required unsupported field | V005/V006 |
| Selectors and status | Stable selectors; experimental constraints visibly unverified | Release/owner update changes no selector/name; unknown input cannot authorize apply | V004/V005 |

Run against the pinned tool and record positive, behavioral-red and green results. Handwritten
projections use shared cases with independently specified expected results;
initial generation is documentation only. Each guarded clause needs a sensitivity control.
Live OVH constraints and tag/authorisation visibility remain phase 003 probes.

## Concrete-consumer diagnosis before implementation

Compare concrete scalar and batch call sites for two organisation conventions and two same-kind
resources. Include import, one invalid field, a long name and a metadata-only update. Use the same
golden expectations and synthetic constraints, then inspect diffs and failure locality. Retain
the selected scalar/shared-context interface unless new evidence warrants reopening it;
avoid expanding a whole resource-inventory DSL before this comparison.
