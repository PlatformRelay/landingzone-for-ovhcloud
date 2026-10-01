# Research and decisions
## Workflow baseline
Bundled Bash templates and generic command documents are vendored with their source
revision and licence in .specify/UPSTREAM.md. Project overrides require predefined
verification for behavioral work; workstation versions are private observations.
## Decision: a prepared offline boundary
Rationale: removing OVH_* variables cannot block a provider or external subprocess from
making calls. Prepare checksum-verified tools/provider cache separately; run the suite in
a pinned no-network image without host credential mounts. Alternative env-only mode rejected.
## Decision: candidate-independent CI launcher
Use a protected base-revision verification workflow, maintainer-dispatched against a candidate
SHA. Fetch its archive as data and validate extraction paths; execute all candidate commands
in the prepared isolated image. Fetch/publish tokens stay in the outer trusted job. No candidate
workflow or Taskfile controls mounts, image or launch arguments. Required checks bind expected
publisher and candidate digest; qualify actual enforcement on both forges. Alternative trusting
a candidate `task check` to enter its own isolation rejected. Parent-run checks only are certified.
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

## Upstream input trace
Naming reference inputs: the pinned scalar context example in ../../docs/reference/upstream-reference-map.md is a comparison input for later T013–T016/V004–V006, not a released schema. Use independent expected vectors, not copied generated outputs. These tasks remain outside the foundation run.
