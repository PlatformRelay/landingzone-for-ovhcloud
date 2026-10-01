# Focused adversarial delta review

Date: 2026-10-01 · Target: revisions addressing C1, C2 and W1 from
[the original review](plan-adversarial.md), plus the planned latency check.
The original report is unchanged. This is a draft-contract review, not implementation qualification.

## Verdict: CLEAN

The three original counterexamples can no longer satisfy the stated acceptance contracts.
No remaining CRITICAL or WARNING finding survived the focused attack/defend pass. The plans
can proceed to task generation, with implementation and live qualification still gated.

### Resolved findings

- **C1 — saved-plan encryption acceptance gap: resolved in the draft.**
  `specs/003-platform-feasibility/spec.md:54` now explicitly requires actual pinned-tool
  state and saved-plan outputs, correct-key consumption, missing/wrong-key and plaintext-input
  rejection, separate state/plan-enforcement mutations, and a private plaintext-canary scan.
  The original attack—encrypted state and escrow recovery accompanied by an unencrypted saved
  plan—fails the saved-plan and canary outcomes. Omitting plan enforcement while retaining state
  enforcement is now a separately named mutant, rather than hidden behind a recovery success.
  `specs/003-platform-feasibility/plan.md:55` and
  `specs/003-platform-feasibility/contracts/checks.md:18` carry the same controls into delivery
  and the command contract. The defence that private storage alone proves encryption is no
  longer needed or accepted. No encryption output or mutation was executed in this review.

- **C2 — floor-removal and tag-envelope coverage gap: resolved in the draft.**
  `specs/003-platform-feasibility/spec.md:41` requires floor-binding/policy self-removal,
  missing/present authorisation tags, cross-tenant retagging and project-tag effects on child
  operations for every applicable route. V006 at line 57 includes per-route positive/denial
  controls and forbids a spike-3 or envelope-authority pass when a subcontrol is absent.
  The original attack—observe a delete denial while leaving floor-shedding or retagging
  untested—therefore leaves qualification blocked. This is repeated in
  `specs/003-platform-feasibility/plan.md:61`,
  `specs/003-platform-feasibility/contracts/checks.md:14` and the per-route subcontrol state
  in `specs/003-platform-feasibility/data-model.md:11`. The alternative defence that forge
  authority checks cover direct cloud credential use is not relied upon. Existing independent
  experiment approval and disposable-scope gates still apply; the revision does not authorize
  a live self-removal, retagging, destructive or ordinary-account federation probe.

- **W1 — candidate-controlled isolation launch configuration: resolved in the draft.**
  `specs/001-offline-foundation/plan.md:16` now names a protected verification workflow at
  an approved base revision, maintainer dispatch with a candidate SHA, archive acquisition as
  data, rooted-path validation, and execution of candidate commands only after isolation.
  Candidate workflow/include/Taskfile content cannot choose mounts, image or launch arguments;
  tokens remain in the outer trusted job. V001 and V007 at
  `specs/001-offline-foundation/spec.md:46` and line 52 explicitly require malicious candidate
  mutation cases. `specs/001-offline-foundation/contracts/checks.md:8` also names socket,
  mount and preparation-path controls. A Taskfile inserting a connected-host call before
  Docker launch now violates both the execution boundary and the acceptance outcome; it
  cannot be defended by a successful in-container network test. A candidate attempting to
  publish its own substitute result is also excluded by the trusted-publisher requirement.
  Parent-run execution is distinguished from infrastructure owned by a fork.

### NOTE

- **The latency criterion now has an explicit planned command.**
  `specs/001-offline-foundation/contracts/checks.md:12` defines `task verify:latency`, and
  V001 maps it to SC-001. It measures the entire applicable naming suite after preparation,
  records discovery counts, and rejects missing layers or a run exceeding 120 seconds.
  Dropping a layer to meet the threshold cannot satisfy the revised contract. It remains
  planned and must receive its creating task; no runtime measurement was produced here.
- **Trusted result origin remains a qualification gate.**
  `specs/001-offline-foundation/plan.md:22` binds the candidate, base, launcher and image
  identities plus the expected publisher, and explicitly requires actual required-check origin
  enforcement to be qualified. Ordinary candidate CI stays advisory. I did not infer that
  either forge already enforces that distinction; an unsupported mechanism must remain blocked.

## Focused persona attempts

- **Saboteur:** replayed the plaintext-plan counterexample, removed plan enforcement while
  keeping encrypted state, and omitted floor-removal/retagging observations. The revised
  V003/V006 outcomes reject each. Tried satisfying the latency target with a shortened suite;
  discovery/layer completeness prevents that acceptance shortcut.
- **New Hire:** followed each original finding into the current requirement, acceptance row,
  plan and contract. The required work and blocking outcome are now explicit. Followed an
  unavailable cloud route or forge-origin mechanism to qualification; neither becomes a pass.
- **Security Auditor:** tried a candidate Taskfile host command, altered candidate workflow/include
  launch arguments, socket/credential mounting, and a candidate-produced substitute check.
  The revised contract puts all candidate commands behind a base-controlled launcher and keeps
  publication authority outside it. Replayed floor-shedding through direct cloud credentials;
  the route-specific tests are now required independently of repository authority checks.
- **Budget Holder:** checked whether the fixes silently require production guardrail rollout,
  general forge infrastructure or a larger cloud slice. They add bounded qualification cases,
  retain independent experiment approval and scope exclusions, and leave unavailable mechanisms
  blocked. No additional product/service scope was introduced by these fixes.

## What I could not check

No implementation or acceptance target exists, so no state/plan file, encryption mutation,
container execution, candidate archive extraction, socket/mount policy, malicious workflow,
latency run or trusted-result enforcement was executed. No pinned-tool fixture was captured.
No real forge repository/protection/token or cloud account, credential, floor binding, tag,
child operation, price, resource or recovery experiment was accessed. Original tool/system
observations and primary-source limitations remain unchanged; this delta did not repeat the
broad review. Passing this draft review does not activate a spec, qualify a platform control,
grant a credential or authorize any live write, purchase, deletion or outward message.

The only file written for this delta review is this report.
