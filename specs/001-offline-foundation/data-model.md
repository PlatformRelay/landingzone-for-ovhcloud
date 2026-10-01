# Data model: offline checks and naming
- CheckDefinition: stable id, artefact kind, requirement/ADR ids, command, creator task,
  applicable clauses, discovery root, fixture provenance and review obligation.
- Observation: check id, subject/location, source/input/policy digests, tool versions,
  fixture or environment, observed/expected, status, evidence reference, upstream message.
  Only fresh matching evidence can pass; non-applicable and docs exemptions are explicit.
- NamingCatalogue: kind, limits, allowed separators/charset, source URL/date, applicability
  (tags/labels/metadata/inventory), evidence status; an unknown kind refuses qualification.
- OrganisationTemplate: ordered segments, separator/case, abbreviations, per-kind override,
  truncation strategy and algorithm version. LabelSchema: namespace, required keys,
  hierarchy derivation, allowed values and authorisation-owned keys.
- NamingResult: names map, name_short, labels; no provider ids or invented URNs. Import
  override is explicit and wins; profile/release label changes do not rename resources.
Validation: strict YAML, duplicate/unknown fields rejected; no silent truncation/collision;
managed-by, managed-in, instance and release are mandatory for applicable resources.
