---
references:
  - path: docs/adr/0008-testing-strategy.md
    blob: d573786070470c6d2ce09f410762891940ce7f4a
  - path: specs/007-cleanup-first-live-slice/evidence/window.md
    blob: 88b69bfbd4ecf173df06d7c0467a76e4da438c6d
  - path: specs/007-cleanup-first-live-slice/evidence/T023.md
    blob: 666119813a4d642fe367698f6321ee4ce46b2367
  - path: specs/007-cleanup-first-live-slice/evidence/retained.md
    blob: f37d79bd0f458297c39d5802c9825513ad1d265c
  - path: specs/006-harden-first-slice/evidence/live-chain-2026-10-20.md
    blob: 38751ed662b4da8b583e325c0d9c76fdcb4a0dbb
  - path: specs/005-first-landing-zone-slice/evidence/T010.md
    blob: 16d7261c66b50f20a3bfcbb42e236771cea70c4c
  - path: specs/005-first-landing-zone-slice/evidence/T044.md
    blob: d43c9917e55aa3e80fff95eade0825919e998238
  - path: specs/005-first-landing-zone-slice/evidence/T049.md
    blob: ac0fe85adeb3ba89b02badbb4da3019e77c7b2c5
  - path: specs/003-platform-feasibility/evidence/T025.md
    blob: 22bfa1a2e93ba1172894e449d828b01ca32e861b
last_verified: fb968189fcc70955232a704509663c595151aeeb
---

# Test costs

What the live test runs have cost, as the repository records it.
[ADR-0008](../adr/0008-testing-strategy.md) asks for this summary. It lists recorded spend only.
An amount nobody read stays UNVERIFIED, and an estimate stays an estimate: it is never added up
as spend. The offline layers (L0–L4) run on the maintainer's machine and in CI and cost nothing
on OVHcloud.

## Total

| Account | Credit | Recorded use | Reading | Status |
| --- | --- | --- | --- | --- |
| First free-trial account | 200 € voucher, expired 2026-10-24 12:42 | 0.17 € | 2026-10-20, the last one ([T023](../../specs/007-cleanup-first-live-slice/evidence/T023.md)) | believed: the coordinator's Control Panel reading |
| First free-trial account, after 2026-10-20 | as above | UNVERIFIED | none | no reading between 2026-10-20 and the expiry |
| First account's separate credit | 10 €, valid to 2027-10-01 | UNVERIFIED | none | not planned spend ([window](../../specs/007-cleanup-first-live-slice/evidence/window.md#credit-and-bounds)) |
| Second free-trial account | its own voucher | none recorded | none | no run on it is recorded in the repository |

The 0.17 € is the whole voucher use at the time of the reading. It already included the spec 003
probes, since the same 0.17 € was read on 2026-10-16
([T025](../../specs/003-platform-feasibility/evidence/T025.md#cost)), and it covers the
2026-10-20 live chain, whose own share cannot be separated
([live chain](../../specs/006-harden-first-slice/evidence/live-chain-2026-10-20.md)). How the
0.17 € splits across runs is not recorded.

## Runs and what their records say

Every live run records its cost in its evidence (and, from spec 005 on, in its pull request).
"Estimate" means the run's author judged the cost from what it created and for how long. Nobody
then read it in the Control Panel.

| Date | Runs | Recorded cost | Kind |
| --- | --- | --- | --- |
| 2026-10-07 | Spec 005 plan-only probes ([T009](../../specs/005-first-landing-zone-slice/evidence/T009.md), [T084](../../specs/005-first-landing-zone-slice/evidence/T084.md)) | 0 €: nothing created | recorded |
| 2026-10-07 | Spec 005 leftover-listing run ([T087](../../specs/005-first-landing-zone-slice/evidence/T087.md)) | 0 € | recorded |
| 2026-10-10 to 2026-10-12 | Spec 005 create-and-destroy probes ([T010](../../specs/005-first-landing-zone-slice/evidence/T010.md#cost), [T096](../../specs/005-first-landing-zone-slice/evidence/T096.md), [T099](../../specs/005-first-landing-zone-slice/evidence/T099.md)) | well under 1 € | estimate |
| 2026-10-12 | Two account bootstraps ([T044](../../specs/005-first-landing-zone-slice/evidence/T044.md#cost)) | well under 1 € | estimate |
| 2026-10-13 | Four first-slice chains ([T049](../../specs/005-first-landing-zone-slice/evidence/T049.md#cost)) | well under 1 € | estimate |
| 2026-10-16 | Spec 003 probes ([T025](../../specs/003-platform-feasibility/evidence/T025.md#cost) to [T028](../../specs/003-platform-feasibility/evidence/T028.md)) | negligible; voucher total 0.17 € | estimate; reading |
| 2026-10-20 | The live chain on the hardened code ([live chain](../../specs/006-harden-first-slice/evidence/live-chain-2026-10-20.md)) | not separable; voucher total 0.17 € | reading |
| 2026-10-20 to 2026-10-24 | Spec 007: no run started ([T023](../../specs/007-cleanup-first-live-slice/evidence/T023.md)) | 0 € | recorded |

## What keeps running

The retained infrastructure of the first slice stays in place after the runs. Its IAM rows cost
nothing, and its buckets and S3 users are believed to cost close to 0 € a month. The bucket size,
and so the bucket cost, is UNVERIFIED. Who pays for it after the voucher expired (the separate
credit or the card) is UNVERIFIED until the owner's billing check. The list, with a cost per row,
is in [retained.md](../../specs/007-cleanup-first-live-slice/evidence/retained.md#rows).

## Keeping this page current

Add a row when a live run's record states its cost, and update the total from the next Control
Panel or `ovhcloud` reading. An estimate goes in as an estimate and a missing reading as
UNVERIFIED.
