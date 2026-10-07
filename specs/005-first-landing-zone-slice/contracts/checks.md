# Checks and command contracts: First landing-zone slice
All commands are **planned**; `tasks.md` names each creator. Evidence starts `not-run`.
Offline targets: `<approved-absolute-path>/lz-offline --candidate <checkout> -- task <target> [-- <path>]`.
Credential-bearing host targets (`bootstrap:account`, `live:*`, including `live:probe`) run in the
owner's main checkout with `MISE_ENV=live`, admitted by the host guard (FR-011); `lz-live` loads
credentials itself and ignores ambient ones. `stacks:reconcile` and `stacks:generate` are host-side
but credential-free and unguarded (like `generate:foundation-ci`) and run in any checkout, including
an authoring worktree.

Outcome words: `pass`, `fail`, `blocked`, `not-run`, `review-required` (constitution V). A missing
tool, empty discovery or a listing error is never `pass`. A live assertion may also report
`known-deviation <id>` for a deviation listed in spec *Known deviations*: it is recorded and shown,
never counted as `pass`, and does not by itself change the exit code.

## V-checks

| Check | Requirements | Positive control | Negative controls (each must fail) | Evidence |
| --- | --- | --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | default template vectors for every kind in `kinds.yaml`; reordered test template; import override passes through (T011); labels contain the five mandatory keys | name over the kind's limit; forbidden char; doubled punctuation in a bucket name; empty required segment; unknown kind; invalid override; extra label overriding `lz:tenant` or `lz:managed-by`; label change altering a name | PR + `evidence/T012.md` |
| V002 | FR-003, FR-004, FR-013, SC-001 | every directory under `modules/`, `components/`, `stages/` passes `lint` and has ≥1 passing `tofu test` run | directory with zero tests; failing test; malformed or truncated test stream (T003); zero discovered directories; adopted project or protected state bucket without `prevent_destroy`; tenant policy containing an `account:apiovh:iam/` action, `region/storage/policy/create` or a wildcard (stage test); tenant S3 user granted another bucket; runtime publishing a `network` capability | PR + `evidence/T032.md` |
| V003 | FR-003 | real repository graph: modules → components → stages → generated stacks, all classified | `backend` or `provider` block in a module/component/stage; `terraform_remote_state` anywhere; hand-written `.tf` under `stacks/`; stage → module edge; component → stage edge | PR + `evidence/T006.md` |
| V004 | FR-005, SC-005 | each stage's fixture `outputs.json` validates; envelope round-trip from captured `tofu output -json` drops sensitive entries | sensitive entry kept; key matching the secret pattern; unknown field; missing required value; `null`/`""` capability placeholder; wrong `stage` for the schema | PR + `evidence/T018.md` |
| V005 | FR-006, FR-007, FR-008, SC-002 | sandbox manifest → 6 stacks, generated files fresh; growth manifest (2×2×2, two runtime slots) generates into scratch with unique ids/paths/keys, exact state keys `<path minus "stacks/">/terraform.tfstate`, distinct planned bucket names for the two slots and one state bucket per tenant; authoring flow in a linked worktree with a dirty tree (edit → reconcile → generate → check) green without credentials; tenant-only manifest generates in a separate scratch repository without account stacks; every S3 backend has `use_lockfile = true` and `encryption {}`; account and account-tenant stacks use the account bucket; the only local backend (`bootstrap`) points outside the repo | unknown field; duplicate key; duplicate id; two rows same scope and slot; `slot` on a non-runtime stage; region on `project`; missing tenant on regional stage; tenant row without `tenant-state`; account row in a tenant-only manifest; consumed producer neither row nor `external`; state project equal to a tenant project without `sandbox.shared_state_project`; `account-fabric` or `account-admin` row (`STAGE_NOT_IMPLEMENTED`); `git` stage source (`STAGE_SOURCE_NOT_IMPLEMENTED`); directory without row, changed id, changed dimension (`UNSUPPORTED_CHANGE`); stale generated file; backend without `encryption`; import block on a non-adopt project; two stacks planning one bucket name (`NAME_COLLISION`); generation refused in an authoring worktree | PR + `evidence/T039.md` |
| V006 | FR-009, FR-013, SC-002 | each generated stack plans under its generated mock test with fixture inputs; `stacks:order -- all` equals the topological order of derived data and authority edges and the captured `terramate list --run-order`; selection per research R21 | consumer planned without producer fixture (`blocked`, no placeholder); upstream-only or intermediate-only code change not selecting the right set; producer digest change not selecting its transitive data consumers; authority-only edge triggering a re-plan; new tenant row not selecting `account-governance` and the new `tenant-state`; changed resolved project id not selecting `account-governance` and that `project`; unrelated tenant selected; second concurrent run for one tenant, or touching account stacks, admitted while the first holds the lock | PR + `evidence/T041.md` |
| V007 | FR-010, FR-011, FR-013, SC-005 | guard admits the owner's main checkout, clean, at the reviewed SHA; fake chain applies in order, destroys `runtime` and `project-network` in reverse, retains `bootstrap`/`tenant-state`/`account-governance`/`project`, appends inventory per resource, leftover check passes on clean listings of every matrix kind (synthetic in T054, captured in T065) with only the admin client and policy ids exempt; every plan passes the retained-resource guard (T064); a probe whose destroy failed is cleaned up by a fresh process from its retained state and passphrase; admin, platform and tenant credentials bind through `GET /auth/details` | run inside `lz-offline`; linked worktree (any path, symlinked or not); dirty tree; SHA mismatch or not on `origin/main`; ambient `OVH_*`/`AWS_*` inherited by a child; credential for another account; missing or world-readable credential file; tenant stack with platform credentials; seeded secret in any captured stream, argv or file; apply failure, SIGINT, SIGTERM or deadline without trap destroy; forward destroy order; plan deleting or replacing a retained resource applied (removed block, replacement, `forget`, unparseable plan); `destroy` of a retained instance; binding through `GET /me` (403 for deployers); admin exemption matched by name; probe passphrase dropped before cleanup succeeded; one seeded leftover per kind, including one absent from every state; resource type outside the matrix; `ovhcloud` listing error, unparseable listing or missing binary reported as pass | PR + `evidence/T047.md` |
| V008 | FR-010, FR-012, SC-005 | empty fake account → phases `guard, identify, passphrase, admin, state, publish, verify` run in order (plus `revoke` with `--fresh-account`); second run → every phase `unchanged`; each partial state of research R13 → only missing phases run or a named refusal; fresh-account journeys from an empty directory and with a previous account's files reach `revoke` with project references prompted, and a failed or aborted fresh run still revokes the root credential and resumes on retry | passphrase file overwritten; encrypted state written before the passphrase exists; root AK/AS/CK accepted without `--fresh-account` or present in argv, a child environment, a file or a stream; root credential not revoked at the end of a fresh run, including a failed or aborted one; `state` phase applying a plan that removes or replaces the account bucket or platform S3 user; admin reported `unchanged` when the credential works but the policy drifted; previous account's file read after migration; binding mismatch accepted; bootstrap writing a deployer file; bucket name taken by another account without the `spec.org` message; secret echoed; credential file mode ≠ 600 or inside the repo | PR + `evidence/T043.md`, `evidence/T057.md` |
| V009 | FR-008, FR-012, SC-004 | Owner session: current sandbox — first run binds the account, reports `admin` `unchanged` (no import, no new client) and creates the bucket; second run reports no change; fresh account (when available) — same command with `--fresh-account` yields a working backend and a revoked root credential | a re-run that plans any change; state object readable as plaintext JSON; lock not acquired; root credential still valid after the fresh run | PR (run id, cost) + `evidence/T044.md`, `evidence/T045.md` |
| V010 | FR-004, FR-008, FR-009, FR-010, FR-011, SC-003 | Owner session: `task live:chain -- all` applies 6 stacks, L7 assertions pass, the two ephemeral stacks are destroyed, leftover check over the full matrix zero, within the deadline (< 60 min) | L7 negatives observed live: tenant deployer IAM write → 403; platform or tenant deployer failing to bind through `GET /auth/details` (P26); tenant S3 user reading the account bucket or another tenant's bucket → denied; concurrent plan on one key → lock error; state object not plaintext. **KD-1 negative (expected red until a separate state project exists)**: tenant deployer API `DELETE` and `bulkDeleteObjects` on the disposable canary bucket in the state project → reported `known-deviation KD-1` when allowed with `shared_state_project: true` (recorded, exit code unchanged), `fail` when allowed without the flag, `pass` when denied | PR (run id, cost, known deviations) + `evidence/T049.md` |
| V011 | FR-014 | every FR/SC/task of spec 005 traced; spec 001 still green | unmapped requirement; task without Verify/Evidence; spec 005 id resolving to spec 001's registry entry | PR + `evidence/T002.md` |
| V012 | FR-005, FR-009, FR-013 | offline round trip on generated roots under mocks: producer apply → envelope → publisher → adapter → consumer plan; local-state `bootstrap` publishes to the account bucket path; record holds the consumed digest | missing artefact (`blocked`); malformed or schema-invalid artefact; artefact from the wrong producer (`instance_id` or `stage`); `project` artefact whose project id differs from the bound account's reference (KD-3 mitigation); sensitive value in the generated tfvars | PR + `evidence/T061.md` |

## Security guards with mandatory mutation proof (FR-013)
Each guard: one behavioural mutant that removes or weakens the clause must turn its control red.

| Guard | Where | Mutant |
| --- | --- | --- |
| G1 credential selection by authority | `tools/internal/live/credentials.go` | always load `sandbox.env` |
| G2 secret redaction / no echo | `tools/internal/live/redact.go` (run core, T055), `rootkeys.go`, bootstrap writers, `stacks/outputs.go`, `stacks/adapter.go` | write a secret to stdout, argv or a generated file; drop the sensitive filter in envelope building or the adapter |
| G3 credential file mode and location | `tools/internal/live/files.go` | accept 0644; accept a path inside the repo |
| G4 tenant label from manifest only | `modules/naming` | allow extra labels to override `lz:tenant` |
| G5 tenant policy scope | `components/identity/ovh-native`, `stages/account-governance`; the stage's captured plan (`TestOutputsAccountGovernanceStagePlan`, T024); `lz-check deps` rules `STAGE_RESOURCE` and `STAGE_COMPONENT_CALLS` (T024) | resource `*`; add `account:apiovh:iam/*`, `region/storage/*` or `region/storage/policy/create`; tenant group role other than `NONE`; an extra or missing OAuth2 client, policy or group in the plan; a second, repeated, non-relative or other module call in the stage |
| G6 tenant state-bucket scope | `components/state-backend`, `stages/tenant-state`; `lz-check deps` rules `STAGE_RESOURCE` and `STAGE_COMPONENT_CALLS` (T022) | ARN of the account bucket, another tenant's bucket or `*`; a resource in the stage; a second, repeated, non-relative or other module call in it, or one stage directory called twice |
| G7 retained-infrastructure protection | `modules/cloud-project`, `modules/object-storage-protected`, `tools/internal/live/protect.go` (T064, shared), its callers `runner.go`, `bootstrap_state.go`, `apply.go`, `chain.go`; `lz-check deps` rules `RETAINED_UNPROTECTED` (T014) and `STATE_BUCKET_UNPROTECTED` (T016) | drop `prevent_destroy`; let `components/state-backend` reach a bucket other than through `modules/object-storage-protected`; skip the plan-level delete/replace refusal, or only check `delete`; ignore a removed block; a caller (bootstrap `state`, apply) bypassing `protect.go`; accept a retained instance in `destroy` or the chain's destroy set |
| G8 trap destroy, reverse order, deadline | `tools/internal/live/runner.go`, `chain.go` | no destroy on error or deadline; forward order |
| G9 leftover check fail-closed | `tools/internal/live/leftovers.go` | listing error → pass; ignore a kind; ignore a resource absent from state; exempt the admin by name instead of by recorded id; delete a probe's passphrase before cleanup succeeds |
| G10 host guard | `tools/internal/live/guard.go` | skip offline-entry detection; accept a linked worktree; accept a dirty tree; skip the reviewed-SHA check |
| G11 passphrase never overwritten | `tools/internal/live/bootstrap.go` | overwrite on re-run |
| G12 child environment from scratch | `tools/internal/live/env.go` | inherit the caller's `OVH_*`/`AWS_*` |
| G13 account binding | `tools/internal/live/binding.go` | skip the account or `org` comparison; bind through `GET /me` (fails for deployers) |
| G14 run locks | `tools/internal/stacks/lock.go` | skip the account lock; admit a second tenant-lock holder |
| G15 known-deviation reporting | `tools/internal/live/observe.go`, `tools/internal/probes/live/chain/` | report an observed KD-1 gap as `pass`; report it as `known-deviation` without the sandbox flag |

## Premise probe contracts
| Probe | Task | Procedure | Pass | Refute → effect |
| --- | --- | --- | --- | --- |
| P4, P6, P16, P17, P24 | T007 (agent, offline, capture admission of 001/T003) | pinned `tofu` with local backend + encryption from var; mock import into nested address; `ignore_changes` on map key across two applies; `terramate create/generate/list --run-order` on a scratch tree; `tofu apply -json` events on `terraform_data` | captured outputs match the assumptions; fixtures stored under `tests/fixtures/{tofu-probes,terramate}/` with the generating command and version | blocks US3 generation design (P4, P6, P16, P17) or switches the inventory to its fallback (P24); research updated |
| P5, P10, P11, P18, P26 (admin) | T009 (Owner session, read-only/plan-only, through `lz-live probe --plan-only`) | import plan of the sandbox project with `-generate-config-out` (no apply); plan alerting/quota; listing of every leftover-matrix kind; `GET /auth/details` with the admin credential | no replacement planned; CLI output per kind captured to `tests/fixtures/ovhcloud/` (sanitised ids) | P5 refuted → reference mode; P18 refuted for a kind → API listing for that kind, decision recorded |
| P1–P3, P7–P9, P12–P15, P26, P25 | T010 (Owner session, create→destroy, `tests/live/probes/`, through `lz-live probe`) | throwaway bucket (versioned) with two state writers (second started after the lock object is listed), encryption, tags with `:`; probe OAuth2 client + policy; P9 allowlist identity creating and destroying network, subnet and bucket, and binding through `GET /auth/details` while `GET /me` is denied (P26); private network + subnet in GRA11; optional tag-conditioned policy; destroy all; leftover listing per kind | every observation recorded; zero leftovers | each refutation blocks the dependent FR clause and names the fallback; a P1–P3 refutation revises FR-008 before T038 closes |
| P22, P23 | T045 (Owner session) | fresh account: root keys at the prompt, admin client and policy creation, credential revocation | admin created, root credential revoked | blocks SC-004's fresh part; manual Control Panel steps recorded |

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
Generate: `terramate generate`. Both print the changed paths; neither touches cloud, reads a
credential or calls the host guard, so both run in an authoring worktree with a dirty tree (the
manifest edit precedes them). The committed result is checked by offline `stacks:check`.

### `task stacks:check` (offline, T036)
Copies the candidate to scratch, runs reconcile `--check` and `terramate generate`, diffs against the
candidate; any difference is `fail` and names the files.

### `task test:stack-plans` (offline, T038)
For each stack: `init -backend=false`, `tofu test` of its generated `_lz_offline.tftest.hcl` with
fixture inputs from `tests/fixtures/outputs/`.

### `task test:exchange` (offline, T061)
Generates the sandbox fixture into scratch, applies producers under mocks, publishes to a scratch
bucket directory, adapts and plans consumers; prints per-edge results.

### `task stacks:order -- <instance|all>` (offline, T041)
Prints the run order and, given a records directory, the selected set with reasons.

### `lz-live` (host, T053/T055/T043/T059/T047)
```
lz-live bootstrap --reviewed-sha <sha> [--fresh-account]
lz-live probe     --reviewed-sha <sha> <probe-root> [--plan-only] [--deadline <dur>]
lz-live probe     --reviewed-sha <sha> --cleanup <run-id>
lz-live plan|apply|destroy --reviewed-sha <sha> <instance|all>
lz-live chain     --reviewed-sha <sha> <instance|all> [--deadline <dur>]
```
The guard runs first, before any credential is read. Exit codes: 0 pass (also with recorded known
deviations); 1 fail; 2 blocked (missing prerequisite or producer artefact); 3 refused (guard, lock,
binding, retained-resource protection, an existing admin under `bootstrap --fresh-account`). Every verb that applies (`probe`, `bootstrap`, `apply`,
`chain`) plans to a file, passes it through `protect.go` and applies only that file. Output lines are `LZ-LIVE <phase> <instance> <status>
<detail>`; no secret, no raw plan JSON. Every run prints its run id and, at the end,
`LZ-LIVE summary <run-id> <outcome> known-deviations=<ids|none>` and
`record approximate cost for run <id> in the PR`.
