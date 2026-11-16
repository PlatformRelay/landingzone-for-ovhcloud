# Retained infrastructure — account `lz2` (second window)

- Written: 2026-11-16, before the final chain (T032). Requirement: FR-011. Plan: `window-lz2.md`
  row 3. Shape, cost notes, removal paths and removal order: as [retained.md](retained.md), which
  stays the first account's record. This file lists only what is on `lz2`.
- Source: the leftover-check listings of the latest run record, spec 003 T044's
  `20261116T115812Z-a689` (outcome `pass`), read in the live clone; the earlier
  `20261116T105653Z-29ec` after its `--cleanup` lists the same. No API call was made for this file.
- Names follow `spec.org` `lz2` (the first account used `lz`).

## Blockers

| # | Blocker | Status |
|---|---|---|
| B1 | Unreconciled probe runs under `state/probes/` | **none**: the directory is empty after T031 and the T044 cleanup |
| B2 | The latest record's leftover check | **clean**: `pass`, 0 leftovers, 0 errors |
| B3 | A resource a probe created | **none** listed: no `lzprobe-` bucket, user or Swift container |

No open blocker: T032 may start once its code dependencies are on `main` (window-lz2.md row 4).

## Rows on `lz2` now

| # | Kind | Name | Owning stack | As in retained.md | In the record |
|---|---|---|---|---|---|
| R2 | Object Storage bucket (versioned, Object Lock) | `lz2-bkt-state` | `account-bootstrap` | R2 | listed |
| R3 | S3 user | `lz2-s3u-state-platform` | `account-bootstrap` | R3 | listed |
| R4 | S3 credential of R3 | (access key) | `account-bootstrap` | R4 | kind listed; no name to match |
| R5 | S3 policy of R3 | (inline on R3) | `account-bootstrap` | R5 | kind listed; no name to match |
| R19 | OAuth2 client, the admin | `lz-sandbox-admin` | bootstrap `admin` phase | R19 | listed (the exempt client) |
| R20 | IAM policy of R19 | `lz-sandbox-admin` | bootstrap `admin` phase | R20 | listed (the exempt policy) |

The project (R1, ref `STATE` = `DEMO_DEV`) is referenced, not managed, as on the first account.
The other listed objects are OVHcloud's own and are never candidates: policy `ovh-default`, groups
`ADMIN`, `DEFAULT`, `UNPRIVILEGED`, and the region list.

## Rows the final chain adds

`account-governance`, `demo-state` and `demo-dev-project` have not been applied on `lz2` yet (no
deployer client, policy, group or tenant bucket in the record). T032's `chain -- all` applies them
and they stay: the rows R6–R18 of retained.md, with `lz2` in place of `lz`. T032's record confirms
them, and its leftover check must show none of them as a leftover.
