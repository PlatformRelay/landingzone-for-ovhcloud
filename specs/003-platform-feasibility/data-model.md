# Data model: approved experiments
- RunConfig: immutable approved revision, exactly one baseline sandbox project id, region/product list,
  credential/cleanup classes, numeric per-spike runtime/exposure ceilings, explicit refutation stops, freshness/reservation/lag bounds,
  per-credential residual windows, scoped authorization and protected candidate digest.
- RunLease: external id, candidate/config digests, owner/liveness, max exposure, deadline,
  status admitted/running/cleanup-required/cleaned/blocked; idempotent admission/retries.
- ResourceRecord: lease/request id, plane/project/region/id, creation intent and observed
  response, state-writer identity, cleanup action and status. Retained outside fixture state.
- RecoveryPackage: fixture checksum, encrypted backup id/key method, versioned backend,
  resource ids/import manifest, toolchain and independently usable sealed authenticators.
- PrincipalRoute: issuer, authenticator class, credential type, bound resource/actions,
  lifetime/refresh/revoke and approved maximum residual window; observed positive/denial
  pairs and floor-removal/tag-envelope/child-operation subcontrol status. Any missing
  subcontrol blocks the corresponding authority claim. A denied OVH route does not
  imply a denied Keystone/S3 route.
- Qualification: supported experiment scope, evidence digests, costs, cleanup status,
  independent review and pass/refuted/blocked result. No tuple promotion by inferred coverage.
Numeric gate values are external approved inputs, validated before live implementation use.

IAMLimitObservation: object type, current count, limit/source/revision or UNVERIFIED;
PropagationObservation: route/action, bounded apply-to-effective elapsed time and repeated
positive/negative observations. Unmeasured values never become guarantees.
