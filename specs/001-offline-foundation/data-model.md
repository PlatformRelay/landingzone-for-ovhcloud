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
- NamingResult: scalar vs batch cardinality and final field names are pending joint decision.
  A result represents one logical resource; batches key by immutable logical request ID, never kind
  or generated name. Names, canonical metadata and target labels/annotations/tags are separate;
  no provider ids or invented URNs. Import override stays exact; metadata updates never rename.
  Name-affecting algorithm/template/abbreviation/catalogue revisions are frozen together.
Validation: strict YAML, duplicate/unknown fields rejected; no silent truncation/collision;
managed-by, managed-in, instance and release are mandatory for applicable resources.
Typed HCL conversion is not strict decoding. Unknown name/security inputs block authoritative
apply preflight; projection/selector constraints are independent of canonical metadata.
