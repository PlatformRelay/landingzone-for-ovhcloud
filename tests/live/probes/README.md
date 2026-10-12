# Probe run sheet (spec 005, T009 and T010)

Live probe roots for the premises of `specs/005-first-landing-zone-slice/spec.md` (*Premises*).
Written in T008; run only by the owner, only through `task live:probe` (`lz-live probe`: host
guard, account binding, child environment, deadline, retained-resource guard, inventory,
destroy-on-exit, redaction, leftover check). No agent runs anything here.

## Before a session

- Run from the dedicated owner clone `~/Projects/PlatformRelay/lz-live` (D92; quickstart
  *Owner session*): a plain clone, clean, `HEAD` at the reviewed SHA on `origin/main`;
  `~/.config/ovh-lz/live.env` with `LZ_OWNER_CHECKOUT` and `LZ_AGENT_WORKTREE_ROOT`;
  `accounts/<account>/account.env` with `LZ_ACCOUNT_ID`, `OVH_ENDPOINT`, `LZ_ORG`,
  `LZ_ADMIN_POLICY_ID`, `LZ_PROJECT_ID_STATE` (the probes' project; without it `lz-live probe`
  stops before any tofu call) and the other `LZ_PROJECT_ID_*` the leftover check lists.
- `MISE_ENV=live mise install` (OpenTofu 1.13.0). The leftover check needs no `ovhcloud`: it lists
  through lz-live's own API client (T084).
- T010 only: T065 has qualified the leftover parser on T009's captured listings (research R12).

```sh
cd ~/Projects/PlatformRelay/lz-live
git fetch origin && git checkout --detach <reviewed sha> && git status --porcelain   # empty
S=<reviewed sha>
```

Every run prints `LZ-LIVE run <run-id> start probe tests/live/probes/<root>` first. Its record is
`.local/live/<run-id>/` in the clone: `plan-<root>.txt` (rendered plan), `inventory.jsonl`,
`listings/<type>.json` (one file per leftover kind listed), `leftovers.json`, `summary.json`
(`outcome`, `plan_only`, `error`), `errors.txt` when a tofu call failed (its stderr, and a
failed apply's diagnostics decoded to plain text, redacted), and `observations.json` for a root
with a check (`state-backend`, row 7b; `storage-iam`, rows 8b–8d). Copy the run id, the outcome and the observations into the
task's evidence file, never the record itself (it may hold ids and the account e-mail).

## What every root shares

- State: `backend "local" { path = var.state_path }` with client-side encryption
  (`state_passphrase`), both set by `lz-live probe` as `TF_VAR_state_path` /
  `TF_VAR_state_passphrase`: `~/.config/ovh-lz/accounts/<account>/state/probes/<run-id>/<root>.tfstate`.
  `lz-live` places the file; the `state_path` validation is a shape check on top (a path not of
  the form `…/.config/ovh-lz/accounts/<account>/state/probes/<run-id>/<root>.tfstate`, or another
  root's file, fails the plan). No root holds a backend path, a credential or a `*.tfvars`.
- tofu's data directory: `lz-live` gives every run, and every `--cleanup`, its own `TF_DATA_DIR`
  in the run's scratch HOME, removed when the run ends. That HOME is created under `$TMPDIR`
  (default `/tmp`): keep `TMPDIR` unset or outside the clone, or it lands in the clone. A root's backend path
  changes with each run id, so a second run of a root (T009 then T010 for `alerting`, any rerun,
  a cleanup after a later run) initialises afresh instead of failing on "Backend configuration
  changed", and nothing is written into `tests/live/probes/<root>/`.
- Run id: `TF_VAR_run_id`, set by `lz-live`; the root refuses a run id other than the name of the
  state file's directory.
- Project: `TF_VAR_project_id`, set by `lz-live` from `LZ_PROJECT_ID_STATE` of `account.env`; the
  root takes that project from `data "ovh_cloud_projects"`, and the plan fails when the account
  has no project with that id. The leftover check lists every `LZ_PROJECT_ID_*` of
  `account.env`.
- Names: `lzprobe-<root>-<run-id>` (buckets: lowercased, P21). Tags: `lz:run-id = <run-id>` on
  every resource that takes tags (buckets); the project URN gets `lzprobe-p15:run-id`. Resources
  without a name or tags (alert, quota) are matched by the id the inventory recorded.
- Provider `ovh/ovh` 2.21.0, pinned by the committed `.terraform.lock.hcl` (linux_amd64 hashes);
  `lz-live` initialises with `-lockfile=readonly`, so a changed provider fails `init` before any
  plan.

## Runs

`project-import` and `quota` run only with `--plan-only`: without it `lz-live probe` refuses them
(exit 3, naming `--plan-only`) before any credential is read or any tofu call is made.

| # | Root | Premises | Task | Command | Expected observation (pass) | Refuted when | Destroy step |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | `project-import` | P5, P6 | T009 | `task live:probe -- --reviewed-sha $S tests/live/probes/project-import --plan-only` | plan: `module.project.ovh_cloud_project.this` *will be imported*, id = the sandbox project; no `must be replaced`, no create, no order; in-place changes allowed and recorded attribute by attribute (expected: `deletion_protection`, `plan`) | the plan fails on `prevent_destroy` (a replacement) or plans a create; then `reference` mode in T039 (research R7) | none (plan only: no state is written, the probe files go with the run) — **never run without `--plan-only`**: an apply would import the retained project into probe state and change it in place, and the destroy-on-exit and every `--cleanup` would then fail on `prevent_destroy` (the project survives; stop and report it to the coordinator, do not edit the probe state by hand) |
| 2 | `alerting` | P10 | T009 | `task live:probe -- --reviewed-sha $S tests/live/probes/alerting --plan-only` | plan: one `ovh_cloud_project_alerting.probe` to create on the sandbox project | plan error (e.g. alerting not offered on a trial project) | none |
| 3 | `quota` | P11 | T009 | `task live:probe -- --reviewed-sha $S tests/live/probes/quota --plan-only` | plan: `ovh_cloud_quota.probe` with `prevent_automatic_quota_upgrade = true` and the project's current regions/profiles unchanged; output `current_prevent_automatic_quota_upgrade` recorded | plan error, or a plan that changes a region profile | none — **never run without `--plan-only`** (singleton; destroy semantics UNVERIFIED) |
| 4 | `alerting` | P10 | T010 | `task live:probe -- --reviewed-sha $S tests/live/probes/alerting` | apply creates the alert; destroy removes it; `GET /cloud/project/<p>/alerting` no longer lists its id | apply or destroy error | destroy-on-exit (automatic) |
| 5 | `network` | P12 | T010 | `task live:probe -- --reviewed-sha $S tests/live/probes/network` | network `lzprobe-network-<run-id>` in GRA11 and subnet `10.250.0.0/24` (no gateway, DHCP) created without any vRack step and destroyed; no line item on the bill (P12 cost) | apply asks for a vRack or a gateway; a charge appears | destroy-on-exit |
| 6 | `iam` | P7, P8 (object), P15 (URN) | T010 | `task live:probe -- --reviewed-sha $S tests/live/probes/iam` | client `lzprobe-iam-<run-id>` and policy of the same name created by the admin and deleted on destroy (P7); the policy accepts the client's `identity` URN (P8, object half); the project URN accepts the key `lzprobe-p15:run-id` (P15); after destroy that key is gone and other project tags are unchanged | 403 on client or policy create (P7: extend the admin policy, R13); policy rejects the identity URN; tag key refused | destroy-on-exit |
| 7 | `state-backend` | P13, P14 (bucket), P15 (bucket), P1–P3 infrastructure | T010 | `task live:probe -- --reviewed-sha $S tests/live/probes/state-backend` | versioned bucket `lzprobe-state-<run-id>` in GRA with tag `lz:run-id` (P15 bucket half) and Object Lock, default retention governance `P1D` (T099, as `modules/object-storage-protected` locks a state bucket); S3 user `lzprobe-state-<run-id>` with one credential and a bucket-scoped S3 policy; the janitor user `lzprobe-state-janitor-<run-id>` (row 7b's undo) with one credential and `s3:*` plus `s3:BypassGovernanceRetention` on that bucket only; all destroyed, bucket included (P14, empty bucket: row 7b's undo leaves no version) | tag key refused; versioning not enabled; the bucket create refuses `object_lock` (record the error: Object Lock not offered in GRA, T099 BLOCKED); destroy leaves the bucket | destroy-on-exit |
| 7a | `state-backend` (S3 policy Deny) | T016 Deny (UNVERIFIED) | T010 | row 7's command (one run) | the apply creates `ovh_cloud_project_user_s3_policy.probe` with the `DenyGivenActions` statement of `components/state-backend` (eleven actions, `Resource = ["*"]`, including the patterns `s3:DeleteObjectVersion*`, `s3:PutBucketPolicy*`, `s3:DeleteBucketPolicy*`; T094, after run 20261010T172836Z-5bcc refused the earlier list); `GET /cloud/project/<p>/user/<u>/policy` returns the Allow statement's six actions and the Deny statement's eleven, each verbatim (patterns neither dropped nor expanded); keep the response body in the record | the policy create is refused (record the whole 400 body and compare it with `tests/fixtures/ovhcloud/s3-policy-actions.json`), or the returned policy drops or rewrites an action: record which, and the state-backend Deny list is changed to match before any `bootstrap` apply. Whether the Deny is *enforced*, and what the `*` patterns match, is row 7b's observation (T096; `evidence/T094.md` of spec 005, *Pattern semantics*) | destroy-on-exit (with row 7) |
| 7b | `state-backend` (Deny and Object Lock enforced, T096, T099) | T094 Deny patterns, T099 Object Lock and governance bypass (UNVERIFIED until this run) | T010 | row 7's command (one run) | after the apply, lz-live's check runs and writes `observations.json` (`check: state-backend-deny`) with ten observations, each `as_expected: true` and `findings: []`. With the state user's credential: `put-v1`, `put-v2` (PutObject of `lz-deny-check/probe.tflock`, 200, two distinct version ids; no Content-MD5, like the backend with `skip_s3_checksum`), `get` (200), `delete-older-version` (DeleteObject with the first version's `versionId`: refused, 403 `AccessDenied` — the lock, since run 20261010T201153Z-ffbf showed the Deny inert), `delete-older-version-bypass` (the same with `x-amz-bypass-governance-retention: true`: refused, 403 `AccessDenied`), `put-bucket-policy` (refused, 403 `AccessDenied`), `suspend-versioning` (PutBucketVersioning Suspended: refused, 403 `AccessDenied`), `delete-current` (DeleteObject without `versionId`, 2xx). With the janitor's: `janitor-delete-locked` (DeleteObject with the second version's `versionId`, no bypass: refused, 403 `AccessDenied` — the retention holds even for `s3:*`), `janitor-delete-bypass` (the same with the bypass: 2xx). The terminal shows `LZ-LIVE undo removed 2 versions; bucket empty` before the destroy, and the destroy succeeds | `delete-older-version` or `janitor-delete-locked` observed `allowed`: Object Lock is not in force on the bucket — T099 stops BLOCKED (owner options); the bypass step after it then reads `skipped` (its version is gone, so it says nothing about the bypass; no request, no finding); `delete-older-version-bypass` allowed: the state user can bypass the retention (its Deny on `s3:BypassGovernanceRetention` inert), a finding for the owner, `bootstrap` (T044) waits; `janitor-delete-bypass` refused (after `janitor-delete-locked` refused): the bypass does not work — T099 BLOCKED, and the undo fails (the root is kept; the versions stay locked for one day, then `--cleanup <run-id>`); `put-v1`/`put-v2` refused (e.g. 400 for a missing Content-MD5): the S3 backend with `skip_s3_checksum` cannot write to a locked bucket, a finding T044 must resolve; `put-bucket-policy` refused with `NotImplemented`: record it (T094 *Pattern semantics* 6); `suspend-versioning` refused with another code (a locked bucket may refuse a suspension for anyone): record it; any other backend step (`get`, `delete-current`) refused: the policy blocks the S3 backend, `bootstrap` must not apply it (rerun once first: how soon a new S3 policy takes effect is UNVERIFIED); an observation `not-run` or `no-answer` (the run fails): the observation is incomplete, rerun | destroy-on-exit: the check's undo first (janitor, with the bypass), then the root; a failed undo keeps the root, `--cleanup <run-id>` undoes again |
| 8 | `storage-iam` | P9, P26, P25 (objects) | T010 | `task live:probe -- --reviewed-sha $S tests/live/probes/storage-iam` | client and policy `lzprobe-storage-iam-<run-id>` with exactly the 13 R6 actions on the project URN; buckets `lzprobe-p25a-…`/`lzprobe-p25b-…` tagged `lz:tenant = lzprobe-a|b`; client and policy `lzprobe-p25-<run-id>` with the `resource.Tag(lz:tenant)` condition accepted; all destroyed | policy create refuses an action name or the condition key with `:` (record, P25 then refuted for that form) | destroy-on-exit |

Runs 1–3 are T009's; their leftover checks also produce T009's listings (`listings/<type>.json`
for every kind below). T009 also records the account binding (`GET /auth/details` with the admin
credential, P26 admin half) from the run's binding step. Runs 4–8 are T010's, one at a time, in
this order (cheapest and least privileged first). Record the approximate cost after each session.

**A failed destroy** (summary `outcome: fail`, error naming `destroy`): in the same session,

```sh
task live:probe -- --reviewed-sha $S --cleanup <run-id>
```

which destroys from the retained encrypted state and reruns the leftover check; the state and
passphrase under `accounts/<account>/state/probes/<run-id>/` are deleted only when both pass.
Never delete them by hand while a resource may exist.

## Leftover listing per matrix kind

The leftover check runs after every run, plan-only included, over every kind of research R12's
matrix, and writes `listings/<type>.json` and `leftovers.json`. Pass: `leftovers.json` has
`"outcome":"pass"`, no leftover and no error. It lists through lz-live's own read-only API client
with the bearer token of the run's bound sandbox credential (T084; ovhcloud 0.15.0 has no `api`
command, P18 refuted): GET only, `/iam/*` on APIv2, every other path on APIv1, a listed id escaped
as one path segment. Pagination follows the APIv2 cursor headers (`X-Pagination-Cursor`,
`X-Pagination-Cursor-Next`); whether v1 listings paginate that way is UNVERIFIED until T084's
captured listings. A listing error, a non-2xx answer, an answer over 8 MiB, a repeated cursor or
more than 1000 pages is `fail`, never skipped.

| Kind (provider type) | Listing | Probe roots that create it | A probe leftover is |
| --- | --- | --- | --- |
| bucket (`ovh_cloud_project_storage`) | `/cloud/project/<p>/region` → `/cloud/project/<p>/region/<r>/storage` | state-backend, storage-iam | a bucket named `lzprobe-…` |
| private network (`ovh_cloud_project_network_private`) | `/cloud/project/<p>/network/private` | network | a network named `lzprobe-…` |
| subnet (`ovh_cloud_project_network_private_subnet`) | `/cloud/project/<p>/network/private/<n>/subnet` per network | network | a subnet of a probe network, or an inventory id |
| cloud project user (`ovh_cloud_project_user`) | `/cloud/project/<p>/user` | state-backend | a user whose description starts `lzprobe-` |
| S3 credential (`ovh_cloud_project_user_s3_credential`) | `/cloud/project/<p>/user/<u>/s3Credentials` per user | state-backend | a credential of a probe user |
| S3 policy (`ovh_cloud_project_user_s3_policy`) | `/cloud/project/<p>/user/<u>/policy` per user | state-backend | the policy of a probe user |
| OAuth2 client (`ovh_me_api_oauth2_client`) | `/me/api/oauth2/client` → `/me/api/oauth2/client/<id>` | iam, storage-iam | a client named `lzprobe-…` (`lz-sandbox-admin` is exempt by its id only) |
| IAM policy (`ovh_iam_policy`) | `/iam/policy` (v2) | iam, storage-iam | a policy named `lzprobe-…` (the admin policy is exempt by `LZ_ADMIN_POLICY_ID` only) |
| identity group (`ovh_me_identity_group`) | `/me/identity/group` → `/me/identity/group/<name>` | none | any group named `lzprobe-…` |
| IAM resource tags (`ovh_iam_resource_tags`) | `/iam/resource/<project urn>` (`tags`) | iam | a tag key `lzprobe-…` or a value holding the run id |
| project alert (`ovh_cloud_project_alerting`) | `/cloud/project/<p>/alerting` → `/cloud/project/<p>/alerting/<id>` | alerting | the alert id the inventory recorded |
| quota (`ovh_cloud_quota`) | none: a property of the project, nothing to leave behind | quota (plan only) | — |
| project (`ovh_cloud_project`) | none: retained, never destroyed | project-import (plan only, import) | — |

To look again by hand after a failed check (read only, same paths), with the admin credential
loaded only in that shell: the API console of the endpoint, or the `ovhcloud` subcommand of the
kind where one exists (e.g. `ovhcloud cloud project list`; per-kind coverage UNVERIFIED). There is
no generic `ovhcloud api get` (P18 refuted, evidence/T009.md).

## Two-stage runs (T010)

Some observations need a credential a probe root creates, used by a second client in the same
run. A root with a `companion/` directory (today `storage-iam/companion/`) runs in two stages
under the one `task live:probe` command of its row:

1. The root applies under the admin credential and publishes the companion's variables as the
   sensitive output `companion_env` (storage-iam: the P9 tenant identity as `OVH_CLIENT_ID` /
   `OVH_CLIENT_SECRET`, the P1 S3 user's keys as `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`;
   since T100 the P25 identity is the storage-iam check's, below).
2. `lz-live` reads that output, writes it 0600 to
   `~/.config/ovh-lz/accounts/<account>/state/probes/<run-id>/companion.env`, refuses (before
   binding anything; the root is still destroyed) a name reserved for the run —
   `OVH_ENDPOINT`, `TF_VAR_state_passphrase`, `TF_VAR_state_path`, `TF_VAR_run_id`,
   `TF_VAR_project_id`, `HOME`, `TMPDIR`, `PATH` and every `TF_*` name other than `TF_VAR_*`, in
   any case — binds every
   published identity to the account (`GET /auth/details`; an identity of another account stops
   the run with exit 3 before the companion, and the root is still destroyed), initialises the
   second writer's data directory, then plans and applies the companion with those variables in
   place of the admin credential. When the companion's apply reports its first resource
   operation, a second writer of the companion's state starts (`plan -lock-timeout=0s`); its
   output goes to `second-writer-storage-iam-companion.txt` in the run record, and its refusal
   does not fail the run.
3. On every way out the companion is destroyed first (under the identity), then the root (which
   removes the identities), then `companion.env` is removed; a companion that cannot be destroyed
   keeps the root (below).

`--plan-only` on such a root plans the root only: nothing is applied, so there is no identity and
the companion does not run. `--cleanup <run-id>` re-reads `companion_env` from the root's retained
state, binds it again, destroys the companion, then the root, and removes `companion.env`; a root
that published nothing (its apply failed before) has no companion to destroy.

The companion's destroy (`plan -destroy`, the judged plan, its apply) is retried as a whole after
a transient API failure — the failed call's output names a 5xx status (also as the OAuth2 token
answer), a transport failure on the OAuth2 token endpoint or a connection reset, and no 4xx
status — at most twice, after 20 s and then 60 s, and never
past the destroy deadline; a 4xx is never retried (T095, after run 20261010T173007Z-637a lost its
identity to an OAuth2 token 500). If the companion's destroy still fails, on a run or a cleanup,
**the root is kept, not destroyed**: it holds the identities the companion runs under, the
`lzprobe-p1-<run-id>` bucket with the companion's state and its S3 credential. The run ends `fail`
with an error naming the kept root and `--cleanup <run-id>`, and the leftover check reports the
kept root's resources and the companion's as leftovers (they are, until the cleanup). Run the
cleanup in the same session: it binds the identity again, destroys the companion, then the root,
and keeps the root again while the companion still cannot be destroyed, or its identity cannot be
read or bound. A published environment the run refused (wrong shape, reserved name, unpaired
credential) never started the companion: then the root is destroyed as before. If the companion can never be destroyed under its identity
(e.g. the allowlist lacks a delete), remove its resources as the admin, then run `--cleanup`
(its companion plan should then find nothing to destroy; UNVERIFIED), and record it in T010's evidence.

The second writer's record always exists once the companion stage began: its init output, then
`second writer exit: …`, or `second writer never started: …` with the reason (the companion's
apply reported no resource operation, or the companion stopped before its apply).

| # | Root (stage) | Premises | Task | Command | Expected observation (pass) | Refuted when | Destroy step |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 8a | `storage-iam` → `companion` (state) | P1, P2, P3 | T010 | row 8's command (one run) | the companion's `init` and `apply` succeed on the S3 backend in `lzprobe-p1-<run-id>` with the OVHcloud options (P2) and enforced client-side encryption (P3); `second-writer-storage-iam-companion.txt` holds `Error acquiring the state lock` and a non-zero exit (P1) | the S3 backend or the lock file is refused (P2); the second writer's plan succeeds while the apply runs (P1: read the record's exit line) | destroy-on-exit: companion, then root |
| 8b | `storage-iam` → `companion` (tenant identity) | P9 usage, P26 tenant | T010 | row 8's command | the tenant identity creates and destroys network and bucket `lzprobe-p9-<run-id>` and subnet `10.251.0.0/24` (P9); the binding step passes; `observations.json` (`check: storage-iam`): `p26-get-me` refused 403 `account:apiovh:me/get` and `p9-iam-write` (`POST /v2/iam/policy`, a harmless policy `lzprobe-p9-write-<run-id>`) refused 403 `account:apiovh:iam/policy/create`, both `as_expected: true`; the terminal shows `LZ-LIVE undo removed 0 IAM policies named lzprobe-p9-write-<run-id> or created by the write` before the root's destroy | a create or delete is denied (the apply or destroy fails naming the action: record it); `p26-get-me` or `p9-iam-write` observed `allowed` (the run fails; an accepted write is deleted by the undo, by name and by the id the `POST` returned, which then prints `removed 1`; `policy_id` records the id) | destroy-on-exit |
| 8c | `storage-iam` → check (P25 identity) | P25 evaluation | T010 | row 8's command | P25 is refuted (T010 run 20261012T072729Z-0646: bucket A refused 403 `region/storage/get` although the policy's tag condition names it; coordinator decision D107, T101): `p25-read-bucket-a` and `p25-read-bucket-b` both refused 403 `publicCloudProject:apiovh:region/storage/get`, both `as_expected: true`. A refused bucket A is equally what a P25 policy not yet in effect would answer (IAM propagation delay UNVERIFIED): a `pass` does not show the tag condition was evaluated | `p25-read-bucket-a` allowed (the run fails, `LZ-LIVE finding p25-read-bucket-a: expected refused, observed allowed 200; P25 holds after all: re-open tag-scoped envelopes (D107)`: record it and re-open tag-scoped envelopes), or `p25-read-bucket-b` allowed | destroy-on-exit |
| 8d | `storage-iam` → check (state object) | P3 | T010 | row 8's command | `p3-state-object`: the P1 S3 key reads `storage-iam-companion.tfstate` back (200) and it is an OpenTofu encryption envelope (`detail: encryption envelope`, `as_expected: true`); record `sse` if present (the `x-amz-server-side-encryption` answer header: whether OVHcloud sends it on a GET is UNVERIFIED, the probe bucket has no default encryption set) | `observed: plaintext` (a state with `resources`, or no `encrypted_data`) or `unreadable`; refused (the P1 user lacks `s3:GetObject`: record the code) | destroy-on-exit |

**The storage-iam check (rows 8b–8d, T100).** After the companion's apply, lz-live reads the root's
sensitive output `iam_check` (the two buckets, the project and its URN, region, run id, the state
bucket and key, the tenant identity's URN — public — and the tenant, P25 and P1 S3 credentials)
from `tofu output -json` into memory only and redacts every credential. It binds the P25 and the
tenant identity to the account (`GET /auth/details`) before using them, then makes five
observations in order: `p25-read-bucket-a`, `p25-read-bucket-b`, `p26-get-me`, `p9-iam-write`,
`p3-state-object`. Each keeps only the status, the IAM actions a 403 names
(`details.unauthorizedActionsByIAM` or the message `missing IAM permissions: …`) or the S3 error
code, and the SSE header; a refusal counts as expected only when it is a 403 naming the step's
action or none (bucket A's must name it: D107). **Unlike the state-backend check, every answer other than the expected one fails
the run** (`LZ-LIVE finding <step>: expected …, observed …`, listed under `findings`, and the run's
error). Since D107 (T101) the bucket-A read is expected refused, as run 20261012T072729Z-0646 saw it
(P25 refuted), so a run whose observations match that run's ends `pass`; a bucket-A read allowed
after all fails the run and says P25 holds.
Before the root's destroy, on every way out and on `--cleanup`, the undo lists `/v2/iam/policy` with
the sandbox admin's credential, deletes every policy named `lzprobe-p9-write-<run-id>` and, in the
same process, every policy id an accepted write returned (T101: a listing that lags behind the
create, or a normalised name, would miss it), each once — the known ids also when the first
listing fails — and lists again, keeping the name and those ids (a delete acknowledged but not
effective under another name); one left (or a refused second listing or delete — the other
deletes are still tried) keeps the root
for `--cleanup`, which knows the name only (the id is in `observations.json`). The undo line counts
the deletes OVHcloud answered; a 404 (expired, gone already) removed nothing. The undo also runs when the
companion could not be destroyed (the root is then kept anyway), and other policies of the account
are skipped whatever their name. The write also expires an
hour after the run, and the leftover check lists `lzprobe-` policies. Copy `observations.json`'s
observations and findings into T010's evidence. The OVHcloud answers the fakes assume (the 403 body,
the v2 policy listing's pages, the SSE header) are UNVERIFIED until this run.

Before T100 these reads were check blocks in the companion, where a refused read is only an
OpenTofu warning: run 20261012T072729Z-0646 printed `pass` while bucket A was refused. That refusal
is now the expected answer (D107): the defect was the warning, not the observation.

**The state-backend check (row 7b).** lz-live reads the root's sensitive output `deny_check`
(bucket, region, the state user's and the janitor's S3 keys) from `tofu output -json` into memory
only — no file, no argv, no child environment — and redacts every key from the run's output and
record. The check signs with lz-live's own SigV4 client, keeps of each answer only the status and
the S3 error code, and never prints a key. A finding (an answer other than the expected one) is
listed in `observations.json` under `findings` and printed as `LZ-LIVE finding <step>`; it does not
fail the run. Before the root's destroy, on every way out (pass, failure, Ctrl-C, deadline) and on
`--cleanup`, the janitor deletes a bucket policy if one is set, re-enables versioning if it is not
`Enabled`, deletes every version and delete marker with the governance bypass (T099: the bucket
locks every version for a day) and lists the bucket again: anything left keeps the root, with the
identities, for `--cleanup`. The janitor's rights rest on the pattern `s3:*` and the named
`s3:BypassGovernanceRetention`, accepted by OVHcloud but UNVERIFIED in effect: if its first call
(`GetBucketPolicy`) is refused with 403 on the run and again on `--cleanup`, the check can never
empty the bucket; if only its bypass is refused, the versions unlock one day after they were written
and `--cleanup <run-id>` may then empty the bucket (UNVERIFIED: whether a DELETE that still carries
the bypass header is refused then). Otherwise, in the Control Panel (after that day), delete every
object version of
`lzprobe-state-<run-id>`, the bucket and the users `lzprobe-state-<run-id>` and
`lzprobe-state-janitor-<run-id>`; look again by hand (*Leftover listing per matrix kind*); only then
delete `accounts/<account>/state/probes/<run-id>/`; record it in T010's evidence. Copy `observations.json`'s observations (step,
observed, status, code) and its findings into T010's evidence.

Still not run by any root (T010 records them `not-run`):

- P14 (versions): object versions left in the `state-backend` bucket at its destroy, to see whether
  destroy removes noncurrent versions too (row 7b's undo empties the bucket first, so its destroy
  observes only the empty-bucket half).
- P8 (usage): the `iam` client reads `GET /cloud/project/<p>` and is denied
  `GET /cloud/project/<p>/region`.
- P9 (IAM write denied): the negative IAM write is V010's.
