# Offline command and report contracts
All commands are planned and must be created by tasks.md; no acceptance check runs yet.
Host entry: `<approved-absolute-path>/lz-offline --candidate <checkout> -- task <target>`
(T003). Its independently reviewed source/binary digest and installation live outside the
candidate. It ignores candidate Taskfiles/includes/hooks/config on the host, fetches/extracts
rooted candidate data safely, then enters isolation. Task/Go invocations below are child
commands; no advertised shortcut may load a candidate Taskfile to reach this boundary.

- `task test:foundation-ci`, `task ci:foundation` (T023): only after T001–T009 have evidenced
  completion, test/bootstrap the implemented foundation on GitHub. Exact Action pins,
  reviewed workflow/launcher and image digests, qualified pre-execution source admission, ephemeral hosted
  runner, read-only outer fetch, network-isolated candidate execution and 10-minute timeout.
  No cloud/deployment/secret-bearing environment. Require actual matching PR-head run/check
  evidence and behavioral red/green controls; missing/stale/skipped/zero-discovery results
  block merge. The proposed source gate is controlled independent source publication with Actions disabled and
  candidate publication suspended, then active no-bypass push rules restricting all
  `.github/workflows/**` before candidate pushes. Actual initial source publication, candidate admission and final rebase-merge under the freeze
  and second unreviewed source push, new automatic YAML, rename/deletion/edit denial must
  precede runner execution. Read back Actions disabled before source publication, then
  active rules/no bypass and frozen source before enabling; no pending candidate run. T023
  creates/probes this setup first in a bounded disposable repository with finally cancellation,
  disabled execution and recorded-resource cleanup. Target failure/source update retains
  disabled Actions and suspended publication until requalified. Record rules and bypass metadata plus approved source closure.
  Frozen YAML executes only pinned approved host code; source changes suspend candidate
  publication and repeat qualification. Entitlement or matching-source inspection afterward
  is insufficient. No source workflow/ruleset is qualified yet; failure blocks T023 and merge.
  This does not close full both-forge/fork/publisher qualification.
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
- `task lint -- modules/naming`: real pinned fmt/check, mirror-only init with readonly lockfile (no direct/network installation fallback), validate JSON
  and configured built-in tflint checks; report exact scanned files and reject no discovery.
- `task test:static`: real valid, unformatted, malformed and linter-offence fixtures;
  deleting/bypassing each static clause must make its control fail. Tool absence is blocked.
- `task schema:check`: strict catalogue/schema and two-org YAML fixture validation;
  malformed/unknown/duplicate field and empty discovery rejected with scan counts.
  Pure naming has no cloud-resource trivy surface: not-applicable, never empty pass.
  Docs-only checks remain exempt; interface docs receive content review.
- `task test:reports`, `test:traceability`, `test:dependencies`, `test:agentex`: valid control,
  concrete violation, crash/missing input and every-clause sensitivity cases. Dependency
  controls separately cover aliases, generated files, nested subdirectories, external sources
  and generated instances; each missed/unclassified edge fails or widens to full selection.
- `task check:specs`, `task dod -- <path>`: mapped requirement table; statuses pass/fail/
  blocked/not-run/review-required/exempt. A non-pass required item exits nonzero.
- `task test:naming`, `test:naming-policy`, `snap:check -- modules/naming`, `generate:check`:
  independently checked algorithm, handwritten evaluator/policy projections against shared
  cases with independent expected results, and generated-documentation freshness; no cloud calls
  or shared evaluator/test generator.
  The detailed V004–V006 matrix in ../../docs/explanation/naming-and-labelling-design.md
  includes same-kind logical IDs, strict decoding, stable complete recipes, scoped collisions,
  exact imports, target metadata/annotation projections and stable selectors. Call-interface
  cardinality is scalar with shared context; module splitting needs concrete-consumer diagnosis.
- `task verify:forge-offline`: both real forge sample repos; repository tokens only in
  protected qualification runner; no secret reaches the contributor workflow under test.
- `task decision-map:check`: regenerate from ADRs, registry and paths and reject diff.
Evidence JSON schema contains status, check/requirement ids, discovered count, revision,
input digest, toolchain, environment, expected, observed and private evidence references.
JSON→JUnit conversion is a view of the same observation, not a separate success oracle.
Required empty, malformed, truncated, killed or skipped runs exit nonzero, retaining
upstream diagnostics. Raw plan/state/log output is private; sanitised summaries only.
