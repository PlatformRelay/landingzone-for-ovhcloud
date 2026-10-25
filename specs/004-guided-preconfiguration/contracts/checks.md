# Checks and command contracts: Answers file to deployments.yaml
Refreshed: 2026-10-25 (D127)

Targets in `Taskfile.yml`: `test:configure` (T016), `configure:check` and `configure:export`
(T020). The entry admits exactly `lz-offline
--candidate <checkout> -- task <target> [-- <path>]` (`tools/cmd/lz-offline/main.go`); its snapshot
skips `.local/`, so the `configure:*` targets run host-side for an operator's file and through the
entry only for files under `tests/`. No target here needs a credential or the network.

Outcome words: `pass`, `fail`, `blocked`, `not-run`, `review-required` (constitution V). "Red" is a
behavioural failure against a compiling stub; a compile error or a missing symbol is not red.

## Commands
- `lz-configure [-root <repo>] check <answers>` (`task configure:check -- <answers>`): decode,
  derive, gate, then print the diff (FR-005), a warning line when a removed row predicts a refusal by
  `stacks:reconcile`, the notes (FR-006) and `configure: ok`; or one refusal line (FR-004), or the
  `configure: error:` or `configure: defect:` line (*Message catalogue*), and exit 1. The current
  `stacks/deployments.yaml` is read like the answers file: a symlink, a non-regular file, more than
  1 MiB or a file that does not decode is refused (`CURRENT_MANIFEST_INVALID`, FR-005), with the
  path alone as detail, never a value from the file (T024). Writes nothing.
- `lz-configure [-root <repo>] export <answers>` (`task configure:export -- <answers>`, host-side):
  the same, then write the staging file (FR-007) and print `wrote .local/configure/deployments.yaml`
  and the next steps. Usage errors exit 2. `LZ_EXPORT_SCRATCH=1` (the registry row) exports
  under a temporary root holding a copy of `stacks/deployments.yaml`, removed afterwards, and runs
  nothing when that root cannot be made, so the row never replaces an operator's staged file (T024).
- `<answers>` is relative to the root or absolute; it must be a regular file (FR-008).

## V-checks

| Check | Requirements | Positive control | Negative controls (each must fail) | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001, FR-008 | the sandbox and two-tenant answers fixtures decode; an answers file with every optional field set decodes; each shared field's pattern equals the one `schemas/deployments.schema.json` gives its manifest field | each `tests/fixtures/answers/invalid/` case accepted or refused with another code: duplicate key, case variant, unknown field, missing field, `null`, wrong `apiVersion` or `kind`, pattern and enum violations, `vlan_id` without `cidr`, a second document, a trailing comment, block YAML, 64 KiB + 1 byte, a symlinked answers path; a pattern that differs from the manifest schema's; a pattern check that compared zero fields | `evidence/T016.md` |
| V002 | FR-002, FR-003, SC-001 | sandbox answers → value-equal to `stacks/deployments.yaml`; two-tenant answers → value-equal to the two-tenant fixture; both pass `DecodeManifest`; two runs give identical bytes; the value counts are 11 and 15 | a refused manifest printed or written; an override of a field data-model marks fixed; a `sandbox` block written for `false`; rows in another order; a network row without `cidr` | `evidence/T017.md` |
| V003 | FR-004, SC-002 | each invalid fixture prints exactly its expected code, a detail, a plain sentence and `Next:`; the reachable decoder codes (`DUPLICATE_NAME`, `DUPLICATE_VLAN`, `SHARED_STATE_PROJECT`, `INVALID_VALUE` from a long region name) each from an answers fixture, with their `generated` entry; a `generated` `UNKNOWN_FIELD` gets the tool-defect entry, a `configure` `UNKNOWN_FIELD` the spelling entry | a (source, code) pair without a catalogue entry; a completeness check that parsed zero `Code*` constants and passed; a message without `Next:`; a raw decoder error printed without its sentence | `evidence/T018.md` |
| V004 | FR-005, FR-006 | diff of the two-tenant answers against the sandbox manifest: four rows `+`, each `expected: created by stacks:reconcile`, no warning; the reverse: four rows `-`, each `expected: refused by stacks:reconcile (UNSUPPORTED_CHANGE)`, then the warning line and `configure: ok`; no manifest: every row `+`; notes in the fixed order, the one-project note naming the bind refusal (A17), the KD-1 note only with the flag, `account.env` names only | a removed row marked as created; a removal without the warning line; an unordered diff; a note whose trigger does not hold; a project id or any `account.env` value printed; a diff printed when the current manifest does not decode | `evidence/T019.md` |
| V005 | FR-007, FR-008, SC-003 | export creates `.local/configure/deployments.yaml` (0600 in a 0700 directory) and nothing else under the test root but the two directories (inventory before and after); an existing 0755 `.local/configure` ends 0700; check writes nothing; a failure before the rename leaves the previous staging file byte-identical and reports `STAGING_WRITE`; a failed directory `fsync` after it leaves the new file and reports `STAGING_NOT_DURABLE`, exit 1 | a write to `stacks/`; a followed symlink at `.local`, `.local/configure` or the target; a partial staging file or a leftover temporary file after an injected failure; a looser directory left as it was; a directory `fsync` failure reported as success or as "unchanged"; an import of `net`, `net/http` or `os/exec`, or a call of `os.Getenv`, `os.LookupEnv`, `os.Environ` or `os.UserHomeDir` in either package (source test) | `evidence/T020.md` |
| V006 | FR-009, SC-004 | `task docs:check` green with the how-to and the walkthrough; T022's record of a second reader's attempt | the how-to without the "does not help" list, or without a final `task` target; T022 without time taken and hard steps | `evidence/T021.md`, `evidence/T022.md` |
| V007 | FR-010 | `task check:specs` with no argument prints `TRACE_OK` for 004; `ci-source` reads spec 004; `TestTaskfileTracesEverySpec` green | either list omitting spec 004 while the registry holds `004/` keys | `evidence/T015.md`, `evidence/T023.md` |

## Security guards with mandatory mutation proof
Each guard: one behavioural mutant that removes or weakens the clause must turn its control red.

| Guard | Where | Mutant |
| --- | --- | --- |
| G29 strict answers decode (FR-001) | `tools/internal/configure/answers.go` | accept an unknown field, a duplicate key or a case variant; drop the size bound; accept a symlinked answers path |
| G30 decoder gate (FR-003) | `tools/internal/configure/export.go` | skip `DecodeManifest`; print or write the bytes after a refusal |
| G31 write boundary (FR-007, FR-008) | `tools/cmd/lz-configure/main.go` | write in place instead of rename; follow a symlinked directory or target; write during `check`; skip the chmod of a looser directory; report a directory `fsync` failure as success |
| G32 catalogue completeness (FR-004) | `tools/internal/configure/messages.go` | delete one entry; fall back to the raw decoder error; look entries up by code alone |

## Message catalogue (FR-004)
Keyed by (source, code). Source `configure`: the answers file, the current manifest and the staging
write, raised by `tools/internal/configure` or the command. Source `generated`: a `Code*` constant
of `tools/internal/stacks/manifest.go` returned for the generated manifest. `<detail>` comes from
the source (data-model *Refusal message*). The completeness test requires an entry for every
`configure` code and every parsed `Code*` constant under `generated`, and fails when it parses zero
constants.

**The one exception.** An I/O failure that is not a refusal of the answers has no entry: a read
error of the answers file other than missing, symlink, non-regular or oversized (for example,
permission denied), or a read error of `stacks/deployments.yaml` other than its absence or a
refusal. It prints one line `configure: error: <error>` (control characters quoted), exit 1, and
nothing else. It is never printed as a catalogue line, so it never blames the content of the
answers (coordinator decision of 2026-10-25, T020).

**A refusal the catalogue cannot explain** (a code without an entry, or a refusal wrapped in another
error) is a defect of the tool, never an I/O error and never printed raw: one line
`configure: defect: <CODE> could not be explained. This is a defect of the tool, not of your
answers. Next: Report it with the answers file; meanwhile edit `stacks/deployments.yaml` by hand.`,
exit 1, without the detail (T024).

| Source | Code | Plain sentence | Next |
| --- | --- | --- | --- |
| configure | `ANSWERS_SYNTAX` | The file is not one JSON-compatible document; only whole-line `#` comments are allowed. | Fix the bracket, comma or quote near the place named, or start again from the sandbox answers file. |
| configure | `ANSWERS_TOO_LARGE` | The answers file is larger than 64 KiB. | Remove what is not an answer; a real answers file is a few hundred bytes. |
| configure | `ANSWERS_NOT_REGULAR` | The answers path is not a regular file (a symlink, a directory or missing). | Give the path of the file itself. |
| configure | `DUPLICATE_KEY` | A key appears twice in one object; only one could be used. | Keep one of the two keys. |
| configure | `UNKNOWN_FIELD` | This key is not part of the answers file, or its case differs. | Check the spelling against the how-to; keys are lower case with `_`. |
| configure | `MISSING_FIELD` | A required answer is missing or `null`. | Add it; the how-to lists the required answers. |
| configure | `UNSUPPORTED_VERSION` | `apiVersion` or `kind` is not the one this tool reads. | Use `lz.platformrelay.dev/v1alpha1` and `DeploymentAnswers`. |
| configure | `INVALID_VALUE` | A value does not have the allowed form. | Use the form the how-to gives for that field. |
| configure | `CURRENT_MANIFEST_INVALID` | Today's `stacks/deployments.yaml` is a symlink, not a regular file, larger than 1 MiB, or does not decode, so no diff can be shown. | Fix it first; `task stacks:check` names a decoding problem. |
| configure | `STAGING_SYMLINK` | `.local`, `.local/configure` or the staging file is a symlink; nothing was written. | Remove the symlink and run the export again. |
| configure | `STAGING_WRITE` | The staging file could not be written; the previous one is unchanged. | Check free space and permissions of `.local/configure/`. |
| configure | `STAGING_NOT_DURABLE` | The staging file was written, but its directory could not be synced; after a crash the previous file may come back. | Check `.local/configure/deployments.yaml` before you copy it, or run the export again. |
| generated | `INVALID_VALUE` | A name built from your names is longer than 63 characters. | Shorten the tenant, environment or region name; if they are short, report it with the answers file. |
| generated | `DUPLICATE_NAME` | A tenant, an environment of one tenant or a region of one environment is named twice, or two environments use one project ref. | Rename one of them, or give each environment its own `project_ref`. |
| generated | `DUPLICATE_VLAN` | Two regions of one environment share a VLAN id. | Give one region its own `vlan_id` (1 to 4000). |
| generated | `SHARED_STATE_PROJECT` | An environment uses the state project, which is allowed only in the sandbox (KD-1). | Give the environment its own `project_ref`, or set `shared_state_project` if this is the sandbox. |
| generated | every other `Code*` constant (today `MANIFEST_SYNTAX`, `DUPLICATE_KEY`, `UNKNOWN_FIELD`, `MISSING_FIELD`, `UNSUPPORTED_VERSION`, `STAGE_NOT_IMPLEMENTED`, `SLOT_NOT_ALLOWED`, `SCOPE_VIOLATION`, `DUPLICATE_ID`, `DUPLICATE_SCOPE`, `PLATFORM_ROWS`, `MISSING_TENANT_STATE`, `TENANT_SCOPE_ACCOUNT_ROW`, `UNRESOLVED_PRODUCER`), each listed by name | Unreachable: the tool built a manifest the decoder refuses. This is a defect of the tool, not of your answers. | Report it with the answers file; meanwhile edit `stacks/deployments.yaml` by hand. |
