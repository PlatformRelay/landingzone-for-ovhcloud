# Offline command and report contracts
All commands are planned and must be created by tasks.md; no acceptance check runs yet.
- `task test:foundation-ci`, `task ci:foundation` (T023): only after T001–T009 have evidenced
  completion, test/bootstrap the implemented foundation on GitHub. Exact Action pins,
  reviewed workflow/launcher and image digests, owned-branch push trigger, ephemeral hosted
  runner, read-only outer fetch, network-isolated candidate execution and 10-minute timeout.
  No cloud/deployment/secret-bearing environment. Require actual matching PR-head run/check
  evidence and behavioral red/green controls; missing/stale/skipped/zero-discovery results
  block merge. This does not close full both-forge/fork/publisher qualification.
- `task verify:toolchain`: versions/pins and required cache/image identity, fail on mismatch.
- `task check`: credential-free prepared image, no network, required L0–L4 applicable to
  the selected artefact. Unknown changed scope selects full suite or fails explicitly.
- `task test:offline-boundary`: probe process and subprocess reachability/credential mount
  absence in that same execution boundary; a permitted outbound call fails the check.
  The protected base-revision launcher isolates the candidate before any candidate code runs;
  mutate Taskfile/workflow/include, mount/socket and preparation paths separately. Host fetch
  and result-publishing tokens never cross the boundary. Ordinary candidate CI cannot replace
  the required trusted publisher result; certify parent-run execution only, not fork hosts.
- `task verify:latency`: run the entire applicable naming check suite with discovery counts on
  an exclusive self-hosted runner with fixed CPU/memory and digest-pinned OS/image after
  preparation; unknown allocation or overlapping jobs blocks measurement; >120 seconds or
  missing layer fails. Hardware/image/cache identity is required in evidence.
- `task lint -- modules/naming`: real pinned fmt/check, prepared-cache init, validate JSON
  and configured built-in tflint checks; report exact scanned files and reject no discovery.
- `task test:static`: real valid, unformatted, malformed and linter-offence fixtures;
  deleting/bypassing each static clause must make its control fail. Tool absence is blocked.
- `task schema:check`: strict catalogue/schema and two-org YAML fixture validation;
  malformed/unknown/duplicate field and empty discovery rejected with scan counts.
  Pure naming has no cloud-resource trivy surface: not-applicable, never empty pass.
  Docs-only checks remain exempt; interface docs receive content review.
- `task test:reports`, `test:traceability`, `test:dependencies`, `test:agentex`: valid control,
  concrete violation, crash/missing input and every-clause sensitivity cases.
- `task check:specs`, `task dod -- <path>`: mapped requirement table; statuses pass/fail/
  blocked/not-run/review-required/exempt. A non-pass required item exits nonzero.
- `task test:naming`, `test:naming-policy`, `snap:check -- modules/naming`, `generate:check`:
  independently checked algorithm, shared data projections and freshness, no cloud calls.
  The detailed V004–V006 matrix in ../../docs/explanation/naming-and-labelling-design.md
  includes same-kind logical IDs, strict decoding, stable complete recipes, scoped collisions,
  exact imports, target metadata/annotation projections and stable selectors. Call-interface
  cardinality/module split is pending joint decision before implementation.
- `task verify:forge-offline`: both real forge sample repos; repository tokens only in
  protected qualification runner; no secret reaches the contributor workflow under test.
- `task decision-map:check`: regenerate from ADRs, registry and paths and reject diff.
Evidence JSON schema contains status, check/requirement ids, discovered count, revision,
input digest, toolchain, environment, expected, observed and private evidence references.
JSON→JUnit conversion is a view of the same observation, not a separate success oracle.
Required empty, malformed, truncated, killed or skipped runs exit nonzero, retaining
upstream diagnostics. Raw plan/state/log output is private; sanitised summaries only.
