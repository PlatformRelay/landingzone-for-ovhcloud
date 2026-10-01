# Independent adversarial review of the initial phase plans

Date: 2026-10-01 · Baseline: 116405b98960dcc78b3843985a4a49f35afae08a
Target: constitution, specs README, and the spec/plan/research/data-model/contracts/quickstart
artefacts for 001, 002 and 003. These are draft planning contracts; no implementation exists.
No author explanation or previous review output was used.

## Verdict: BLOCK

Revise the acceptance contracts before generating implementation tasks. This verdict concerns
what the drafts currently require a future check to observe; it is not a claim that an existing
implementation failed or that an unrun cloud experiment is already disproved. Two coverage
omissions below were independently exposed by the Saboteur and Security Auditor perspectives;
the skill's convergence rule raises them from WARNING to CRITICAL.

### CRITICAL

- **C1: V003 can pass without proving saved-plan encryption.** —
  `specs/003-platform-feasibility/spec.md:39`, `specs/003-platform-feasibility/spec.md:54`
  [Saboteur, Security Auditor]
  Failure: configure native state encryption and an independently encrypted escrow backup,
  but omit the plan-encryption configuration. Restore the fixture on a fresh runner, reject
  the wrong escrow key, reject the same-key replica as key-loss recovery, and reconcile empty
  bootstrap state. Every stated V003 outcome can succeed while `tofu plan -out=...` writes a
  plaintext saved plan containing the sensitive fixture value. The draft then maps FR-004's
  encrypted saved-plan handling clause to a recovery result that never observed it.
  Defence considered: 002's plan mentions synthetic encrypted plans, and 003 keeps raw
  plans private. Neither is an acceptance control for actual pinned-tool plan encryption;
  private storage and state recovery do not demonstrate encryption of saved plans. The
  general requirement to test every clause still needs its predefined observable here.
  Fix: add explicit V003 subcases, or a separate mapped check, for actual pinned OpenTofu
  state and saved-plan outputs. Require a correct-key control, missing/wrong-key rejection,
  enforced rejection of plaintext input, and separate mutations removing state and plan
  enforcement. Exercise the intended saved-plan consumer under the valid key, and inspect
  the persisted artefacts for the sensitive canary without publishing the canary or raw
  artefacts. Keep method/key identifiers and format provenance in private evidence.
  This is consistent with the separate state and plan configuration in the
  [OpenTofu encryption documentation](https://opentofu.org/docs/language/state/encryption/).

- **C2: Spike 3 can be called qualified without proving that tenants cannot shed the floor
  or escape a tag-conditioned envelope.** —
  `specs/003-platform-feasibility/spec.md:6`, `specs/003-platform-feasibility/spec.md:41`,
  `specs/003-platform-feasibility/spec.md:57`,
  `specs/003-platform-feasibility/contracts/checks.md:19`
  [Saboteur, Security Auditor]
  Failure: the currently bound canary principal receives an attributable project-delete
  denial and the same-route positive control succeeds. The principal can nevertheless
  remove/change its own floor binding, rewrite an applicable policy, or modify an
  authorisation tag to reach another envelope. V006's stated outcomes do not exercise any
  of those operations, yet V008 can record ranked spike 3 as passed. ADR-0006 explicitly
  requires floor-shedding resistance plus missing-tag, tag-present and tenant tag-mutation
  probes (`docs/adr/0006-guardrails-without-org-level-policy.md:84`); its decision also calls
  out whether project tags govern child operations (line 33).
  Defence considered: 001 rejects tenant-authored authorisation keys and 002 rejects
  self-authorising repository deltas. Those protect the data/forge lane, not an already
  issued credential making direct cloud calls. 003's route exclusions and experimental
  catalogue prevent unsupported promotion only when the missing subcontrol is recorded.
  Fix: carry these ADR-0006 cases into FR-006/V006 and the per-route qualification record:
  attempt floor-binding/policy self-removal with the canary credential; probe missing and
  present authorisation tags, cross-tenant retagging, and applicable child operations.
  Use positive controls and attributable denial reasons for each. Each live mutation
  remains a separately reviewed disposable experiment. If this phase deliberately
  excludes an envelope case, explicitly mark that subcontrol blocked and prohibit a full
  spike-3 or dependent envelope-authority pass; do not expand it into a production rollout.

### WARNING

- **W1: The isolation launcher has no stated trust owner outside candidate-controlled code.** —
  `specs/001-offline-foundation/plan.md:13`,
  `specs/001-offline-foundation/spec.md:37`,
  `specs/001-offline-foundation/spec.md:52`,
  `specs/001-offline-foundation/contracts/checks.md:6`
  [Security Auditor]
  Failure: implement the stated Task interface with a host workflow checking out the
  candidate and running its `task check`. A fork changes `Taskfile.yml` so its first command
  attempts an outbound request on the runner, then launches the usual network-disabled
  image. The in-container process/subprocess boundary test still observes no network.
  The forbidden call occurred before that boundary. An altered workflow/include can
  similarly change mounts or image arguments before the container starts. The plan
  qualifies the container and says the fork has no privilege, but does not specify who
  controls the launch configuration or require this candidate-mutation case.
  Defence considered: independently protected sensor review governs merging such edits,
  and cloud secrets must be absent. That does not stop candidate code executing during
  its pre-merge check, and a credential-free preparation step can still have networking.
  This is an underspecified planning boundary, not an observed exploit of a real workflow.
  Fix: name a trusted, immutable launcher/runner configuration outside the candidate's
  control. It must enter the prepared isolated environment before executing candidate
  Taskfiles, scripts, generators, providers or hooks; give candidate code no runtime
  socket, privileged mount, preparation credential or protected cache-write capability.
  Retain the Task contract inside that boundary. Extend V001/V007 with a malicious
  Taskfile and workflow/include mutation and assert that no candidate instruction runs
  on the connected host. Define the qualification scope for GitLab fork pipelines
  separately from fork-owned infrastructure: the parent can control only its own runner
  execution. Docker's [none driver](https://docs.docker.com/engine/network/drivers/none/)
  isolates the container network stack, while GitLab's
  [fork-pipeline documentation](https://docs.gitlab.com/ci/pipelines/merge_request_pipelines/)
  confirms that a parent-triggered fork pipeline uses configuration from the fork branch.

### NOTE

- **The deferred mechanisms are honest gates, not failed experiments.** —
  `specs/002-transaction-rehearsal/spec.md:70`,
  `specs/003-platform-feasibility/spec.md:76`
  Missing assent adapters, disposable repositories, cloud credentials, scoped projects,
  escrow holders, numerical windows or account-specific observations must leave the
  corresponding check blocked. No finding requires those experiments to have already
  passed during this planning review.

## Load-bearing premises checked before the persona review

The premise most capable of invalidating the first two phases is that the proposed pinned
tools exist and provide the required test/orchestration surfaces. I attempted that check
first, using the installed commands and primary sources rather than treating the draft's
version table as evidence.

| Premise | Actual check and result | Qualification limit |
| --- | --- | --- |
| Proposed OpenTofu 1.13.0 and Terramate 0.17.3 can be pinned | Opened the upstream [1.13.0 release](https://github.com/opentofu/opentofu/releases/tag/v1.13.0) and [0.17.3 release](https://github.com/terramate-io/terramate/releases/tag/v0.17.3); both exist. Ran `tofu version` → 1.10.3, `terramate version` → 0.17.1, `task --version` → 3.53.1. | Target binaries were not installed or executed. No target-version JSON fixture or exact semantic qualification was produced. |
| Terramate selection and order are separate mechanisms | Ran installed `terramate run --help`; opened upstream [stack configuration](https://terramate.io/docs/cli/stacks/configuration). The docs describe forced selection through wants and separate order through after/before. | Installed help is 0.17.1; no 0.17.3 changed-set/order fixture was run. The draft correctly retains that qualification gate. |
| A fallback method is not independent recovery of another key's ciphertext | Read [OpenTofu encryption](https://opentofu.org/docs/language/state/encryption/). Fallback tries the old configured reader; key loss still requires the correct key or a separately decryptable copy. | This supports the draft's mechanism distinction, not any account's actual OKMS helper or escrow restore. |
| Native S3 locking has a relevant mechanism to test | Read the upstream [S3 backend](https://opentofu.org/docs/language/settings/backends/s3/) description of conditional-write locking. | No OVH conditional write, permissions, interrupted lock, replica or old-writer denial was observed. Those remain 003 experiments. |
| The forge engine can be reused rather than recreated | Opened [assent's public repository](https://github.com/PlatformRelay/assent); its README describes alpha status and the GitLab CI path. `command -v assent` did not find an installed binary. | No exact assent release/commit or adapter was executed. Its public README is not GitHub-adapter qualification. |
| The planned offline host has candidate-independent isolation | Found Docker on PATH; checked the Docker and forge primary sources above. | Did not run Docker, inspect daemon settings, mount credentials, alter a workflow, or make an outbound cloud probe. This premise is underdefined as W1, not verified. |

The provider [2.21.0 release](https://github.com/ovh/terraform-provider-ovh/releases/tag/v2.21.0)
also exists. No provider schema was captured, and no naming/tag limit is promoted here.

## Concrete persona attacks and disposition

- **The Saboteur:** walked two publishers finishing out of order, a producer update immediately
  after a consumer fence, failed publication with an old current pointer, driver death during
  apply, lock-owner loss, simultaneous reservations, and a reservation expiring mid-apply
  through 002 spec/plan/contracts/data model. The shared sorted locks, pending status before
  mutation, immutable publication/CAS, downstream freeze and reservation hold through actual
  reconciliation defeat these attacks at the documented contract level. No implementation
  test was run. Tried the plaintext-plan counterexample and floor-shedding sequence above;
  those expose C1/C2. Tried a no-id create response after process death in 003; the external
  request/inventory reconciliation and no-retry gate explicitly retain the ambiguity, so
  I did not mistake a future feasibility deliverable for an observed platform guarantee.
- **The New Hire:** reconstructed phase dependencies using the required check ids and the
  plan's creating surfaces, checked the future commands against their planned/not-run labels,
  and followed absent tools, absent adapters, unknown kinds and missing numerical RunConfig
  values through the stop conditions. These are discoverable blocked states. Checked whether
  the local transaction rehearsal could silently become production qualification; 002
  explicitly requires future remote-store qualification against the same race suite. The
  saved-plan and spike-3 acceptance gaps remain concrete onboarding hazards, but their
  root defects are already C1/C2 rather than duplicate findings.
- **The Security Auditor:** attempted request-authored owners/policy/defaults, forged approval,
  stale base/candidate/schema/toolchain/artefact bindings, foreign instance paths, fork cache
  writes, raw secret evidence, and expired-token/outage-as-denial substitutions against the
  named requirements and check rows. Trusted-base effective-document evaluation, rooted
  path/symlink rejection, immutable candidates, protected tokens and same-route positive
  controls are explicit. Checked the launcher before the container and native plan encryption
  separately from state encryption; W1/C1 survive. Followed the route denial after legitimate
  credentials mutate cloud authority; repository authority checks do not close C2.
- **The Budget Holder:** challenged building a generic generator, second forge engine,
  production database, TACO matrix, provider replay service, token broker and full runtime
  before a consumer. The plans reject or defer them, keep two real examples as the generator
  trigger, and describe local models as rehearsals. Reconstructed monthly exposure admission
  with recurring/storage/replica/preparation costs and failed cleanup; unknown/stale costs and
  unhealthy reaper block new admission while cleanup retains authority. Checked whether a
  floor or recovery failure could be declared a supported tuple to justify further spend;
  explicit blocked/refuted downstream gates are present, subject to fixing C1/C2's omitted
  observations. No independent extra budget finding survived the defence.

## Sequencing and irreversible-action walk

No deployment objects, workflow implementations or live mutations exist to derive an actual
deployment dependency graph from. The documented experiment graph was independently walked:

1. Offline harness → offline sandbox ledger/reaper tests and approved cost/sandbox decision
   → external setup → read-only preflight. Missing approval or numerical bounds blocks.
2. Disposable fixture and allowlisted project → healthy cleanup lane plus approved lease
   → cleanup canary → injected failure drill and inventory reconciliation. Failed health
   blocks subsequent admission, while cleanup remains executable.
3. Disposable state/key/backup → sandbox exit and independent escrow package → clean-runner
   recovery, isolation and locking. An old writer must be fenced before replica activation.
4. Canary identity/policy → already drilled native recovery and separately reviewed mutation
   → route-denial probes. Federation requires disposable account scope; ordinary-account
   singleton replacement is forbidden. Destructive actions without explicit experiment
   authority remain blocked.
5. Issued credential → approved residual window and issuer-specific route → expiry/refresh,
   offboarding and cleanup probes → aggregate qualification, additionally gated on 002.

No cycle was found in that planned sequence. I did not infer ordering proof from a missing
implementation. The first vertical slice, actual source/store closure, native writer fencing,
forge required-check enforcement and any irreversible probe still require their declared
evidence and approval gates.

## What I could not check

- No acceptance target exists. I ran no `task test:*`, mutations, naming module, report parser,
  reconciler, driver, reservations implementation or sensor implementation. Counterexamples
  above are analysis of the draft acceptance contract, not measured mutation survivors.
- No OpenTofu 1.13.0, Terramate 0.17.3 or assent pin was executed. Actual target-version output
  shape and fixture provenance remain unverified; no handwritten fixture substituted for them.
- No Docker execution boundary, daemon socket/mount policy, prepared image digest, cache
  permissions or real runner credential environment was inspected or exercised.
- No disposable GitHub/GitLab repository, token, branch protection, merge race or required-check
  race was used. Real forge qualification remains blocked until its setup exists.
- No account/project inventory, price/billing record, API call, schema capture, naming constraint,
  cloud state/artifact/decrypt isolation, lock, replication, issuer, federation, reaper, key-loss
  or credential-revocation experiment was run. Primary-source descriptions do not prove this
  account. No cloud writes, orders, purchases, destructive probes or outbound messages occurred.
- Broader runtime/network/resilience/release claims and later AgentEx generators/drills were
  reviewed only for explicit deferral, not qualified. Draft ADR statuses remain Proposed.

The only file written by this review is this report.
