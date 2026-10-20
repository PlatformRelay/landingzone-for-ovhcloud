# ADR-0026: Observability and incident response
- Status: Accepted (2026-10-20, D128)
- Date: 2026-10-20
- Related: ADR-0004, ADR-0006, ADR-0009, ADR-0017, ADR-0018, ADR-0020, ADR-0021, ADR-0022,
  ADR-0024, ADR-0025; spec 005, spec 007, spec 006 E5

## Context
Several accepted ADRs promise an alert without saying where it goes or who reads it: break-glass
use "alerts through the audit sink" ([ADR-0006](0006-guardrails-without-org-level-policy.md),
[ADR-0018](0018-identity-planes-roles-and-providers.md)); a failed or stale scanner alerts "the
explicitly configured accountable recipient" ([ADR-0020](0020-generated-landing-zone-documentation.md));
the cost design needs an alert recipient ([ADR-0024](0024-cost-and-sandbox-operations.md)). The
profile has an observability family (`ldp | byo | none`, [ADR-0016](0016-golden-paths-as-profiles.md),
[ADR-0017](0017-runtime-families-and-output-contract.md)), and the `observability` stage name is
reserved ([ADR-0004](0004-staged-roots-and-output-contracts.md)). The project has one maintainer and
promises no SLA ([ADR-0022](0022-support-release-upgrade-and-succession-policy.md)). The
pipeline-run live lanes (inventory B8) depend on this record
([spec 006, postponed row E5](../../specs/006-harden-first-slice/spec.md#out-of-scope--postponed-with-trigger)).

**Decision drivers**
- Fail closed: a missing observation or an unset recipient is never "healthy" (constitution V;
  ADR-0020).
- No secrets in any signal: credentials never reach logs, and raw state, plans and live logs stay
  private ([ADR-0021](0021-threat-model-authorisation-boundaries-and-supply-chain.md); constitution V).
- No telemetry: the product sends nothing to its maintainers (constitution VI).
- Small org: alerts must be few, owned by a named person, and answerable in working hours.
- Cost: Logs Data Platform (LDP) is billed and had no consumer in the first slice
  ([spec 005, stack set](../../specs/005-first-landing-zone-slice/spec.md#stack-set-d85-more-stacks-from-the-beginning-d88-admin-by-script-tenant-state-per-tenant),
  `observability` row, `spec.md:34`).

**OVHcloud facts this relies on** (docs mirror paths are under `docs.ovhcloud.com` guides):
- Account audit logs (logins, password changes), activity logs (API and Control Panel actions) and
  access-policy logs (IAM evaluations) can each be forwarded to an LDP stream through their own
  subscription route (`/me/logs/audit/log/subscription`, `/me/api/log/subscription`,
  `/iam/log/subscription`; `manage-and-operate/iam/logs-forwarding.mdx:38-40`, `:54-64`).
- LDP can alert on a stream: counter, numeric-value and text-content alerts
  (`manage-and-operate/observability/logs-data-platform/alerting.mdx:32-36`).
- A project budget alert sends e-mail to one address when a monthly threshold in currency is
  crossed, at most once per `delay` seconds (provider `cloud_project_alerting.md:23-25`).
- Whether OVHcloud offers a managed metrics service for Public Cloud resources: UNVERIFIED here.

## Options considered
| Option | Assessment |
| --- | --- |
| **A. LDP for everything** — every signal, from audit logs to run records, goes to an LDP stream in `account-fabric` | Strong search and alerting, and the audit source ADR-0018 asked for. Billed from day one for every installation, and it puts the alert path for LDP problems inside LDP. |
| **B. Forge and e-mail only** — run records, CI results, SARIF in the forge's code-scanning tab, budget e-mail; no log platform | Free and already half built. No home for account audit logs, so break-glass use and console changes go unseen until the scanner's next run. |
| **C. A small always-on floor, plus a pluggable sink** — the floor is budget e-mail, forge issues and run records; the sink (`ldp`, `byo` or `none`) adds audit-log forwarding and stream alerts when chosen | Recommended. `solo` and the sandbox run on the floor at no cost; `ldp` is the first sink built, when its trigger fires; `byo` keeps the door open for an existing SIEM. |

## Decision
Option C.

### Signals and where they go
| Signal | Source | Destination | Who acts |
| --- | --- | --- | --- |
| Cost | Project budget alert | E-mail to the one address the alert takes (`modules/cloud-project/main.tf:48`): a shared mailbox or alias that reaches both the accountable recipient and the backup | Recipient, next working day |
| Run outcome | Run record: a `chain` writes `summary.json` and `leftovers.json`; `plan`, `apply` and `destroy` write `run.json` only | The run's PR or evidence (sanitised: run id, outcome, and for a chain its assertions and deviations) | Whoever started the run |
| CI health | Forge checks; recorded foundation runs | Forge notifications | Maintainer |
| Drift and conformance | `lz-audit` SARIF and JSON (ADR-0006) | Forge code-scanning tab; the sink when one exists | Maintainer, weekly triage |
| Scanner health: failed run, or last success older than its freshness bound (ADR-0020) | A failed run: the scanner job. A stale one: a separate scheduled watchdog that checks the last success, since a job that never starts cannot report itself | A forge issue, on every sink including `none`; no e-mail, because no SMTP credentials exist | Maintainer, next working day |
| Account audit, activity, access-policy logs | OVHcloud log forwarding, set up by `account-fabric` | The account's audit sink: `ldp` with one-year retention (owner decision, below), or `byo` for an existing SIEM; required before break-glass or planned elevation (below) | Security recipient |
| Break-glass use and planned elevation | Audit log entry | A sink alert to the security recipient (ADR-0006, ADR-0018) | Security recipient, same day |
| Workload logs and metrics | The tenant's runtime | The tenant's choice; the landing zone only provides the sink | Tenant |

**An audit sink before elevation.** Break-glass and planned elevation stay unavailable until an
operational account audit sink exists: forwarding in place and its alert seen to fire. This holds
when the observability value is `none` too, because ADR-0006 and ADR-0018 require an alert for
both routes and on `none` nothing would deliver it.

**Who owns which sink.** Account-level audit forwarding and its stream belong to the
`account-fabric` stage, because the account needs them before any project exists (ADR-0004). The
`observability` stage holds the per-environment sink that runtimes and tenants write to.

Every alert names **one accountable recipient** and a backup (the backup maintainer of ADR-0022).
Recipients live in private configuration, never in the repository. A missing recipient makes the
installation's observability status `blocked`, as ADR-0020 already requires for the scanner.

### On-call model for a small organisation
No paging and no out-of-hours rota. Three severities:
- **Sev 1, best effort, next working day:** a credential that may have leaked, break-glass use
  nobody announced, state lost or corrupted, an unexplained resource in an account. Whoever sees
  a possibly leaked credential revokes it at once; the rest of the response follows the
  commitment.
- **Sev 2, next working day:** a failed apply or destroy, a leftover after a run, a budget
  threshold crossed.
- **Sev 3, weekly triage:** drift findings, stale docs, a red CI check on a dependency update.

An organisation that needs faster response adds paging through its own sink (`byo`); the project
does not promise it (ADR-0022).

### Incident runbook
One page, [Respond to an incident](../how-to/respond-to-an-incident.md), written after acceptance. It
covers five cases, each with first steps, who decides, and the `task` target where one exists:
a leaked or suspected credential, lost or corrupt state, a failed run with leftovers, a cost
spike, and break-glass use. It builds on these existing procedures:
[read a run record](../how-to/run-the-first-slice.md#read-a-run-record),
[clean up and check for leftovers](../how-to/run-the-first-slice.md#clean-up-and-check-for-leftovers),
[reporting a vulnerability](../../.github/SECURITY.md), and stale-lock recovery in
[ADR-0009](0009-state-backend-bootstrap-and-credentials.md).
Each incident ends with a short note: what happened, what was lost, what changes. A recurring
cause becomes a known-issue record (`docs/known-issues/`).

### What the first slice already does
- Budget alert: an optional `ovh_cloud_project_alerting` in `modules/cloud-project/main.tf:41-50`,
  passed through `components/project-factory/main.tf:35`; switched off in the sandbox manifest
  (`stacks/deployments.yaml:36`). Spec 007 adds a disposable probe root for it
  (`tests/live/probes/budget-alert/`); its live run is still pending, and its recipient is the
  owner's, not committed ([spec 007](../../specs/007-cleanup-first-live-slice/spec.md)). The raw
  resource was created and destroyed once on the trial project (spec 005 T010). ADR-0006's plan
  policy that refuses a project without a budget alert is not built yet.
- Run records: every live run writes a gitignored record, and what it holds depends on the verb. A
  `chain` writes a pass, fail or blocked outcome per assertion and the leftover report; `plan`,
  `apply` and `destroy` write `run.json` and no summary or leftover report, and only `apply` adds
  the inventory ([runbook](../how-to/run-the-first-slice.md#read-a-run-record));
  every child stream passes through the redactor (`tools/internal/live/runner.go:40-43`,
  `tools/internal/live/redact.go:14-15`).
- CI: one workflow, its runs recorded in `pipelines/github/foundation-runs.json`.
- Not done: no `observability` stack, no log forwarding, no scanner, no recipients beyond the
  probe's, no incident runbook ([verification status](../reference/verification-status.md#not-in-this-slice)).

### Postponed, with triggers
| Part | Trigger |
| --- | --- |
| Account audit-log forwarding and its stream alerts, in `account-fabric` (inventory B10) | A break-glass user, planned elevation, or a second human with console access |
| `observability` stack with the `ldp` sink for environments (inventory B10) | A consumer that needs logs |
| `byo` sink | A user with an existing SIEM asks for it |
| CI failure and live-lane alerts for pipeline-run lanes (inventory B8) | Protected-branch CI holding sandbox credentials |
| `lz-audit` scanner, its SARIF and the stale-scanner watchdog | The first reference environment (spec 005 postponed table) |
| Budget alert on by default in every project, and ADR-0006's plan policy that refuses a project without one | A project outside the sandbox, or the second slice's platform-owned `project` stack |
| Incident runbook page | Owner acceptance of this ADR, or the first incident, whichever comes first |
| Metrics | The first runtime with compute |

### Owner decisions (2026-10-20)
- The account audit sink is LDP, with one-year retention of the audit stream. It is built when its
  trigger fires (postponed table), as a billed opt-in; the free tier and `solo` do not run it by
  default.
- Sev 1 is best effort, next working day; there is no out-of-hours cover.

Still open: the recipients (accountable, backup and security, kept in private configuration) and
the retention of run records.

## Consequences
- The sandbox and `solo` cost nothing extra to observe, and see account audit events only when
  they choose a sink; until then they have no break-glass or planned elevation. The honesty page
  states that gap.
- Every promised alert now has a destination and an owner, or a visible `blocked` status.
- Running on e-mail and forge notifications means signals are only as good as the inbox behind
  them; the named backup recipient is the mitigation.

## Counterpoints (kept even if overruled)
- LDP from day one would answer ADR-0018's audit-source spike sooner. It is deferred because it is
  billed and nothing consumes it yet; the spike can still run on its own.
- A leaked credential handled next working day leaves an attacker most of a day. The immediate
  revocation step above narrows that; a faster commitment needs paging through `byo`.
- One-year LDP retention is a recurring cost after the trial credit, and no audit requirement
  demands it yet. Its price is UNVERIFIED; the spike below records it.
- Three severities may be too coarse for an organisation with many tenants. They fit one
  maintainer; a larger organisation brings its own on-call through `byo`.

## Verification (how we will know / spikes)
- Spec 007's budget-alert probe: create, read back and remove an alert on the sandbox project.
- Spike (ADR-0018): forward audit logs to an LDP stream and see a test login arrive; record cost.
- L10 drill (ADR-0008): a break-glass login produces an alert at the security recipient.
- Offline control: an installation without a recipient reports observability `blocked`.
- Offline control: the watchdog opens a forge issue when the scanner's last success is older than
  its bound, including when the scanner has never run.

## Review log
- 2026-10-20: drafted; one independent review round, findings applied.
- 2026-10-20: accepted (D128) after two further independent reviews; their changes applied. The
  owner decisions above stay open.
- 2026-10-20: the owner decided the paid sink (LDP, one-year retention, opt-in) and the Sev-1
  commitment (best effort, next working day).
