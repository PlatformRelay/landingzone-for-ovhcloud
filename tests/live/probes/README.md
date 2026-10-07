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
- `MISE_ENV=live mise install` (OpenTofu 1.13.0), `ovhcloud` 0.15.0 on `PATH` (without it every
  leftover check fails closed).
- T010 only: T065 has qualified the leftover parser on T009's captured listings (research R12).

```sh
cd ~/Projects/PlatformRelay/lz-live
git fetch origin && git checkout --detach <reviewed sha> && git status --porcelain   # empty
S=<reviewed sha>
```

Every run prints `LZ-LIVE run <run-id> start probe tests/live/probes/<root>` first. Its record is
`.local/live/<run-id>/` in the clone: `plan-<root>.txt` (rendered plan), `inventory.jsonl`,
`listings/<type>.json` (one file per leftover kind listed), `leftovers.json`, `summary.json`
(`outcome`, `plan_only`, `error`). Copy the run id, the outcome and the observations into the
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
| 7 | `state-backend` | P13, P14 (bucket), P15 (bucket), P1–P3 infrastructure | T010 | `task live:probe -- --reviewed-sha $S tests/live/probes/state-backend` | versioned bucket `lzprobe-state-<run-id>` in GRA with tag `lz:run-id` (P15 bucket half); S3 user `lzprobe-state-<run-id>` with one credential and a bucket-scoped S3 policy; all destroyed, bucket included (P14, empty bucket) | tag key refused; versioning not enabled; destroy leaves the bucket | destroy-on-exit |
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
`"outcome":"pass"`, no leftover and no error. The `ovhcloud` argv (`ovhcloud api get <path>`) and
its pagination are UNVERIFIED until T065 (P18); a listing error is `fail`, never skipped.

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
loaded only in that shell: `ovhcloud api get <path>` (argv UNVERIFIED, P18).

## Two-stage runs (T010)

Some observations need a credential a probe root creates, used by a second client in the same
run. A root with a `companion/` directory (today `storage-iam/companion/`) runs in two stages
under the one `task live:probe` command of its row:

1. The root applies under the admin credential and publishes the companion's variables as the
   sensitive output `companion_env` (storage-iam: the P9 tenant identity as `OVH_CLIENT_ID` /
   `OVH_CLIENT_SECRET`, the P25 identity as `TF_VAR_p25_client_id` / `_secret`, the P1 S3 user's
   keys as `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`).
2. `lz-live` reads that output, writes it 0600 to
   `~/.config/ovh-lz/accounts/<account>/state/probes/<run-id>/companion.env`, binds every
   published identity to the account (`GET /auth/details`; an identity of another account stops
   the run with exit 3 before the companion, and the root is still destroyed), initialises the
   second writer's data directory, then plans and applies the companion with those variables in
   place of the admin credential. When the companion's apply reports its first resource
   operation, a second writer of the companion's state starts (`plan -lock-timeout=0s`); its
   output goes to `second-writer-storage-iam-companion.txt` in the run record, and its refusal
   does not fail the run.
3. On every way out the companion is destroyed first (under the identity), then the root (which
   removes the identities), then `companion.env` is removed.

`--plan-only` on such a root plans the root only: nothing is applied, so there is no identity and
the companion does not run. `--cleanup <run-id>` re-reads `companion_env` from the root's retained
state, binds it again, destroys the companion, then the root, and removes `companion.env`; a root
that published nothing (its apply failed before) has no companion to destroy.

If the companion's destroy (or its binding) fails, on a run or a cleanup, the root is still
destroyed, so no probe identity outlives the session. That also removes the identity and the
`lzprobe-p1-<run-id>` bucket that held the companion's state: a further `--cleanup` cannot reach
what the companion left. The leftover check lists it (`lzprobe-p9-<run-id>` network, subnet,
bucket); remove it as the admin in the same session (`ovhcloud`, or the Control Panel) and record
it in T010's evidence. The second writer's record always exists once the companion stage began: its init output, then
`second writer exit: …`, or `second writer never started: …` with the reason (the companion's
apply reported no resource operation, or the companion stopped before its apply).

| # | Root (stage) | Premises | Task | Command | Expected observation (pass) | Refuted when | Destroy step |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 8a | `storage-iam` → `companion` (state) | P1, P2, P3 | T010 | row 8's command (one run) | the companion's `init` and `apply` succeed on the S3 backend in `lzprobe-p1-<run-id>` with the OVHcloud options (P2) and enforced client-side encryption (P3); `second-writer-storage-iam-companion.txt` holds `Error acquiring the state lock` and a non-zero exit (P1) | the S3 backend or the lock file is refused (P2); the second writer's plan succeeds while the apply runs (P1: read the record's exit line) | destroy-on-exit: companion, then root |
| 8b | `storage-iam` → `companion` (tenant identity) | P9 usage, P26 tenant | T010 | row 8's command | the tenant identity creates and destroys network and bucket `lzprobe-p9-<run-id>` and subnet `10.251.0.0/24` (P9); the binding step passes and check `p26_me_denied` warns that `GET /me` was denied (P26) | a create or delete is denied (the apply or destroy fails naming the action: record it); check `p26_me_denied` fails with "P26 refuted" | destroy-on-exit |
| 8c | `storage-iam` → `companion` (P25 identity) | P25 evaluation | T010 | row 8's command | check `p25_bucket_a_readable` passes without a warning; check `p25_bucket_b_denied` warns that bucket B was denied | check `p25_bucket_b_denied` fails with "P25 refuted", or `p25_bucket_a_readable` warns (the identity was denied its own tenant's bucket) | destroy-on-exit |

Whether a denied read inside a `check` block is a warning rather than an error is believed
(Terraform semantics), UNVERIFIED for OpenTofu 1.13 until this run; record what the run printed.

Still not run by any root (T010 records them `not-run`):

- P14 (versions): two object versions written to the `state-backend` bucket before its destroy,
  to see whether destroy removes noncurrent versions too (the root's bucket is empty).
- P8 (usage): the `iam` client reads `GET /cloud/project/<p>` and is denied
  `GET /cloud/project/<p>/region`.
- P9 (IAM write denied): the negative IAM write is V010's.
