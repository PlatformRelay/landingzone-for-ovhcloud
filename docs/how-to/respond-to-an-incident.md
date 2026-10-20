---
references:
  - path: tools/cmd/lz-live/main.go
    blob: 0f07bee04de813455817624ddefbfa0b9a293aa2
  - path: tools/cmd/lz-live/probe.go
    blob: 093177583a9d931f5869336a1349142151db1c58
  - path: tools/internal/live/apply.go
    blob: 8960463e6b9aa9b86917f237aa969ab2cfda9ae3
  - path: Taskfile.yml
    blob: 040a14f1587853be135b2c91ca86204628d4e727
verified_against: {task: "3.53", tofu: "1.13"}
last_verified: df64a059046a97da780042092e1a670d5957df51
---

# Respond to an incident

This page is the incident runbook that
[ADR-0026](../adr/0026-observability-and-incident-response.md#incident-runbook) asks for. It
covers five cases: a leaked or suspected credential, lost or corrupt state, a failed run with
leftovers, a cost spike, and break-glass use. Each case gives the severity, who decides, the
first steps in order, and the `task` target or the existing procedure where one exists. Where
none exists, the case says so.

Everything here acts on a real account. **Only the owner runs it, on the maintainer
workstation**, with the same exception as the [runbook](run-the-first-slice.md): the
coordinating session may run `task live:probe`, `live:plan`, `live:chain` and `live:destroy`
under the bounded, expiring extension in [AGENTS.md](../../AGENTS.md) ("Known deviation",
"Bounded extension"). Subagents never run a live target. Every command below runs in the owner
clone at a reviewed SHA (`S`), as the runbook's
[Before you start](run-the-first-slice.md#before-you-start) describes.

## Severity and who decides

There is no paging and no out-of-hours rota
([ADR-0026, on-call model](../adr/0026-observability-and-incident-response.md#on-call-model-for-a-small-organisation)).

| Severity | Examples | Response |
| --- | --- | --- |
| Sev 1 | A credential that may have leaked, break-glass use nobody announced, state lost or corrupted, an unexplained resource in an account | Best effort, next working day. Whoever sees a possibly leaked credential revokes it **at once**. |
| Sev 2 | A failed apply or destroy, a leftover after a run, a budget threshold crossed | Next working day |
| Sev 3 | Drift findings, stale docs, a red CI check on a dependency update | Weekly triage |

ADR-0026 names an accountable recipient, a backup and a security recipient for every alert. They
are not chosen yet
([owner decisions](../adr/0026-observability-and-incident-response.md#owner-decisions-2026-10-20)).
Until they are, the owner decides every case below. Recipients live in private configuration,
never in the repository.

## Rules for every case

- Stop starting live runs until you know what happened. Run live verbs one at a time.
- Keep the run record under `.local/live/<run-id>/`. Do not delete it while you investigate.
- Never copy a credential, an account id or a project id into a commit, a PR, an issue or a
  chat. Copy the run id, the outcome and the assertion outcomes only
  ([Read a run record](run-the-first-slice.md#read-a-run-record)).
- Write down what you do as you do it. The incident note at the end is made from it.

## A leaked or suspected credential

**Severity:** Sev 1. Revoke first, investigate afterwards.

**Who decides:** whoever sees it revokes it, without waiting. The owner decides the re-issue and
the follow-up.

The credentials that exist are listed in the runbook's
[local files](run-the-first-slice.md#local-files-never-in-a-repository) table.

1. **Revoke it now.**
   - An OAuth2 service account: the admin in `sandbox.env`, or a platform or tenant deployer
     under `sets/account-governance/`. Detach its access policies in the Control Panel, which
     can be undone, or delete the service account, which cannot: in the Control Panel, or
     through `DELETE /me/api/oauth2/client/{clientId}`
     (`ovhcloud-docs/.../api/manage-service-account.mdx:104-115`, `:173-183`). A service
     account's secret cannot be regenerated, only replaced by a new service account (same
     guide, line 101). Whether tokens already issued to it stop working at once is UNVERIFIED.
   - Temporary root keys (AK, AS, CK) from a fresh-account bootstrap: delete the application on
     the Control Panel's *API keys* page
     ([Set up a fresh account, step 2](set-up-a-fresh-account.md#2-create-the-temporary-root-keys-and-revoke-them-afterwards);
     `ovhcloud-docs/.../api/first-steps.mdx:196-218`).
   - S3 keys (`state.env`, a tenant's `state.env` or `platform-state.env`): delete the key pair
     of that S3 user. The Control Panel path and the API call are UNVERIFIED here.
   - The state passphrase (`state-passphrase.env`): it cannot be revoked. Anyone who holds it
     and can read a state bucket can read that state. Revoke the S3 keys of every bucket it
     protects. No procedure rotates the passphrase today: the bootstrap never generates a
     second one. Rotating it is an owner decision.
2. Stop every live run. A run would fail on the revoked credential, or use the leaked one.
3. Find where it leaked: a commit, a PR, an issue, a log, a chat, a shared screen. Remove it
   there. A pushed commit stays in clones and forks: treat its secret as public whatever you
   rewrite afterwards.
4. Look for what it was used for. No account audit sink exists yet, so no API or login history
   reaches you (ADR-0026, *What the first slice already does*). Whether the Control Panel shows
   the account's API activity is UNVERIFIED. Look by hand for resources nobody's run created
   ([Clean up and check for leftovers](run-the-first-slice.md#clean-up-and-check-for-leftovers),
   last items). An unexplained resource stays Sev 1.
5. If the leak came from a defect in this repository (for example, a redactor that let a
   secret through to a run record), report it privately as
   [SECURITY.md](../../.github/SECURITY.md) says.
6. Re-issue the credential.
   - The admin: no `task` target re-issues a revoked admin, and that path has not been run.
     `task bootstrap:account -- --reviewed-sha "$S" --fresh-account` creates an admin with new
     root keys, but it refuses (exit 3) an admin that `account.env` names and that no working
     credential can reach ([A fresh account](run-the-first-slice.md#a-fresh-account-migration),
     step 5). Clear that by hand first. Owner only.
   - A deployer or an S3 key: the lane writes them when `account-governance` or a
     `tenant-state` instance applies. Whether `task live:apply` re-creates a deleted one, and
     whether the retained-resource guard lets it, is UNVERIFIED. Run `task live:plan` first
     and read the plan.

## Lost or corrupt state

**Severity:** Sev 1.

**Who decides:** the owner. Promoting a restored state is an explicit, logged step
([ADR-0025, State backup and restore](../adr/0025-network-and-resilience.md#state-backup-and-restore)).

Signs: a plan that cannot read or decrypt its state, a chain whose `state-object-encrypted`
assertion fails, a plan refused because a lock is held after its writer died.

1. Stop every run that writes the affected bucket. Write nothing to it until you have decided.
2. Note which instance's state is affected, the error, and the run id. Keep the run record.
3. **A stale lock** (the writer was killed and no `lz-live` process runs): release it with
   `tofu force-unlock -force <lock id>`, using the lock id from the refusal. This was observed
   with the state credential alone on a governance-locked probe bucket, not yet on a lane
   stack's state ([ADR-0009, Backend and locking](../adr/0009-state-backend-bootstrap-and-credentials.md#backend-and-locking)).
   No `task` target runs it, and no page says how to run `tofu` by hand against a lane stack's
   backend. Until one does, this is the owner's call.
4. **A lost or corrupt state object:** the bucket keeps earlier versions (versioning plus
   Object Lock in governance mode). Restore by copying an earlier version, as ADR-0009 records
   on a probe bucket. Copy into an **isolated** backend first, validate it, then promote it
   and fence the old writer (ADR-0025). No `task` target does this. The off-project backup of
   ADR-0025 (layer 2) is not built, so the versions in the bucket are the only copy today.
5. **A lost passphrase:** the state cannot be read, and nothing recovers it. The escrow of
   ADR-0009 is not built. Rebuilding the stacks is an owner decision.
6. **The local `account-bootstrap` state** (`accounts/<account>/state/<id>.tfstate`) is a file
   on the workstation. Its only copy is the owner's own backup.
7. A lost **run record** under `.local/live/records/` is not lost state: that stack looks never
   applied, and the next run selects it again
   ([Read a run record](run-the-first-slice.md#read-a-run-record)).
8. After any restore, plan the affected instance by name,
   `task live:plan -- --reviewed-sha "$S" <instance>`, and read the plan before any apply.
   `all` is not enough: it plans only the stacks whose code, inputs or records changed, so an
   unchanged stack with restored state is skipped. A consumer is refused while a producer it
   consumes is selected and not applied. The lane never plans `account-bootstrap`,
   and no target only plans it. An empty plan does not prove a rebuild worked (ADR-0009,
   *Recoverable bootstrap*).

## A failed run with leftovers

**Severity:** Sev 2 for a failed apply or destroy and for a leftover of a run. Sev 1 for a
resource no run created.

**Who decides:** whoever started the run, then the owner.

1. Read the run's last lines: `LZ_RUN <run-id> <verb> <outcome>`, or for a chain
   `LZ-LIVE summary <run-id> <outcome> …`. The exit code says pass, fail, blocked or refused
   ([Run the chain](run-the-first-slice.md#run-the-chain), exit code table).
2. Read the run record ([Read a run record](run-the-first-slice.md#read-a-run-record)). For a
   chain, read `leftovers.json`: a leftover, a listing error, or `blocked waiting-for=…`.
   `before_run` marks what was there before the run.
3. Clean up by the existing procedure
   ([Clean up and check for leftovers](run-the-first-slice.md#clean-up-and-check-for-leftovers)):
   - An ephemeral destroy failed: `task live:destroy -- --reviewed-sha "$S" <instance>`.
   - A probe's destroy failed: `task live:probe -- --reviewed-sha "$S" --cleanup <run-id>`.
     A probe run whose baseline cannot be trusted is refused. The owner's recovery is the same
     command with `--accept-lost-baseline`, at a terminal, where you type the run id back. Run it
     only after you have checked the project and removed by hand what the run created that its
     state does not hold. For `budget-alert` it destroys and deletes nothing: first remove by
     hand every alert that matches the probe's plan and was created at or after the run's
     start, the one in its state included (`tools/cmd/lz-live/probe.go`).
   - Never run `--cleanup <run-id>` while that run still runs in another terminal.
4. Look by hand, read only, before you conclude that something was left behind (same section).
   The leftover check misses some kinds; the section lists them.
5. A retained instance is never destroyed by a verb. Removing one is a manual Control Panel task
   ([Destroy](run-the-first-slice.md#destroy)).
6. A resource that no run's inventory or `lz-` prefix explains is Sev 1: handle it as
   [a leaked credential](#a-leaked-or-suspected-credential).
7. Record the approximate cost next to the run id in the PR.

## A cost spike

**Severity:** Sev 2 (a budget threshold crossed). Sev 1 if the cost comes from a resource no
run created.

**Who decides:** the owner. The spend stop is a manual cost check, not code
([AGENTS.md](../../AGENTS.md), "Bounded extension").

How you learn of it: a budget alert e-mail, if the project has one (it is switched off in the
sandbox manifest, `stacks/deployments.yaml`), or the owner's own billing check. A project budget
alert e-mails one address when its monthly threshold is crossed, at most once per `delay`
seconds (`terraform-provider-ovh/docs/resources/cloud_project_alerting.md:23-25`).

1. Open the project's billing in the Control Panel and read *Current usage* and the estimate of
   the next invoice (`ovhcloud-docs/.../public-cloud/cross-functional/analyze-billing.mdx:84-94`).
   Note what costs the most.
2. Stop starting live runs. Below 20 € of remaining credit, the coordinating session's live
   runs end anyway ([AGENTS.md](../../AGENTS.md), "Bounded extension").
3. Match the cost to a resource. One from a run: go to
   [a failed run with leftovers](#a-failed-run-with-leftovers). One nobody created: go to
   [a leaked credential](#a-leaked-or-suspected-credential).
4. Remove what costs. An ephemeral instance: `task live:destroy -- --reviewed-sha "$S" <instance>`.
   A retained one, or something outside the stacks, is removed by hand in the Control Panel
   ([Destroy](run-the-first-slice.md#destroy)).
5. Check the usage again the next working day. Record the cost in the PR or the incident note.
6. If the account's credit is spent, the maintainer moves to a fresh account
   ([Set up a fresh account](set-up-a-fresh-account.md)).

## Break-glass use

**Severity:** Sev 1 for use nobody announced.

**Who decides:** the security recipient (ADR-0026). None is named yet; until then, the owner.

**There is no break-glass route yet.** ADR-0026 says break-glass and planned elevation stay
unavailable until an account audit sink exists: forwarding in place and its alert seen to fire
([ADR-0026, Signals and where they go](../adr/0026-observability-and-incident-response.md#signals-and-where-they-go)).
No sink, no break-glass user and no planned elevation exist today, and no `task` target touches
them.

1. If you see signs of a login or an elevation that nobody announced, treat it as
   [a leaked or suspected credential](#a-leaked-or-suspected-credential).
2. Nothing alerts on such a login today. Do not assume it would have been seen.

When the sink and the route are built, their use alerts the security recipient the same day, and
this section gets its own steps.

## Close the incident

Every incident ends with a short note, in the PR or forge issue that carries its follow-up:

- **What happened:** the timeline, the run ids, the severity.
- **What was lost:** state, resources, credit, credentials. "Nothing" is an answer.
- **What changes:** the fix, the check that would have caught it, or why nothing changes.

The note holds no credential, account id or project id.

A cause that recurs becomes a known-issue record in [`docs/known-issues/`](../known-issues/README.md),
one `KI-NNN` file following its [template](../known-issues/TEMPLATE.md), with its row in the
index.
