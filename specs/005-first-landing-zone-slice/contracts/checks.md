# Checks and command contracts: First landing-zone slice
All commands are **planned**; `tasks.md` names each creator. Evidence starts `not-run`.
Offline targets: `<approved-absolute-path>/lz-offline --candidate <checkout> -- task <target> [-- <path>]`.
Host-only targets (`stacks:generate`, `stacks:reconcile`, `bootstrap:account`, `live:*`) run in the
owner's checkout with `MISE_ENV=live` and direnv-loaded credentials.

Outcome words: `pass`, `fail`, `blocked`, `not-run`, `review-required` (constitution V). A missing
tool, empty discovery or a listing error is never `pass`.

## V-checks

| Check | Requirements | Positive control | Negative controls (each must fail) | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | default template vectors for every kind in `kinds.yaml`; reordered test template; import override passes through; labels contain the five mandatory keys | name over the kind's limit; forbidden char; doubled punctuation in a bucket name; empty required segment; unknown kind; extra label overriding `lz:tenant` or `lz:managed-by`; label change altering a name | PR + `evidence/T012.md` |
| V002 | FR-003, FR-004, FR-013, SC-001 | every directory under `modules/`, `components/`, `stages/` passes `lint` and has ≥1 passing `tofu test` run | directory with zero tests; failing test; malformed test stream; zero discovered directories; adopted project without `prevent_destroy` (stage test); tenant policy containing an `account:apiovh:iam/` action (stage test); runtime publishing a `network` capability | PR + `evidence/T032.md` |
| V003 | FR-003 | real repository graph: modules → components → stages → generated stacks, all classified | `backend` or `provider` block in a module/component/stage; `terraform_remote_state` anywhere; hand-written `.tf` under `stacks/`; stage → module edge; component → stage edge | PR + `evidence/T006.md` |
| V004 | FR-005, SC-005 | each stage's fixture `outputs.json` validates; envelope round-trip from captured `tofu output -json` drops sensitive entries | sensitive entry kept; key matching the secret pattern; unknown field; missing required value; `null`/`""` capability placeholder; wrong `stage` for the schema | PR + `evidence/T018.md` |
| V005 | FR-006, FR-007, FR-008, SC-002 | sandbox manifest → 6 stacks, generated files fresh; growth manifest (2×2×2) generates into scratch with unique ids/paths/keys and one state bucket per tenant; every S3 backend has `use_lockfile = true` and `encryption {}`; local backends point outside the repo | unknown field; duplicate key; duplicate id; two rows same scope; region on `project`; missing tenant on regional stage; `account-fabric` row (`STAGE_NOT_IMPLEMENTED`); directory without row, changed id, changed dimension (`UNSUPPORTED_CHANGE`); stale generated file; backend without `encryption`; import block on a non-adopt project | PR + `evidence/T039.md` |
| V006 | FR-009, FR-013, SC-002 | each generated stack plans under its generated mock test with fixture inputs; `stacks:order -- all` equals the topological order of derived edges and the captured `terramate list --run-order` | consumer planned without producer fixture (refuses, no placeholder); producer digest change not selecting its data consumers; authority-only edge triggering a re-plan; unrelated tenant selected; second concurrent run for one tenant admitted | PR + `evidence/T041.md` |
| V007 | FR-010, FR-011, SC-005 | fake chain applies in order, destroys ephemeral set in reverse, retains `account-admin`/`bootstrap`/`project`, records inventory, leftover check passes on a clean captured listing | run inside `lz-offline`; missing or world-readable credential file; tenant stack with platform credentials; seeded secret in any captured stream or file; apply failure, SIGINT, SIGTERM without trap destroy; destroy of a retained instance; leftover present; `ovhcloud` listing error or missing binary reported as pass; candidate worktree path | PR + `evidence/T047.md` |
| V008 | FR-010, FR-012, SC-005 | empty fake account → all phases run; second run → every phase `unchanged`; partial state → only missing phases run | passphrase file overwritten; root AK/AS/CK used without `--fresh-account`; `root-bootstrap.env` left after confirmed revocation; secret echoed; credential file mode ≠ 600 or inside the repo | PR + `evidence/T043.md` |
| V009 | FR-008, FR-012, SC-004 | Owner session: current sandbox — first run imports `lz-sandbox-admin` and creates the bucket; second run reports no change; fresh account (when available) — same command yields a working backend | a re-run that plans any change; state object readable as plaintext JSON; lock not acquired | PR (run id, cost) + `evidence/T044.md`, `evidence/T045.md` |
| V010 | FR-004, FR-008, FR-009, FR-010, FR-011, SC-003 | Owner session: `task live:chain -- all` applies 6 stacks, L7 assertions pass, ephemeral destroy completes, leftover check zero, < 60 min | L7 negatives observed live: tenant deployer IAM write → 403; tenant S3 user reading the account bucket or another tenant's bucket → denied; concurrent plan on one key → lock error; state object not plaintext | PR (run id, cost) + `evidence/T049.md` |
| V011 | FR-014 | every FR/SC/task of spec 005 traced; spec 001 still green | unmapped requirement; task without Verify/Evidence; spec 005 id resolving to spec 001's registry entry | PR + `evidence/T002.md` |

## Security guards with mandatory mutation proof (FR-013)
Each guard: one behavioural mutant that removes or weakens the clause must turn its control red.

| Guard | Where | Mutant |
| --- | --- | --- |
| G1 credential selection by authority | `tools/internal/live/credentials.go` | always load `sandbox.env` |
| G2 secret redaction / no echo | `tools/internal/live/redact.go`, bootstrap writers | write secret to stdout; drop the sensitive filter in envelope building |
| G3 credential file mode and location | `tools/internal/live/files.go` | accept 0644; accept a path inside the repo |
| G4 tenant label from manifest only | `modules/naming` | allow extra labels to override `lz:tenant` |
| G5 tenant policy scope | `components/identity/ovh-native` | resource `*` or add `account:apiovh:iam/*` |
| G6 tenant state-bucket scope | `components/identity/ovh-native` | ARN of the account bucket, another tenant's bucket or `*` |
| G7 adopted project protection | `modules/cloud-project`, `lz-live` retained set | drop `prevent_destroy`; include `project` in destroy set |
| G8 trap destroy and reverse order | `tools/internal/live/chain.go` | no destroy on error; forward order |
| G9 leftover check fail-closed | `tools/internal/live/leftovers.go` | listing error → pass |
| G10 host-only refusal | `tools/cmd/lz-live` | skip offline-entry detection |
| G11 passphrase never overwritten | `tools/internal/live/bootstrap.go` | overwrite on re-run |

## Premise probe contracts
| Probe | Task | Procedure | Pass | Refute → effect |
| --- | --- | --- | --- | --- |
| P4, P6, P16, P17 | T007 (agent, offline, capture admission of 001/T003) | pinned `tofu` with local backend + encryption from var; mock import into nested address; `ignore_changes` on map key across two applies; `terramate create/generate/list --run-order` on a scratch tree | captured outputs match the assumptions; fixtures stored under `tests/fixtures/{tofu-probes,terramate}/` with the generating command and version | blocks US3 generation design; research updated |
| P5, P10, P11, P18 | T009 (Owner session, read-only/plan-only) | import plan of the sandbox project with `-generate-config-out` (no apply); plan alerting/quota; `ovhcloud` list of buckets, networks, OAuth2 clients, IAM policies | no replacement planned; CLI output captured to `tests/fixtures/ovhcloud/` (sanitised ids) | P5 refuted → reference mode; P18 refuted → leftover check via API listing (`ovhcloud` raw or Go client), decision recorded |
| P1–P3, P7, P8, P12–P15 | T010 (Owner session, create→destroy, `tests/live/probes/`) | throwaway bucket (versioned) with two state writers, encryption, tags with `:`; probe OAuth2 client + policy; private network + subnet in GRA11; destroy all; list leftovers | every observation recorded; zero leftovers | each refutation blocks the dependent FR clause and names the fallback |

## Command contracts

### `task test:unit -- <dir>` (T004)
Runs, in `lz-offline`, mirror-only `tofu -chdir=<dir> init -backend=false -lockfile=readonly` then
`tofu -chdir=<dir> test -json` through the 001 report adapter. Zero tests, a skipped run, a crash or a
truncated stream is `fail`.

### `task test:slice` (T004)
Discovers every directory classified `library` or `stage` by the dependency graph under `modules/`,
`components/`, `stages/`; runs `lint` and `test:unit` on each; prints per-directory counts. Zero
directories is `fail`.

### `task stacks:reconcile` / `task stacks:generate` (host, T036/T038)
Reconcile: decode manifest, create missing stacks via `terramate create`, refuse other mismatches.
Generate: `terramate generate`. Both print the changed paths; neither touches cloud.

### `task stacks:check` (offline, T036)
Copies the candidate to scratch, runs reconcile `--check` and `terramate generate`, diffs against the
candidate; any difference is `fail` and names the files.

### `task test:stack-plans` (offline, T038)
For each stack: `init -backend=false`, `tofu test` of its generated `_lz_offline.tftest.hcl` with
fixture inputs from `tests/fixtures/outputs/`.

### `task stacks:order -- <instance|all>` (offline, T041)
Prints the run order and, given a records directory, the re-plan set with reasons.

### `lz-live` (host, T043/T047)
```
lz-live bootstrap [--fresh-account]
lz-live plan|apply|destroy <instance|all>
lz-live chain <instance|all>
```
Exit codes: 0 pass; 1 fail; 2 blocked (missing prerequisite); 3 refused (guard). Output lines are
`LZ-LIVE <phase> <instance> <status> <detail>`; no secret, no raw plan JSON. Every run prints its run id
and, at the end, `record approximate cost for run <id> in the PR`.
