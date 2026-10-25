# Checks and command contracts: Answers file to deployments.yaml
Refreshed: 2026-10-25 (D127)

Planned targets, each absent from `Taskfile.yml` until its creating task closes: `test:configure`
(T016), `configure:check` and `configure:export` (T020). The entry admits exactly `lz-offline
--candidate <checkout> -- task <target> [-- <path>]` (`tools/cmd/lz-offline/main.go`); its snapshot
skips `.local/`, so the `configure:*` targets run host-side for an operator's file and through the
entry only for files under `tests/`. No target here needs a credential or the network.

Outcome words: `pass`, `fail`, `blocked`, `not-run`, `review-required` (constitution V). "Red" is a
behavioural failure against a compiling stub; a compile error or a missing symbol is not red.

## Commands (planned)
- `lz-configure [-root <repo>] check <answers>` (`task configure:check -- <answers>`): decode,
  derive, gate, then print the diff (FR-005), the notes (FR-006) and `configure: ok`; or one refusal
  line (FR-004) and exit 1. Writes nothing.
- `lz-configure [-root <repo>] export <answers>` (`task configure:export -- <answers>`, host-side):
  the same, then write the staging file (FR-007) and print `wrote .local/configure/deployments.yaml`
  and the next steps. Usage errors exit 2.
- `<answers>` is relative to the root or absolute; it must be a regular file (FR-008).

## V-checks

| Check | Requirements | Positive control | Negative controls (each must fail) | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001, FR-008 | the sandbox and two-tenant answers fixtures decode; an answers file with every optional field set decodes | each `tests/fixtures/answers/invalid/` case accepted or refused with another code: duplicate key, case variant, unknown field, missing field, `null`, wrong `apiVersion` or `kind`, pattern and enum violations, `vlan_id` without `cidr`, a second document, a trailing comment, block YAML, 64 KiB + 1 byte, a symlinked answers path | `evidence/T016.md` |
| V002 | FR-002, FR-003, SC-001 | sandbox answers → value-equal to `stacks/deployments.yaml`; two-tenant answers → value-equal to the two-tenant fixture; both pass `DecodeManifest`; two runs give identical bytes; the value counts are 11 and 15 | a refused manifest printed or written; an override of a field data-model marks fixed; a `sandbox` block written for `false`; rows in another order; a network row without `cidr` | `evidence/T017.md` |
| V003 | FR-004, SC-002 | each invalid fixture prints exactly its expected code, a detail, a plain sentence and `Next:`; the reachable decoder codes (`DUPLICATE_NAME`, `DUPLICATE_VLAN`, `SHARED_STATE_PROJECT`, `INVALID_VALUE` from a long region name) each from an answers fixture | a decoder `Code*` constant without a catalogue entry; a message without `Next:`; a raw decoder error printed without its sentence | `evidence/T018.md` |
| V004 | FR-005, FR-006 | diff of the two-tenant answers against the sandbox manifest: four rows `+`, each `created by stacks:reconcile`; the reverse: four rows `-`, each `refused by stacks:reconcile (UNSUPPORTED_CHANGE)`; a CIDR change as one value line; no manifest: every row `+`; notes in the fixed order, the KD-1 note only with the flag, `account.env` names only | a removed row marked as created; an unordered diff; a note whose trigger does not hold; a project id or any `account.env` value printed; a diff printed when the current manifest does not decode | `evidence/T019.md` |
| V005 | FR-007, FR-008, SC-003 | export writes `.local/configure/deployments.yaml` (0600 in a 0700 directory) and nothing else under the test root (inventory before and after); check writes nothing; a failing rename leaves the previous staging file byte-identical | a write to `stacks/`; a followed symlink at `.local`, `.local/configure` or the target; a partial staging file after an injected failure; an import of `net`, `net/http` or `os/exec`, or a call of `os.Getenv`, `os.LookupEnv`, `os.Environ` or `os.UserHomeDir` in either package (source test) | `evidence/T020.md` |
| V006 | FR-009, SC-004 | `task docs:check` green with the how-to and the walkthrough; T022's record of a second reader's attempt | the how-to without the "does not help" list, or without a final `task` target; T022 without time taken and hard steps | `evidence/T021.md`, `evidence/T022.md` |
| V007 | FR-010 | `task check:specs` with no argument prints `TRACE_OK` for 004; `ci-source` reads spec 004; `TestTaskfileTracesEverySpec` green | either list omitting spec 004 while the registry holds `004/` keys | `evidence/T015.md`, `evidence/T023.md` |

## Security guards with mandatory mutation proof
Each guard: one behavioural mutant that removes or weakens the clause must turn its control red.

| Guard | Where | Mutant |
| --- | --- | --- |
| G29 strict answers decode (FR-001) | `tools/internal/configure/answers.go` | accept an unknown field, a duplicate key or a case variant; drop the size bound; accept a symlinked answers path |
| G30 decoder gate (FR-003) | `tools/internal/configure/export.go` | skip `DecodeManifest`; print or write the bytes after a refusal |
| G31 write boundary (FR-007, FR-008) | `tools/cmd/lz-configure/main.go` | write in place instead of rename; follow a symlinked directory or target; write during `check` |
| G32 catalogue completeness (FR-004) | `tools/internal/configure/messages.go` | delete one entry; fall back to the raw decoder error |

## Message catalogue (FR-004)
`<detail>` comes from the source (data-model *Refusal message*). "Unreachable" codes are refusals
the derivation cannot produce; their entry says so.

| Code | Plain sentence | Next |
| --- | --- | --- |
| `ANSWERS_SYNTAX`, `MANIFEST_SYNTAX` | The file is not one JSON-compatible document; only whole-line `#` comments are allowed. | Fix the bracket, comma or quote near the place named, or start again from the sandbox answers file. |
| `ANSWERS_TOO_LARGE` | The answers file is larger than 64 KiB. | Remove what is not an answer; a real answers file is a few hundred bytes. |
| `ANSWERS_NOT_REGULAR` | The answers path is not a regular file (a symlink, a directory or missing). | Give the path of the file itself. |
| `DUPLICATE_KEY` | A key appears twice in one object; only one could be used. | Keep one of the two keys. |
| `UNKNOWN_FIELD` | This key is not part of the answers file, or its case differs. | Check the spelling against the how-to; keys are lower case with `_`. |
| `MISSING_FIELD` | A required answer is missing or `null`. | Add it; the how-to lists the required answers. |
| `UNSUPPORTED_VERSION` | `apiVersion` or `kind` is not the one this tool reads. | Use `lz.platformrelay.dev/v1alpha1` and `DeploymentAnswers`. |
| `INVALID_VALUE` | A value does not have the allowed form, or a name built from your names is longer than 63 characters. | Use the form the how-to gives for that field; shorten the tenant, environment or region name. |
| `DUPLICATE_NAME` | A tenant, an environment of one tenant or a region of one environment is named twice, or two environments use one project ref. | Rename one of them, or give each environment its own `project_ref`. |
| `DUPLICATE_VLAN` | Two regions of one environment share a VLAN id. | Give one region its own `vlan_id` (1 to 4000). |
| `SHARED_STATE_PROJECT` | An environment uses the state project, which is allowed only in the sandbox (KD-1). | Give the environment its own `project_ref`, or set `shared_state_project` if this is the sandbox. |
| `STAGE_NOT_IMPLEMENTED`, `SLOT_NOT_ALLOWED`, `SCOPE_VIOLATION`, `DUPLICATE_ID`, `DUPLICATE_SCOPE`, `PLATFORM_ROWS`, `MISSING_TENANT_STATE`, `TENANT_SCOPE_ACCOUNT_ROW`, `UNRESOLVED_PRODUCER` | Unreachable: the tool built a manifest the decoder refuses. This is a defect of the tool, not of your answers. | Report it with the answers file; meanwhile edit `stacks/deployments.yaml` by hand. |
| `CURRENT_MANIFEST_INVALID` | Today's `stacks/deployments.yaml` does not decode, so no diff can be shown. | Fix it first; `task stacks:check` names the problem. |
| `STAGING_SYMLINK` | `.local`, `.local/configure` or the staging file is a symlink; nothing was written. | Remove the symlink and run the export again. |
| `STAGING_WRITE` | The staging file could not be written; the previous one is unchanged. | Check free space and permissions of `.local/configure/`. |
