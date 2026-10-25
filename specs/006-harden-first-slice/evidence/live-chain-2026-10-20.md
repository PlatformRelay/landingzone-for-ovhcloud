# Live chain on the hardened code — 2026-10-20

- Run `20261020T000733Z-773f`, `task live:chain -- --reviewed-sha 818cf40 all`, from the owner's live
  clone, by the coordinator under D118. Read from the run record's `summary.json`,
  `observations.json` and `leftovers.json` in the live clone on 2026-10-25; account and project ids
  left out.
- Outcome: `pass`; known deviation KD-1 only. Leftover check `pass` (0 leftovers, 0 errors).
- Cost: within the voucher; voucher use 0.17 EUR in total at the 2026-10-20 reading (the chain's own
  share is not separable).
- Spec 006's code was all on `main` at `818cf40`. Spec 007's stage B fixes (#163) came after it.

## Observations

| Assertion | Subject | Observed | Detail |
|---|---|---|---|
| `state-bucket-versioned` | both state buckets | holds | versioning enabled |
| `bucket-tags` | both state buckets | holds | |
| `state-bucket-object-lock` | both state buckets | holds | governance, default retention 7 days |
| `state-object-encrypted` | five state objects (account, tenant state, project, network, runtime) | holds | encryption envelope |
| `state-lock-contention` | the project stack's state | holds | a second writer refused |
| `outputs-schema` | all six stacks | holds | |
| `deployer-binding` | platform and tenant `demo` | holds | each bound to the account |
| `tenant-iam-write` | tenant `demo` | denied | 403 |
| `tenant-s3-read` | tenant `demo` on the platform state bucket | denied | 403 |
| `kd1-canary` | bulk delete and `DELETE` | allowed | KD-1, the known deviation |

## What this settles

- T048's harness gap, row 7: the state buckets' S3 users may send `GetObjectLockConfiguration`, and
  the read answers the governance configuration: `state-bucket-object-lock` reads `holds`, not
  `blocked`, after the owner's bootstrap re-apply.
- The live-lane changes of spec 006 ran once against the account in a full chain.
