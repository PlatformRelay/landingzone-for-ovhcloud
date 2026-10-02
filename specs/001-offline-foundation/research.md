# Research and decisions
## Workflow baseline
Bundled Bash templates and generic command documents are vendored with their source
revision and licence in .specify/UPSTREAM.md. Project overrides require predefined
verification for behavioral work; workstation versions are private observations.
## Decision: a prepared offline boundary
Rationale: removing OVH_* variables cannot block a provider or external subprocess from
making calls. Prepare checksum-verified tools/provider filesystem mirror and lockfile separately; run the suite in
a pinned no-network image without host credential mounts and explicit mirror-only provider
installation (no direct/network fallback). A plugin cache alone is not offline installation.
The host entry is an approved absolute-path `lz-offline` installed outside the candidate
from independently reviewed source/binary digest; candidate Task/Go commands are child
operations only. Tests must enter this real host entry with malicious checkout data.
T002's initial boundary stub/test driver is explicitly reviewed before its bounded proof. Alternative env-only mode rejected.
## Decision: candidate-independent CI launcher
Use a protected base-revision verification workflow, maintainer-dispatched against a candidate
SHA. Fetch its archive as data and validate extraction paths; execute all candidate commands
in the prepared isolated image. Fetch/publish tokens stay in the outer trusted job. No candidate
workflow or Taskfile controls mounts, image or launch arguments. Required checks bind expected
publisher and candidate digest; qualify actual enforcement on both forges. Alternative trusting
a candidate `task check` to enter its own isolation rejected. Parent-run checks only are certified.
## Deferred GitHub source-admission hypothesis (C2 / KI-001)
The 2026-10-02 operator priority exception (local decision D60) accepts C010.5/P4 as
DEFERRED, nonblocking [KI-001](../../docs/known-issues/KI-001-ci-source-admission-unqualified.md)
for current private maintainer development, publication, review PR creation and merge.
The hypothesis below is retained for later qualification, outside minimal T023.
Whole-feature hardened guarantees remain unqualified; the constitution and ADRs are
unchanged. Review PR creation does not establish merge readiness: T001–T009 evidence,
independent review, exact pins, read-only CI authority, T003 isolation and actual exact-head
CI remain required for merge.
Owned-branch push filters do not select trusted workflow source: GitHub resolves YAML from
the event SHA/ref. Manual dispatch also resolves its ref and requires a default-branch
workflow; changing the event alone cannot close this gate. Post-run digest review is too late.
The deferred KI-001 hypothesis proposes one independently approved source publication on a ref that cannot execute
candidates with Actions disabled and candidate publication suspended, followed by an active no-bypass push
ruleset restricting every workflow path before candidate admission. Frozen YAML invokes only
full-SHA/digest-pinned approved host code. Qualification must accept the exact intended
initial source publication, candidate history and final rebase-merge under the repo-wide freeze and reject a second unreviewed push, added YAML and edit/rename/deletion
through available push/API paths before any runner executes. Read back Actions disabled before publication, then active rules/no bypass and source
closure before re-enabling. Later KI-001 qualification probes this setup on a disposable repository first,
with bounded finally cleanup; failed setup/source updates retain disabled execution until
requalified. Source updates repeat the gate.
GitHub documents private push rulesets for Team; plan availability is not enforcement proof.
No source/rule is already qualified. Failure blocks the hardened source-admission claim,
not minimal T023 or current private maintainer publication/PR/merge under this exception;
a protected external/base-source evaluator plus scoped publisher is an alternative requiring
separate setup/authority disposition, not a claimed existing fallback.
## Decision: one pure module first
Rationale: names/labels are required by D2 and exercise reporting, schemas, contracts,
snapshots and policy without cloud charges. Cloud applicability is gated on evidenced rows;
synthetic constraints are allowed only for algorithm tests. Alternative broad scaffold rejected.
## Decision: explicit traceability, documentation exemption
Requirement → ADR → implementation → Verify → evidence is the portable contract.
Each link must describe the actual task; unrelated spec-wide lists are refused.
Pure prose needs content review rather than invented behavioral tests.
## Decision: per-resource naming and independent projections
The configuration-vector idea is not an instruction. Scalar shared-context calls, batches keyed by
logical resource IDs and catalogue previews are compared in
../../docs/explanation/naming-and-labelling-design.md. The operator selected a per-resource pure
helper with reusable trusted context and ordinary caller for_each on 2026-10-01. Callers/catalogue
checks own cross-resource collision detection; a scalar call cannot inspect other calls. Diagnose
module splitting against concrete consumers before the later naming work. Handwrite evaluators
and policy projections against shared cases with independently specified expected results;
initial generation is documentation only, never a shared evaluator/test generator. Strict
decoding, stable recipe revisions and per-target metadata projections remain required.
## Primary references
- https://github.com/github/spec-kit (workflow context; installed bundled commands govern this run)
- https://opentofu.org/docs/cli/commands/test/ (framework; capture actual pinned output before parser work)
- ADR-0002 (Accepted), 0003, 0008, 0019, 0021 (other recorded statuses remain unchanged).
No cloud name/tag constraint is promoted from prose to observed API behaviour here.

## Source-selection references
- [GitHub workflow resolution](https://docs.github.com/en/actions/concepts/workflows-and-actions/workflows)
- [GitHub dispatch ref/default-branch requirements](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch)
- [Push ruleset availability](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets)
- [Workflow path restrictions](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets#restrict-file-paths)
- [OpenTofu mirror installation](https://opentofu.org/docs/cli/config/config-file/#explicit-installation-method-configuration)
These describe mechanisms; T003 owes pinned-runtime/isolation evidence and T023 owes
actual minimal CI evidence. Source denial/admission evidence remains deferred to KI-001.

## Upstream input trace
Naming reference inputs: the pinned scalar context example in ../../docs/reference/upstream-reference-map.md is a comparison input for later T013–T016/V004–V006, not a released schema. Use independent expected vectors, not copied generated outputs. These tasks remain outside the foundation run.
