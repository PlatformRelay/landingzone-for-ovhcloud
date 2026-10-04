# Data model: offline checks and naming
- CheckDefinition: stable id, artefact kind, requirement/ADR ids, command, creator task,
  applicable clauses, discovery root, fixture provenance and review obligation.
- Observation: check id, subject/location, source/input/policy digests, tool versions,
  fixture or environment, observed/expected, status, evidence reference, upstream message.
  Only fresh matching evidence can pass; non-applicable and docs exemptions are explicit.
- TrustedSourceAdmission (T003 local entry / T023 CI): review reference, source closure and
  binary/image digests, external installation identity. Unqualified local admission blocks
  execution. Source manifests are data compared to external approval, never authority.
  Deferred to KI-001 (C010.5/P4), not required by T023: Actions-disabled observation, frozen
  workflow tree, active ruleset/empty bypass identity, candidate/history admission and
  denied-push controls.
- FoundationCIObservation (T023): executed workflow/Action/launcher/image/publisher identities,
  exact examined candidate SHA, run/check ids, per-check discovery/status. Fresh matching run
  evidence is required; enforced pre-execution source proof is deferred to KI-001.
- NamingCatalogue: kind, limits, allowed separators/charset, source URL/date, applicability
  (tags/labels/metadata/inventory), evidence status; an unknown kind refuses qualification.
- OrganisationTemplate: ordered segments, separator/case, abbreviations, per-kind override,
  truncation strategy and algorithm version. LabelSchema: namespace, required keys,
  hierarchy derivation, allowed values and authorisation-owned keys.
- NamingResult: scalar cardinality with shared context is selected; final field names and module
  splitting need concrete-consumer diagnosis before the later naming work. A result represents
  one logical resource; callers/catalogue checks retain stable logical IDs and detect collisions
  across resources. Names, canonical metadata and target labels/annotations/tags are separate;
  no provider ids or invented URNs. Import override stays exact; metadata updates never rename.
  Name-affecting algorithm/template/abbreviation/catalogue revisions are frozen together.
Validation: strict YAML, duplicate/unknown fields rejected; no silent truncation/collision;
managed-by, managed-in, instance and release are mandatory for applicable resources.
Typed HCL conversion is not strict decoding. Unknown name/security inputs block authoritative
apply preflight; projection/selector constraints are independent of canonical metadata.
