# Tasks technical review

```text
REVIEW
Verdict: REQUEST CHANGES
Scope: initial Spec Kit scaffold and the three draft spec/plan/task sets
Branch: codex/first-phases
Base: 116405b98960dcc78b3843985a4a49f35afae08a
Date: 2026-10-01
Findings: 0 P0, 0 P1, 2 P2
```

This review was conducted independently from the current artifacts, without reading
the plan-adversarial reports. It assesses whether the draft tasks implement the
declared checks and sequencing; it does not qualify any future product behavior.

## Required changes

### P2 — Assign and verify the promised L0 checks before the full-suite gate

Location: `specs/001-offline-foundation/tasks.md:123`–`130` (T020–T021);
contract: `specs/001-offline-foundation/contracts/checks.md:4`–`5`.

The command contract requires the applicable L0–L4 checks, and T020/T021 reject an
omitted layer when measuring the complete naming suite. The tasks assign naming
unit/contract tests, snapshots and policy checks, but no task creates or verifies
the applicable L0 static checks. Across this feature's spec, plan and tasks there
is no `tofu fmt -check`, `tofu validate`, static linter invocation, or corresponding
static-check creator/control. T009 creates dependency selection; T021 creates the
aggregate and latency check. Neither assigns the missing static implementation.
ADR-0008's L0 contract makes syntax/type/format and interface checks part of the
module's cheap verification, rather than an optional later phase.

As written, an implementer can assemble the named targets and record a layer
count while leaving basic naming-module static checks undefined, or discover at
T021 that its required aggregate cannot be built from the preceding tasks.

Smallest fix: extend a foundation task, or add a test/implementation pair, to
define the first module's exact applicable L0 commands, their configured paths,
creators and report/discovery controls. Include a real malformed/unformatted HCL
negative control and a valid control. Make the latency/exit tasks depend on this
work and consume its actual results. Explicitly defer any ADR L0 checks that are
inapplicable to this bounded module instead of counting an empty layer.

### P2 — Keep absent forge adapters from blocking the local transaction MVP

Location: `specs/002-transaction-rehearsal/tasks.md:14`–`20` (T001–T002).

T001 requires tool/adapter availability and says a missing adapter yields blocked
qualification; T002 depends on T001, and the entire local roots/graph/transaction
chain descends from T002. Consequently the known missing assent forge adapter can
stop strict decoding and the provider-free local rehearsal before it starts.
This contradicts `plan.md:13`–`15` and `plan.md:77`–`78`, which gate real-forge
qualification on the adapter, and `research.md`'s explicit allowance for local CEL
fixtures while that external dependency is unavailable. T019 already carries the
appropriate both-forges adapter/setup prerequisite.

Smallest fix: scope T001's completion to exact Terramate and assent CLI pins and
tool-owned offline fixtures; record adapter availability separately without
requiring an absent adapter for that task to close. Keep actual adapter gates on
T018/T019 and V007. If fixture capture itself requires an unavailable dependency,
split the Terramate/local preparation from the assent/forge preparation and wire
only the consumers that need each result.

## Checks actually run

- `git diff --cached --check`: passed. Root README and ignore changes were reviewed;
  the additional private `.local/` directory matches the evidence policy.
- `bash -n` over all six `.specify/scripts/bash/*.sh`: passed.
- Python JSON decoding of all seven `.specify/**/*.json` metadata files: passed.
- `resolve-template.sh <spec|plan|tasks>-template --json`: all three resolved
  contents exactly match the project overrides, including mandatory non-docs
  verification and the docs-only exemption.
- In a disposable copy of the scaffold and each feature, ran
  `check-prerequisites.sh --json --require-spec --require-tasks --include-tasks`
  and `setup-tasks.sh --json`: passed with the expected feature and override paths.
  Removing the required `spec.md` and `tasks.md` separately made the prerequisite
  gate exit nonzero with the appropriate missing-file diagnostic. These are real
  prerequisite violations, not missing-binary or syntax failures.
- In that disposable copy, `setup-plan.sh --json` preserved existing plans and
  copied the project override when the plan was absent. Metadata-writing scripts
  were not run against the reviewed worktree.
- Independent task parsing found 66 sequential, unique, unchecked tasks, each
  with Requirements, Depends on, Verify, Evidence and initial `not-run` metadata;
  four have explicit docs-only exemptions. All 26 FRs and nine SCs appear in the
  23 acceptance rows. All acceptance commands, including commands following
  semicolons, have planned creator entries: 15 in 001, nine in 002, ten in 003.
- Reconstructed local and cross-feature task dependencies: 66 nodes, no missing
  referenced task and no cycle. Checked the live ordering: sandbox canary/fault
  exit precedes later live branches; native recovery precedes floor binding;
  old-writer denial precedes replica activation; aggregate qualification depends
  on both state/identity results and phase-002 evidence.
- Version observations repeated locally: Specify `1.0.5.dev0`, OpenTofu `1.10.3`,
  Terramate `0.17.1`, Task `3.53.1`. The recorded OpenTofu/Terramate qualification
  gaps are accurate. Confirmed `Taskfile.yml`, `mise.toml`, `tools/go.mod` and
  `modules/naming` are absent, as the draft documents state.

## Review dimensions and limits

The task sets require compiling stubs and behavioral red controls before
implementation, explicitly rejecting missing-tool/compile/outage failures as TDD
evidence. Their positive and negative cases meaningfully cover report corruption,
zero discovery, evidence forgery, naming stability, strict decoding, publication
faults, approval-field mutations, reservations, cross-tenant authority, encryption,
floor self-removal, already-issued credentials and cleanup failure. Requirement
ID coverage alone was not treated as proof that these behaviors exist.

The security design separates the immutable host launcher from candidate code,
keeps publisher tokens outside offline execution, scopes live authority, protects
cleanup from admission failure, and refuses simulated forge/cloud support claims.
Recovery distinguishes independent decryption from same-key replication and
requires writer fencing. These are planned contracts with adequate stated stop
conditions; their platform mechanisms remain unverified until the named probes.

Go helpers live under the single future tools module; root tests contain fixtures,
HCL and data. No nonexistent Go package or Task target was executed. No cloud
credential, account mutation, purchase, destructive probe, forge mutation or
outbound message was used. Full product lint/tests, live discovery separation,
actual isolation, tool-output parsers, race safety, costs and forge publisher
enforcement cannot be demonstrated from a planning-only lane. They remain owed
implementation evidence, rather than review failures caused by absent code.

Approval can follow after the two task-definition gaps are corrected and the
changed planning/scaffold checks are rerun.
