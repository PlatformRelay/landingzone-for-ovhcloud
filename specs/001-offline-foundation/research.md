# Research and decisions
## Tool and workflow observations
Observed 2026-10-01: installed `specify --version` is 1.0.5.dev0; init supports bundled
Bash templates and generic command files. `tofu version`: 1.10.3. `terramate version`:
0.17.1. `task --version`: 3.53.1. These are workstation facts, not project qualification.
The bundled tasks command makes tests optional unless requested; the project constitution
and overrides require tests/checks for every non-docs requirement and task.
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
## Decision: retain Anvil's trace, not its harness
Requirement→ADR→implementation→Verify→evidence comes from the local Anvil specs/README.md.
Anvil currently has no .specify constitution and explicitly uses its own spec workflow;
no code or unrelated fleet machinery is copied. Docs-only tests are exempt by user request.
## Open choice: naming and labelling interface
The configuration-vector idea is not an instruction. Scalar shared-context calls, batches keyed by
logical resource IDs and catalogue previews are compared in
../../docs/explanation/naming-and-labelling-design.md. Current recommendation is a per-resource pure
helper with reusable trusted context and ordinary caller for_each; final cardinality/module split
needs a joint decision before T013/T014. Strict decoding, stable recipe revisions, per-target
metadata projections and cross-caller collision checks are required whichever interface wins.
## Primary references
- https://github.com/github/spec-kit (workflow context; installed bundled commands govern this run)
- https://opentofu.org/docs/cli/commands/test/ (framework; capture actual pinned output before parser work)
- ADR-0002, 0003, 0008, 0019, 0021 (project choices; still Proposed).
No cloud name/tag constraint is promoted from prose to observed API behaviour here.
