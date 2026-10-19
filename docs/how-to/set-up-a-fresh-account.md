---
references:
  - path: tools/internal/live/bootstrap.go
    blob: 60d561756d64e3d1b45349988d4856f5406b7b2f
  - path: tools/cmd/lz-live/bootstrap.go
    blob: feda64aa61134e79c189b1ece984ff607ae356a3
  - path: tools/internal/live/files.go
    blob: 7ef962a505c2f045ef3a681c8dbf3e01c3f05194
  - path: tools/internal/stacks/adapter.go
    blob: 353a4b6b62176345db1a9466df79988f980dd6a4
verified_against: {task: "3.53"}
last_verified: e48983dd592c328ae39b78ddbf9d7ceec39479be
---

# Set up a fresh OVHcloud account

A command sheet for the owner. It takes a new OVHcloud account from signup to a first chain run,
while the old account still exists beside it. Steps marked **(browser)** happen in a web
browser. Everything else runs on the maintainer workstation, in the owner clone
`~/Projects/PlatformRelay/lz-live`.

The background, every file and every exit code are in the
[runbook](run-the-first-slice.md). This sheet only puts the fresh-account steps in order. It
adds what the runbook leaves out: revoking the application, and switching between two accounts.

## Before you start

- **Run it while the old account still works.** The bootstrap moves the old account's
  `sandbox.env` aside. To find out which account that file belongs to, it signs in with it. If
  that sign-in fails (what happens to an expired trial account's credentials is UNVERIFIED), the
  run stops with "cannot tell which account sandbox.env belongs to". Then move the file by hand
  to `accounts/<old account>/sandbox.env` and re-run.
- **You need a reviewed SHA whose manifest has a new `spec.org`.** Bucket names are global
  across OVHcloud (`ovhcloud-docs/.../object-storage/s3-limitations.mdx:49-53`). The old
  account's state bucket (`lz-bkt-state`) is retained and still exists during the window
  (whether its name is freed when the account closes is UNVERIFIED), so the new account needs
  another discriminator, e.g. `lz2`. That change is a reviewed pull request on `main`
  (`task stacks:reconcile`, `task stacks:generate`, review, merge). The merged commit is the
  `<sha>` below. Merge it only after the old account's last live run: from that commit on, the
  live lane refuses the old account, because its `account.env` names the old org.
- The tools from the runbook's [Before you start](run-the-first-slice.md#before-you-start) are
  installed in the owner clone. `live.env` stays as it is: both accounts share it.

## 1. Sign up and create the Public Cloud project (browser)

1. **(browser)** Create the OVHcloud account
   (`ovhcloud-docs/.../account-information/ovhcloud-account-creation.mdx`).
2. **(browser)** In the Control Panel, create one Public Cloud project
   (`ovhcloud-docs/.../cross-functional/create-a-public-cloud-project.mdx:19-35`). The API has
   no endpoint for creating a first project (same guide, line 69).
3. **(browser)** Activate the project and register a payment method. Until then the project is
   in *discovery mode*, where no resource can be created (same guide, lines 37-44). The trial
   credit is applied on activation and lasts one month (lines 52-57).
4. **(browser)** Check that the credit shows in the new account's billing view before you run
   anything. The guide says a customer who has had a Public Cloud project or a trial credit
   before is not eligible (line 59). Whether a second account of the same person counts as a
   new customer is UNVERIFIED. **No credit, no run:** without it, every resource goes on the
   card.

## 2. Create the temporary root keys, and revoke them afterwards

The bootstrap creates the account's admin service account (`lz-sandbox-admin`). For that it
needs the account root's API keys, once. Root keys bypass IAM (AGENTS.md), so they live for one
run only.

**Create** **(browser)**. Open the EU token page (`https://eu.api.ovh.com/createToken/`; the
provider docs link `api.ovh.com/createToken`, `terraform-provider-ovh/docs/index.md:88`. Which
one issues `ovh-eu` keys for a new account is UNVERIFIED). Sign in with the **new** account, then
fill in:

- rights: `GET` `/*`, `POST` `/*`, `DELETE` `/*` (the bootstrap sends only these three methods,
  `tools/internal/live/bootstrap.go`; narrower path rights are UNVERIFIED);
- validity: the shortest the page offers (the page has a validity field,
  `ovhcloud-docs/.../api/first-steps.mdx:129`).

The page gives you three keys: AK, AS and CK. Keep the page open. Do not paste them anywhere.
You type them at the bootstrap's prompt in step 4.

**Revoke.** Once all three keys are typed, the bootstrap revokes the consumer key (CK) itself
at the end of the run, also a failed or interrupted one, and prints `LZ-LIVE revoke bootstrap ran`.
The application (AK and AS) stays. After the run:

1. **(browser)** Open the Control Panel's *API keys* page
   (`ovhcloud-docs/.../api/first-steps.mdx:196-218`) and delete the application you created.
2. If the run printed `LZ-LIVE revoke bootstrap fail`, **or no `revoke` line at all** (for
   example, you aborted at a key prompt), the keys are still live: delete them on the same page
   **now**, before you retry with new keys.

## 3. Keep the old account's files next to the new one's

Every local credential lives under `~/.config/ovh-lz/` (directories 700, files 600), never in a
repository. Each account has its own directory, named after the account id that OVHcloud's
`GET /auth/details` returns (the NIC handle). After the bootstrap the layout is:

```text
~/.config/ovh-lz/
  live.env                    shared by both accounts; unchanged
  sandbox.env                 the active account's admin: the new account after step 4
  accounts/<old account>/     account.env, state-passphrase.env, state.env, state/, sets/, locks/,
                              and sandbox.env, which the bootstrap moved here
  accounts/<new account>/     the same set, written by the bootstrap
```

You do not have to move anything yourself. The bootstrap moves the old `sandbox.env` into
`accounts/<old account>/`. It never overwrites a file that is already there, and it reads no
other file of the old account.

**Back up the new `state-passphrase.env`.** Without it, the new account's state cannot be read,
and the bootstrap never generates a second one.

**Only one account is active at a time.** The top-level `sandbox.env` picks it. To run something
against the old account during the window (for example its last cleanup), swap the files, and
swap them back afterwards:

```sh
cd ~/.config/ovh-lz
mv sandbox.env accounts/<new account>/sandbox.env
mv accounts/<old account>/sandbox.env sandbox.env
# … the old-account run, at a reviewed SHA from before the spec.org change …
mv sandbox.env accounts/<old account>/sandbox.env
mv accounts/<new account>/sandbox.env sandbox.env
```

`mv` keeps mode 600. The run records in the clone (`.local/live/records/`) belong to no
account. That is harmless after a switch: every stack that consumes no producer artefact takes a
project id as input (`tools/internal/stacks/adapter.go`), so a new project id selects it again.

## 4. Run the bootstrap

```sh
cd ~/Projects/PlatformRelay/lz-live
git fetch origin
git checkout --detach <sha>
git status --porcelain          # must print nothing
S=<sha>
MISE_ENV=live mise exec -- task bootstrap:account -- --reviewed-sha "$S" --fresh-account
```

What the run asks and prints:

1. Three prompts with echo off: application key (AK), application secret (AS), consumer key (CK).
2. `LZ-LIVE identify bootstrap info moved the previous sandbox.env to accounts/<old account>/`.
3. The new account's project list, then one prompt for each project reference:
   `LZ_PROJECT_ID_STATE` and `LZ_PROJECT_ID_DEMO_DEV`. Give the same project id for both: the
   sandbox has one project (KD-1).
4. One `LZ-LIVE <phase> bootstrap <status>` line per phase: `guard`, `identify`, `passphrase`,
   `admin`, `state`, `publish`, `verify`, and `revoke` last. On a first run, each one after
   `guard` should read `ran`.

If it stops, the runbook's [fresh-account steps](run-the-first-slice.md#a-fresh-account-migration)
(step 5) say whether to re-run with or without `--fresh-account`. A bucket name that is already
taken fails with a message naming `spec.org`: merge a new `spec.org` (*Before you start*), set
`LZ_ORG` in `accounts/<new account>/account.env` to the same value (the run says so), and re-run
at the new SHA.

Then delete the application (step 2, *Revoke*).

## 5. Verify

Run the bootstrap once more, without the flag and without keys:

```sh
MISE_ENV=live mise exec -- task bootstrap:account -- --reviewed-sha "$S"
```

Every phase after `guard` must print `unchanged`. Keep the `LZ-LIVE` lines of both runs for the
T045 evidence. They name the account, so they go through a review before any commit; ids never
go into the repository.

## 6. The first chain

```sh
MISE_ENV=live mise exec -- task live:chain -- --reviewed-sha "$S" all
```

It applies the five lane stacks, judges the assertions, destroys the two ephemeral stacks and
runs the leftover check ([runbook: Run the chain](run-the-first-slice.md#run-the-chain)). Expect
`kd1-canary known-deviation KD-1` and a final `LZ-LIVE summary <run-id> pass known-deviations=KD-1`.
The new account's state users get the Object Lock read when they are created, so
`state-bucket-object-lock` should not be `blocked` for a missing grant. That the grant authorises
the read on OVHcloud is UNVERIFIED until a chain shows the lock read. Then note the run id and the approximate cost
from the new account's billing view.
