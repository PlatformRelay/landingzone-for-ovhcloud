# Feature Specification: Answers file to deployments.yaml
Feature: `004-guided-preconfiguration` · Created: 2026-10-01 · Refreshed: 2026-10-25 (D127) · Status: draft (state of 2026-10-25 below)
ADRs: 0005, 0008, 0013, 0019, 0021, 0023 (also read: 0003 naming, 0004 instances)
Input: D127 (the wizard gate opens on 2026-10-25 with a re-scoped spec 004) and its dissenting
judge's counterpoints; the strict manifest decoder `tools/internal/stacks/manifest.go` behind
`task stacks:check`; `schemas/deployments.schema.json` (v1alpha1); the two-tenant fixture
`tests/fixtures/manifests/sandbox-two-tenants/` (spec 007 T017); constitution 1.3.0.

## State (2026-10-25)
- **Built and merged:** the strict answers decoder and the export through the manifest decoder (US1:
  T016, T017; #171); the refusal catalogue, the dry-run row diff and the consequence notes (US2,
  US3: T018, T019; #172). Every task closed with its evidence under `evidence/`.
- **Built on `lane-004`, not yet merged:** the `lz-configure` command with its staging write and the
  `configure:check` and `configure:export` targets (T020), and the second operator's how-to
  `docs/how-to/prepare-deployments-from-answers.md` (T021).
- **T022 is blocked:** no second reader has been named; only the owner names one
  (`evidence/T022.md`). SC-004 is not met, and D-US4's first trigger (the answers file itself was
  the obstacle) is undecided.
- **Deferred stories:** none fired. D-US4 waits on T022; D-US5 needs D-US4; D-US7 needs D-US4 or a
  reported missing-tool failure after a clean export, and none is recorded; D-US6 needs a second
  profile in the manifest schema.
- **CI:** `test:configure` is not yet a target of the CI workflow, so CI does not run this spec's
  tests. It is added with the next re-record of `pipelines/github/foundation-runs.json`, after this
  spec's burn-down; until then `ci:foundation` reports its recorded runs stale (`evidence/T023.md`).

## History
- **D29 (2026-10-01):** a bounded renderer prototype and a written journey first; persistence and
  export wait for real profile schemas. T014 wrote the journey (closed 2026-10-02).
- **D61 (2026-10-02):** widened eligible work to all implementable specs; D29's gates stayed.
- **D85 (2026-10-06):** postponed the wizard with the PoC staging; every wizard task held.
- **D113 (2026-10-14):** D29's schema gate is met by `schemas/deployments.schema.json`; the export
  target is that schema; the live consequences of spec 005 must be explained; the D85 gate stayed.
- **D127 (2026-10-20):** the D85 gate opens on 2026-10-25. Scope starts at the simplest form: a
  non-interactive answers file exported to `deployments.yaml`, gated by `stacks:check`. The
  interactive terminal and save/resume layers are later, optional slices. Burn-down capped at about
  15 strong review legs. This refresh replaces the 13-task terminal/save/resume plan: task ids
  T001–T013 are retired unstarted, T014 stays closed, new tasks start at T015.
- **Spec-set review round 1 (2026-10-25, on `463520a`):** the diff is cut to row additions and
  removals, the answers schema file is dropped, the catalogue is keyed by source and code, and the
  staging write has a publication boundary ([loop.md](loop.md)).

## Why and scope
Today one maintainer writes `stacks/deployments.yaml` by hand and checks it with `task stacks:check`.
That works for him. It is a steep entry for a second operator: 41 values for the one-tenant sandbox
and 67 for two tenants. The instance rows hold 21 and 38 of those values, and they follow fixed
rules that a person has to apply by hand. The decoder's refusals are codes such as `DUPLICATE_VLAN`
or `SCOPE_VIOLATION`.

This spec adds a short **answers file** and one credential-free command that turns it into a
complete `deployments.yaml`. The command checks the result with the same strict decoder that
`stacks:check` uses. It explains every refusal in plain words with the next action, shows a dry-run
diff against today's manifest, and lists the consequences of the choices (D113). The written file
lands in local staging; the operator copies it and runs the existing `stacks:reconcile`,
`stacks:generate` and `stacks:check`.

**What the answers file makes easier than editing `deployments.yaml`** (the D127 counterpoint):
- fewer values: 11 for the sandbox and 15 for two tenants, against 41 and 67 (research R2);
- no instance rows: ids, stages, dimensions and tenant-state rows are derived;
- defaults for the fields that rarely change (header, scope, stage source, state ref and endpoint,
  project ref and mode, VLAN 0, both toggles off);
- each refusal explained in a plain sentence with the next action;
- a dry-run diff of the instance rows added and removed, each marked with what `stacks:reconcile`
  is expected to do, judged against the current manifest.

**Where it does not help** (stated, not hidden): a maintainer fluent in the manifest gains little
beyond the diff and the notes. The diff shows rows only: a changed CIDR, VLAN id, toggle or header
value is not listed, and its prediction assumes the stack tree matches the current manifest. A
manifest that uses anything outside the answers file (tenant
scope, `external` entries, runtime slots, a region without a runtime row, custom instance ids) is
still edited by hand. Renames and removals stay refused by `stacks:reconcile` either way. The
project ids still go into `account.env` by hand; the command only lists the refs. Comments of a
hand-edited manifest are not carried over. The flow-form syntax is still JSON-like.

**In:** the answers file format and its strict decoder; defaults and derivation; the decoder gate; plain
messages; the dry-run diff; consequence notes; a staging write; a how-to; tracing spec 004.

**Out** (see *Deferred user stories* and *Out of scope*): the interactive terminal journey, save and
resume, use-case presets, local tool checks, any write to `stacks/`, any cloud call.

## Clarifications

### Session 2026-10-25
Answered as the owner's delegate from D127, D113, ADR-0023 and the code at `46f5115`. One item is
left open, with the default this spec applies.
- Q: Which form does the answers file take? → A: YAML in its JSON-compatible flow form with
  whole-line comments, decoded strictly, exactly as the manifest is (`manifest.go:6-10`). Block YAML
  would need a vendored YAML library, a dependency decision this spec does not take. Counterpoint
  kept: flow form is less friendly to a newcomer than block YAML; the starting file and the plain
  messages carry that cost.
- Q: Is the answers file committed? → A: No. `stacks/deployments.yaml` stays the one source of truth.
  The answers file is a local input; the how-to suggests `.local/configure/answers.yaml`. Two
  committed sources would drift.
- Q: Which manifests can the answers file express? → A: platform scope only, `stage_source` local,
  every region with a runtime row, no slots, no external entries. Everything else is hand-edited.
- Q: Does the command read `account.env` or any credential? → A: No. It lists the
  `LZ_PROJECT_ID_<REF>` names the account file needs, never values (FR-006, FR-008).
- Q: Is the export byte-identical to the committed manifest? → A: No; it is value-equal: with
  whole-line comments removed, both parse to the same JSON value (same keys and values, arrays in
  the same order). Comments and formatting differ. Value-equality, not equality of the decoded
  `Manifest`, is the test: `Manifest` drops CIDRs, VLAN ids and toggles, which Terramate reads from
  the file itself (`stacks/_lz/globals.tm.hcl:5`).
- Q: Does the command re-implement the decoder's cross-row rules to name the place? → A: No. The
  decoder is the only judge of the generated manifest; its refusal detail already names the tenant,
  environment and region, and the message adds the plain sentence and the next action.
- Q: Do the interactive journey and save/resume stay in this spec? → A: As deferred user stories with
  triggers (D127), not as tasks.
- **Open, owner's, with default** — [NEEDS CLARIFICATION: may `configure:export` write
  `stacks/deployments.yaml` in place (behind a flag), or only the staging file?] Default: staging
  only (`.local/configure/deployments.yaml`), as ADR-0023 *Reviewable output* says ("applying that
  bundle to the repository is a separate reviewed action"); the command prints the copy step.
  Writing in place would need an ADR-0023 amendment.

## Decisions and proposals
- **Approved direction:** D127 (scope, cap, deferred layers); D113 (export target and consequences);
  ADR-0023 (noninteractive input shares the validation; staging; no authority); ADR-0005 (strict
  decode; `deployments.yaml` is platform-controlled).
- **Proposals, with alternatives** (detail in `research.md`):
  - a new package `tools/internal/configure` and command `tools/cmd/lz-configure` (R1), rejecting
    a subcommand of `lz-stacks` (it would mix the generator's trust boundary with input convenience);
  - derive every instance row by one fixed rule (R3), rejecting an answers field per row;
  - check the export by calling `stacks.DecodeManifest` in-process (R4), rejecting a call of
    `task stacks:check` (that judges the candidate tree, not a staging file);
  - a message catalogue keyed by source and refusal code (the answers file and the command, or the
    generated manifest), checked complete against the decoder's `Code*` constants (R5);
  - no published answers schema: the Go types and data-model.md are the definition, and a test
    checks the shared fields' patterns against `schemas/deployments.schema.json` (review round 1).

## User Scenarios & Testing

### User Story 1 — A second operator exports a manifest from a short answers file (Priority: P1)
A second operator copies the sandbox answers file, adds tenant `lab` with environment `dev`, region
`GRA11` and a CIDR, and runs `task configure:check -- .local/configure/answers.yaml`. The command
prints the dry-run diff (four new rows for `lab`, each "expected: created by stacks:reconcile"),
the consequence notes (`LZ_PROJECT_ID_LAB_DEV` is needed in `account.env`; one project per tenant
and environment, and `LAB_DEV` on the project of `DEMO_DEV` is refused at bind (A17); KD-1 covers
`STATE` only) and `configure: ok`. `task configure:export -- …` then writes
`.local/configure/deployments.yaml` and prints the next steps. After the copy, `stacks:reconcile`,
`stacks:generate` and `stacks:check` pass.
**Independent test**: V001, V002, V004, V005. Failure: if the decoder refuses the export, nothing
is written and the previous staging file stays.

### User Story 2 — A mistake is explained in plain words (Priority: P1)
The operator gives two regions of `lab/dev` the default VLAN 0, writes `"cidr": "10.40.0.0"`, or
misspells `environments`. The command prints one line with the code, where it is, a plain sentence
and the next action, for example
`configure: DUPLICATE_VLAN: vlan_id 0 of lab/dev/SBG5 is also GRA11's: project LAB_DEV holds one private network per VLAN id. Two regions of one environment share a VLAN id. Next: give one region its own "vlan_id" (1 to 4000).`
(the detail is the decoder's own, `manifest.go:499-500`).
Nothing is written and the exit status is 1.
**Independent test**: V003.

### User Story 3 — The maintainer sees what would change before anything changes (Priority: P2)
The maintainer runs `configure:check` on an answers file that drops tenant `lab`. Against the
current manifest, the diff lists four removed rows marked "expected: refused by stacks:reconcile
(UNSUPPORTED_CHANGE)", a warning line says that `stacks:reconcile` would refuse the export, and the
command writes nothing. With no current manifest every row is new.
**Independent test**: V004.

### Deferred user stories (not tasks; each starts only on its trigger and within the review budget)
| Story | What it adds | Trigger to start |
| --- | --- | --- |
| D-US4 Interactive terminal journey | a renderer (Huh or Gum, ADR-0023), help, back/edit, progress, plain/no-TTY mode, PTY tests | US1–US3 done; T022 records that the answers file itself, not the manifest, was the second reader's obstacle; review budget left after the D127 cap; a renderer pin qualified under 001/T003's capture rules |
| D-US5 Save and resume | an atomic, owner-only, single-writer session under `.local/configure/` with revalidation (ADR-0023 *Save and resume*) | D-US4 exists and a journey longer than one sitting is observed; until then the answers file is the saved state |
| D-US6 Use-case presets with default provenance | learning versus production presets, explicit/default provenance, preview of changed defaults | the manifest schema admits a second profile or golden path (ADR-0016) whose defaults differ |
| D-US7 Local tool checks | bounded read-only checks of pinned tools before export | D-US4, or a reported failure caused by a missing tool after a clean export |

### Edge cases
An empty `tenants` list (valid: the two account rows only); a tenant with no environments (valid:
its tenant-state row only); an environment with no regions (valid: its project row only); a region
without `cidr` (no network row, runtime row kept); two regions with VLAN 0 in one environment
(`DUPLICATE_VLAN`); a project-ref override equal to `STATE` without `shared_state_project`
(`SHARED_STATE_PROJECT`); two overrides with one ref (`DUPLICATE_NAME`); a region name long enough
that a derived id passes 63 characters (`INVALID_VALUE` from the decoder); a duplicate key, a case
variant (`Tenants`), an unknown field, `null`, a second document, a trailing comment, block YAML; a
file over 64 KiB; a symlinked answers path, `.local`, `.local/configure` or staging file; a current
manifest that does not decode (refused by check and export, exit 1); no current manifest; a staging
write that fails before the rename (the previous staging file stays) or whose directory sync fails
after it (the new file is in place, its durability is reported uncertain); an existing
`.local/configure` looser than 0700 (tightened to 0700).

## Premises

| ID | Premise | Source | Probe | Status |
| --- | --- | --- | --- | --- |
| P1 | `stacks:reconcile`, `stacks:generate` and `stacks:check` see only the manifest's value, never its comments or layout: they read it through `stacks.DecodeManifest` and through Terramate's `tm_yamldecode` of the same file, so a value-equal export yields the same stacks | `tools/internal/stacks/reconcile.go:74-81`, `generate.go:81-88`, `tools/cmd/lz-stacks/main.go:183-187`, `stacks/_lz/globals.tm.hcl:5` | T017 (value-equality with `stacks/deployments.yaml` and the two-tenant fixture, which `test:stacks` reconciles and generates, 007 T017); the operator's `stacks:check` after the copy | read in code; believed |
| P2 | The state endpoint of region `<r>` is `https://s3.<r>.io.cloud.ovh.net` | OVHcloud kb mirror `storage-and-backup/object-storage/s3-post-object-upload.mdx`, cited in `components/runtime/managed-only/README.md:17-20`; `gra` used by spec 005's live chains | none here: the derived default is overridable | doc; observed live for `gra` only; UNVERIFIED for other regions |
| P3 | The answers file lowers the entry for a second operator | D127 (two judges for, one against) | T022, a second reader's recorded attempt | UNVERIFIED |

## Requirements

### Functional requirements
- **FR-001 Answers file, strictly decoded** — an answers file has `apiVersion`
  `lz.platformrelay.dev/v1alpha1`, `kind` `DeploymentAnswers` and the fields of data-model.md
  *Answers file*. It is YAML in its JSON-compatible flow
  form with whole-line comments, at most 64 KiB, decoded strictly through `checks.DecodeStrict`:
  syntax errors, duplicate keys, case variants, unknown or missing fields, `null`, unsupported
  versions and values outside a field's pattern or enum are refused before anything is derived.
  Shared fields use the manifest's patterns: `tools/internal/configure` keeps its own copies, and a
  test checks each against `schemas/deployments.schema.json` for the manifest field it maps to.
  Drift would fail safe (the decoder gate still refuses the generated manifest), but the refusal
  would name the manifest path instead of the answers path, so the test keeps it out. No field
  holds a project id, a credential or a command.
- **FR-002 Defaults and derivation** — the export fills every manifest field the answers file omits
  by the fixed rules of data-model.md *Derivation* (header, `scope` platform, `stage_source` local,
  `state.project.ref` `STATE`, `state.endpoint` from the region, project ref `<TENANT>_<ENV>`, mode
  `reference`, `vlan_id` 0, both toggles off) and derives every instance row in a fixed order with
  fixed ids. An explicit answer overrides only the defaults data-model.md marks overridable. The
  output is deterministic: the same answers give the same bytes.
- **FR-003 The decoder gate** — before it prints or writes a manifest, the command passes the
  generated bytes through `stacks.DecodeManifest`, the decoder behind `stacks:check`; a refused
  manifest is never printed as a result or written. The export of the sandbox answers fixture is
  value-equal to `stacks/deployments.yaml`, and that of the two-tenant answers fixture to
  `tests/fixtures/manifests/sandbox-two-tenants/deployments.yaml`.
- **FR-004 Plain-word refusals** — every refusal prints one line
  `configure: <CODE>: <detail>. <plain sentence>. Next: <action>`, where `<detail>` is the field
  path for an answers decode error and the decoder's own detail (which names tenant, environment and
  region) for a refusal of the generated manifest. The catalogue of contracts/checks.md is keyed by
  source and code: source `configure` covers every refusal code of `tools/internal/configure` and
  the command (answers file, current manifest, staging), source `generated` covers every `Code*`
  constant of `tools/internal/stacks/manifest.go` as a refusal of the generated manifest. The
  answers decoder and the manifest decoder share code strings (`DUPLICATE_KEY`, `UNKNOWN_FIELD`,
  `MISSING_FIELD`, `UNSUPPORTED_VERSION`, `INVALID_VALUE`), so the source picks the entry. Every
  `generated` code the derivation cannot reach maps to the one tool-defect entry, which says so and
  asks for a report. A (source, code) pair without an entry fails a test and never falls back
  silently. Exit status 1. The one exception is an I/O failure that is not a refusal of the
  answers: it prints `configure: error: <error>`, exit 1, with no catalogue entry
  (contracts/checks.md *Message catalogue*).
- **FR-005 Dry-run diff** — `configure:check` decodes the current `stacks/deployments.yaml` (when
  present) and prints a deterministic diff of the instance rows, by id: rows added and rows removed.
  Value-level changes (header, CIDR, VLAN id, toggles, a changed stage or dimension of a kept id)
  are not listed. The marks are a prediction made against the current manifest, assuming the stack
  tree matches it, not against the tree: each added row is marked "expected: created by
  stacks:reconcile"; each removed row "expected: refused by stacks:reconcile (UNSUPPORTED_CHANGE)"
  (`reconcile.go:25-29`). When a refusal is predicted, the command prints one warning line before
  `configure: ok`; the exit status stays 0. It writes nothing. A current manifest that does not
  decode is refused by `check` and `export` alike: one `CURRENT_MANIFEST_INVALID` line, exit 1, no
  diff, no notes and nothing written (fail closed: the diff would be judged against a manifest
  nobody can reconcile; coordinator decision of 2026-10-25, T020).
- **FR-006 Consequence notes** — after a successful check or export, the command prints the notes of
  data-model.md *Consequence notes* whose trigger applies, in a fixed order: the
  `LZ_PROJECT_ID_<REF>` names the bound `account.env` needs (names only); one project per tenant and
  environment (D107), and two refs that resolve to one project id are refused at bind (A17, spec
  007 US2), so `configure: ok` and a green `stacks:check` do not mean the bind passes; KD-1 when `shared_state_project` is set (D88, D105); reference versus adopt
  (D94); one VLAN id per private network (005 T098); `org` keeps globally unique bucket names apart
  (005 P21); state buckets are Object-Locked (D104).
- **FR-007 Staging write** — `configure:export` writes only `.local/configure/deployments.yaml`
  (directory 0700, file 0600) through a temporary file in that directory, `fsync`, rename and a
  directory `fsync`. It creates `.local` and `.local/configure` when absent and tightens an existing
  `.local/configure` looser than 0700 to 0700. It refuses a symlinked `.local`, `.local/configure`
  or target. The rename is the publication point: a failure before it removes the temporary file
  and leaves the previous staging file unchanged (`STAGING_WRITE`); a failed directory `fsync`
  after it is reported as written but of uncertain durability (`STAGING_NOT_DURABLE`), exit 1. It
  never writes `stacks/` or any other path (pending the open clarification). It
  prints the next steps: copy to `stacks/deployments.yaml`, then `task stacks:reconcile`,
  `task stacks:generate`, `task stacks:check`. `configure:check` writes nothing.
- **FR-008 No authority** — neither command opens a network connection, reads a credential, an
  environment variable, `~/.config/ovh-lz/` or `account.env`, or executes anything named in the
  answers file. The answers path must be a regular file, not a symlink.
- **FR-009 Second-operator how-to** — `docs/how-to/prepare-deployments-from-answers.md` takes a
  reader from the sandbox answers file to a checked manifest, states what the answers file makes
  easier and where it does not (this spec's *Why and scope*), and ends in `task configure:check`.
  `docs/explanation/guided-setup-walkthrough.md` marks the interactive journey as deferred.
- **FR-010 Traceability** — `task check:specs` with no argument and CI's source check trace spec
  004; every requirement and task of this spec is traced through its `004/` registry keys.

### Key entities
Answers file, derivation rule, generated manifest, refusal message, dry-run diff entry, consequence
note, staging file. See `data-model.md`.

## Success criteria
- **SC-001**: The sandbox answers fixture holds 11 values and the two-tenant one 15; their exports
  are value-equal to the 41-value and 67-value manifests (V002).
- **SC-002**: Each invalid answers fixture yields exactly its expected code, its detail and a next
  action, writes nothing and exits 1; the catalogue completeness test fails when a code lacks an
  entry (V003).
- **SC-003**: Across the check and export tests the only paths created or changed under the test
  root are `.local` and `.local/configure` (created when absent, `.local/configure` tightened to
  0700) and `.local/configure/deployments.yaml`; `check` changes none; and the two packages import no `net`, `net/http` or
  `os/exec` and call no `os.Getenv`, `os.LookupEnv`, `os.Environ` or `os.UserHomeDir` (a source
  test) (V005).
- **SC-004**: A second reader, given only the how-to, produces a two-tenant answers file that
  `configure:check` accepts, and records the time taken and each step that was harder than editing
  the manifest (V006).

## Acceptance and predefined verification
Planned Task targets: `test:configure` (creator T016), `configure:check` and `configure:export`
(creator T020). Offline checks run through `lz-offline` as `task <target> [-- <path>]` when their
input lies in an admitted tree. An operator's answers file under `.local/` is outside the entry's
snapshot (`EntrySkipped`, `tools/internal/checks/offline.go:43-45`), so both `configure:*` targets
are also host-side, credential-free commands, like `stacks:reconcile`. Controls in
`contracts/checks.md`. Evidence starts `not-run`.

| Check | Requirements | Verify |
| --- | --- | --- |
| V001 | FR-001, FR-008 | `task test:configure` (planned, T016) |
| V002 | FR-002, FR-003, SC-001 | `task test:configure`; after the copy, `task stacks:check` |
| V003 | FR-004, SC-002 | `task test:configure` |
| V004 | FR-005, FR-006 | `task test:configure; task configure:check -- tests/fixtures/answers/sandbox.yaml` (planned, T020) |
| V005 | FR-007, FR-008, SC-003 | `task test:configure` |
| V006 | FR-009, SC-004 | `task docs:check`; T022's record |
| V007 | FR-010 | `task check:specs; task test:traceability; task test:foundation-ci` |

## Implementation surface
- `tools/internal/configure/` (new: `answers.go`, `export.go`,
  `messages.go`, `diff.go`, `notes.go`, their tests); `tools/cmd/lz-configure/` (new: `main.go`,
  `main_test.go`); `tests/fixtures/answers/` (new: `sandbox.yaml`, `sandbox-two-tenants.yaml`,
  `invalid/` with `cases.json`).
- `Taskfile.yml` (`test:configure`, `configure:check`, `configure:export`; spec lists);
  `tools/harness/checks.yaml` (keys, checks, inputs).
- `docs/how-to/prepare-deployments-from-answers.md` (new), `docs/explanation/guided-setup-walkthrough.md`,
  `docs/README.md`, `specs/README.md`.
- One tools Go module; no new dependency.

## Out of scope / postponed (with trigger)
| Item | Why not here | Trigger to start |
| --- | --- | --- |
| D-US4 to D-US7 | D127: simplest form first, about 15 review legs | see *Deferred user stories* |
| Writing `stacks/deployments.yaml` in place | ADR-0023: applying the draft is a separate reviewed action | the open clarification answered "in place", with an ADR-0023 amendment |
| Tenant-scope manifests, `external` entries, runtime slots, regions without runtime | not needed by the sandbox or the two-tenant layout; each adds answers fields | a manifest in use that needs one |
| A published `schemas/answers.schema.json` | review round 1: a JSON Schema cannot judge duplicate keys, size or symlinks, the module has no validator, and a second definition beside the Go types adds drift without a reader | an editor integration is asked for, or D-US4 needs a machine-readable answers form |
| Value-level diff (header, CIDR, VLAN id, toggles, changed stage or dimension) | review round 1: only row additions and removals predict `stacks:reconcile`; the rest is scope without a decision behind it | T022 records that a value change surprised the reader |
| Block-style YAML for the answers file | a vendored YAML library is a dependency decision (005 T034) | the owner takes that decision for the manifest too |
| Plain messages for `stacks:check` itself | that changes spec 005's checked output | after this catalogue lands, if a hand-edit error goes unexplained |
| Consequences beyond D113's list | no new live evidence in this spec | a later live spec |

## Dependencies and stop conditions
- Builds on spec 005's decoder and schema and spec 007's two-tenant fixture, all on `main`.
- Stop: a change to `schemas/deployments.schema.json` or to the decoder's codes during this spec
  re-opens T016–T018 (the catalogue completeness test fails by design). If T017 finds an export that
  is not value-equal to the committed manifest, the derivation is wrong; the manifest is not edited
  to match. No task makes a cloud call, reads a credential or writes `stacks/`.
