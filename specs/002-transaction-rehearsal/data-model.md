# Data model: instances and transactions
- DeploymentInstance: immutable id, tenant/env/region, stage version, project id,
  stack path, backend bucket/key, credential class, consumed/published artefact contracts.
  Unique ids, canonical paths and keys; library fixture stages have no backend/provider auth.
- EffectiveDocument: strict tenant data resolved with base-controlled profile defaults,
  schema revision and digest. Request cannot supply its own authority or policy revision.
- ArtefactGeneration: producer, monotonic generation, revision, effective/input digests,
  schema version and output digest. Immutable objects plus current publication record.
- ProducerStatus: pending→applying→applied-unpublished→published; failure/crash leads
  failed/needs-reconciliation and blocks old-current reuse until independently reconciled.
- PlanBinding: exact saved-plan digest, candidate/base/policy/schema/toolchain, target id,
  effective document and consumed artefact digests; expiry and one-use operation identity.
- Reservation: request/candidate idempotency key, aggregate dimensions and quantity,
  phase (reserved/committed/released/expired), durable deadline; applying holds reservation
  until actual reconciliation, not a blind timer release that would oversubscribe.
- Transaction locks: global sorted instance-id acquisition, owner id/liveness, crash status.
  A lost owner cannot keep writing; local process death is fenced before lock recovery.
Retirement is a protected id+manifest-bound record; absence creates a tombstone, not deletion.
