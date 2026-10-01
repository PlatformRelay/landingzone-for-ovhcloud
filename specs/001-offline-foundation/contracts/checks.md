# Offline command and report contracts
All commands are planned and must be created by tasks.md; no acceptance check runs yet.
Host entry: `<approved-absolute-path>/lz-offline --candidate <checkout> -- task <target>`
(T003). Its independently reviewed source/binary digest and installation live outside the
candidate. It ignores candidate Taskfiles/includes/hooks/config on the host, fetches/extracts
rooted candidate data safely, then enters isolation. Task/Go invocations below are child
commands; no advertised shortcut may load a candidate Taskfile to reach this boundary.

## Preparation and capture order

T001 selects pins and records version/provenance metadata; it does not implement or
qualify tool-output fixture capture. Its independently approved preparation source must
refuse missing/wrong tool or module identity before finalization/use. It also owns the
preparation trust, configuration and cleanup controls below. T002 authors capture/isolation
tests with valid controls and compiling behavioural red stubs; T003 implements admission
and repeats those controls green through the actual approved entry. Matching pinned
tool/artifact identities **and** an evidenced capture/isolation gate are mandatory before
any tool-output fixture capture, including T004. A preparation metadata status or a test
driver's self-report cannot grant capture permission.

The following drivers are planned deliverables, not existing executable prerequisites:

- **T001 preparation control driver:** author only the bounded driver/control inputs under
  ignored `.local/preparation/001/`; keep the already approved preparation source unchanged.
  Freeze source, explicit input allowlist/digests, runtime, build recipe and external
  installation paths for independent approval **before any build or execution**. Install
  only that approved closure outside the candidate; approval of the old preparation source
  alone does not approve this driver or any input injection seam. Run the unchanged subject
  in disposable private scratch with its fixed source/candidate/destination paths mapped
  to private copies from an explicit allowlist, never a wholesale checkout copy. Controlled
  artifact-response delivery and tool/config/path fault placement must be explicit in the
  reviewed driver closure; no presumed injection seam. Preserve real checksum/signature/
  publisher verifiers and identity commands as the subject. Synthetic transport responses
  are control inputs, not evidence of upstream transport. Do not replace verifier outcomes
  or install/configure anything in shared host paths. Prove a valid preparation using the
  same delivery mechanism before accepting tamper rejection; outage/timeout/missing runtime
  cannot count as rejection. If exact-source delivery cannot be implemented safely, T001
  remains blocked until a separately reviewed source/seam correction, not a substituted
  subject. Each negative run observes refusal before the affected tool is used or final
  inputs/tools/pass evidence are published; failure removes private staging in finally
  and leaves no final outputs. Image rejection may follow extraction into private staging,
  but must precede tool identity execution/finalization. Place synthetic ancestor, system
  and candidate mise configuration with executable marker hooks in private mappings and
  verify those markers never run. Extra inputs cannot widen the approved closure. Record
  exact subject/driver/input identities, refusal reason/timing and cleanup observations;
  separately approve and label guard-removal mutants before execution to prove sensor
  sensitivity, never pass a modified subject off as the approved production source.
- **T002 boundary proof driver/stub:** author tests and minimal compiling stubs in the
  named tools paths, then independently approve the explicit driver/stub/test source,
  build dependencies and private synthetic inputs before any build/execution. Build and
  install externally from the approved allowlist, never from the hostile fixture checkout.
  The driver exercises that checkout as data in disposable private scratch with synthetic
  credentials/markers and a controlled reachable outbound responder; retain actual denial
  reasons and isolation-off sensitivity. Valid controls plus behavioural red are required;
  absent tools or compilation failures do not qualify. It cannot admit real fixture capture
  and cannot be reused as production admission. T003 separately approves/builds/installs
  the actual entry, repeats the complete suite green and proves capture refusal before
  tool invocation/publication. Candidate code cannot select, build or replace a host driver.

| Control / prior T001 matrix row | Test/control creator | Required proof owner and use barrier |
| --- | --- | --- |
| Exact signed/checksummed tool, module and image identities; valid preparation (1–4, 7) | T001 preparation closure/driver | T001 positive identities/provenance before T002 build |
| Missing/wrong tool/version/module before preparation finalization | T001 preparation control driver | T001 concrete refusal and cleanup before T002 build |
| Missing/wrong tool/artifact/image or unproven capture/isolation gate before fixture capture (5) | T002 tests and compiling stubs | T002 valid/red; T003 actual-entry green/rejection and gate-off mutant before T004 capture |
| Reviewed source/path and bounded host closure (6, 10) | T001 preparation control driver | T001 source approval, false-source/wrong-path refusal before setup |
| Tampered manifest/signature/archive/image trust data; failure cleanup (8) | T001 preparation control driver | T001 valid delivery/rejection/sensitivity/cleanup before T002 build; T003 repeats for any new preparation closure before use |
| Extra inputs; hostile ancestor/system/candidate config (9) | T001 preparation control driver | T001 no expansion/marker execution and clause sensitivity before T002 build; T002 red/T003 green separately cover actual launcher attacks |
| Credential/socket/cache mount, outbound child, candidate Taskfile/include/hooks/launcher attacks | T002 boundary tests/driver/stub | T002 valid/red; T003 actual-entry green/rejection before candidate checks/capture |
| Provider mirror/lockfile/hash and network fallback | T003 preparation and boundary | T003 real mirror-only init positive/rejection before provider fixture use |

No row is waived by this ordering. T001 preparation controls remain required for T001
closure; T002/T003 own capture admission because they create its tests/implementation.
T003 results do not close later latency/forge/CI duties. CI publication/merge remains
blocked on qualified independent evaluation source; candidate self-reports are insufficient.

## Planned child targets

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
- `task verify:toolchain` (T003): versions/pins and required cache/image identity, fail on mismatch;
  capture admission requires those identities and proven isolation, not preparation metadata alone.
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
