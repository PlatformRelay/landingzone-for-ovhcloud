# Feature Specification: Harden and tidy the first slice
Feature: `006-harden-first-slice` · Created: 2026-10-15 · Status: draft
ADRs: 0002, 0007, 0008, 0009, 0010, 0011, 0013, 0019, 0021, 0022, 0024
Input: the owner's direction to plan the unspecified work so that agents can work unblocked; the
unspecified-work inventory of 2026-10-15 (themes A, E and the offline part of C, priority 1–2);
decisions D87–D114; constitution 1.3.0.

## Why and scope
Spec 005 delivered a working first slice and left a trail of recorded gaps in its evidence files.
This spec closes the ones that matter for safety and for what an operator sees. It also tidies the
documents that no longer match the code. Every task runs offline, with no cloud credential, no
OVHcloud API call and no owner session. Agents can run it end to end while the owner is away.

Visible outcome: a live verb never leaves a secret in an error or a run record, and never runs
without a deadline or an inventory. The offline entry bounds memory and process count. Dependency
bumps, releases and the runtime image get an offline-checked lane. The status pages say what
was observed.

It is a hardening pass, not a new slice. It adds no stage, stack or cloud resource, and it does not
fire the AgentEx trigger of constitution VI ("the second vertical slice").

**In** (inventory ids in brackets):
- live-lane hardening, priority 1–2: bounded plan, apply and destroy outside the chain [A1, A22];
  destroy-on-exit with its own budget [A21]; no secret in binding errors [A2] or in leftover
  records [A3]; credential files written as one set [A4]; companion variables by allow-list [A5];
  scratch `HOME` pinned on every child path [A20]; one retry budget per leftover check [A9]; KD-1
  canary removed after an error answer [A11, first part]; Object Lock observed on deployed state
  buckets [A12];
- generation and harness: stage locks copied and agreeing, unreconciled rows refused before
  generation [A7]; registry inputs complete for Go checks [A8];
- offline entry and runtime: memory and process limits [C1, KI-002]; runtime preparation in tested
  Go instead of untested Python [A25];
- supply chain, offline parts: a dependency-update lane configuration with an offline check and a
  provider schema snapshot [C5]; a release-train manifest and changelog renderer, without a tag
  [C4]; an unsigned provenance statement for the runtime image [C6];
- docs and spec drift: `verification-status.md` after T049 run 4 [E1]; KI-001 and KI-002 [E2];
  open spec-005 wording fixes [E3]; two offline ADR-0013 anti-rot gates [E4]; a repository-map
  drift check [E6];
- tracing spec 006 by default and in CI's source check.

**Out**: everything that needs the owner, a live run, the network or a new tool. See
*Out of scope / postponed (with trigger)*.

## Clarifications

### Session 2026-10-15
Answered from the inventory, the code at 234a88d and the decision log. The three owner decisions
were decided by the owner on 2026-10-15 (D115).
- Q: Should companion variables move from the deny-list to an allow-list (inventory Q4)? → A:
  Decided (D115, 2026-10-15): the allow-list of FR-006.
- Q: Which bot reads the dependency-update configuration (inventory Q5)? → A: Decided (D115,
  2026-10-15): Renovate, in `.github/renovate.json`; installing the app stays the owner's later step
  (C5).
- Q: Which release tooling and provenance (inventory Q7, C6)? → A: Decided (D115, 2026-10-15): the
  in-repo renderer and an unsigned in-toto statement; signing is decided at the first tag (C6).
- Q: Is A21 (chain destroy budget equal to the run deadline) already closed? → A: No. The destroy
  context is already detached from the run's cancellation (`WithTimeout(WithoutCancel(ctx),
  deadline)`, `tools/internal/live/chain.go:285-287`, `runner.go:921-923`), but its budget is the
  run's `--deadline` (`chain.go:226-228`, `runner.go:830-832`), so a run deadline shorter than
  `DefaultDeadline` gives a destroy budget shorter than `DefaultDeadline`. In scope as FR-002.
- Q: Does A26 (planted `stacks/Taskfile.yml` in the boundary suite) need work? → A: No. Closed by
  06a8c21 (`TestTrustedTargetsIgnoreCandidate`, `tools/internal/probes/security/offline_test.go:256`).
- Q: Is A6 (JSON-escaped secrets slip past redaction) in scope? → A: No. Its evidence marks it
  "Accepted, latent" (`specs/005-first-landing-zone-slice/evidence/T055.md:97`), and the inventory
  refuses accepted items. It is postponed with a trigger.
- Q: Should the repository map be generated (ADR-0002, ADR-0013) or drift-checked? → A:
  Drift-checked. Constitution VI lets generators follow two real examples; one hand-written map is
  not two. The generator is postponed.
- Q: Does spec 006 fire the AgentEx trigger? → A: No. It adds no capability slice; spec 007 is the
  candidate second slice.

## Decisions and proposals
- **Approved direction**: the owner's "plan the unspecified work so agents can work unblocked";
  inventory recommendation "spec 006 = A + E + offline C, capped at priority 1–2"; D89 (agents land
  their work); D104 (Object Lock on state buckets); D111 (no review-tool names in committed files);
  D112 (T045 deferred); D114 (entry r4-e2, 240 s child limit).
- **Decided by the owner (D115, 2026-10-15)**, formerly the three open choices of this spec:
  1. Companion variables: the allow-list of FR-006, not the present deny-list (inventory Q4).
  2. Dependency-update bot: Renovate, in `.github/renovate.json` (inventory Q5); installing the app
     is the owner's later step (C5).
  3. Release tooling and provenance: the in-repo renderer and an unsigned in-toto statement
     (inventory Q7); signing is decided at the first tag (C6).
- **Proposals, credible alternatives** (detail in `research.md`):
  - entry limits by a transient cgroup with `MemoryMax`/`TasksMax`, rejecting `setrlimit`, which
    cannot bound the total across processes (R5);
  - `prepare.py` ported to Go in `tools/internal/bundle`, rejecting a host-side Python target,
    because ADR-0002 rejects Python and the pins would stay duplicated (R6);
  - repository map drift-checked, not generated (R10).

## User Scenarios & Testing

### User Story 1 — Secrets stay out of live-lane errors and records (Priority: P1)
An operator runs `lz-live` against a hostile or broken API. Given an endpoint that answers a redirect
with an unparseable `Location` header quoting the client secret (Go's HTTP client quotes that header
in its error), or a transport error naming such a value, the error on stderr does not contain the
secret. Given
a listing that echoes the leftover check's bearer token as a resource id, no file of the run record
contains the token. Given a write failure after `account-governance` applies, the deployer
credential files are either all the new set or all the previous set, never a mix. The next safe
action printed is to re-run the same verb.
**Independent test**: V001.

### User Story 2 — Every live verb is bounded, inventoried and cleans up in time (Priority: P1)
An operator runs `task live:apply -- <instance>` outside a chain. The run has a deadline, appends
each created id to an inventory, and prints a summary line with the run id and the cost reminder,
as the chain already does. `live:destroy` prints the same summary. A chain started with a short
`--deadline` still gives destroy-on-exit at least the default budget. A leftover check waiting on several retained instances
shares one retry budget and keeps time for the read and the listings after the last wait. A KD-1
canary bucket created by a call that answered with an error is still removed. The chain asserts
that each deployed state bucket carries Object Lock.
**Independent test**: V002. Interruption: SIGINT during `live:apply` destroys nothing retained,
leaves the inventory on disk and prints the run id for the leftover check.

### User Story 3 — The offline entry bounds memory and processes (Priority: P2)
A contributor runs any target through `lz-offline`. A candidate that allocates beyond the memory
limit or forks beyond the task limit is stopped and the entry reports `RESOURCE_LIMIT` with the
limit that tripped. A host where the limits cannot be applied is refused before the child starts,
with the reason and the next action (enable user cgroup delegation). The valid suite still passes
under the limits. The runtime bundle is prepared by tested Go code, so the pins live in one place.
**Independent test**: V005, V006.

### User Story 4 — Checks re-run when what they read changes (Priority: P2)
A contributor changes a Go package that a check imports, or a stage lock. The registry digest of
every check that reads the package changes, and `stacks:generate` refuses a manifest with
unreconciled rows instead of writing half a tree. New stacks get the stage's lock, and two stages
with different provider locks are reported.
**Independent test**: V003, V004.

### User Story 5 — Dependencies, releases and provenance have an offline lane (Priority: P3)
The maintainer opens the update-lane configuration and sees every pinned dependency covered, with
the runtime-mirrored tools grouped and marked as needing an entry revision. The maintainer renders
a release-train manifest for a commit and gets the same bytes twice, with the changelog grouped by
commit type. `lz-pack` writes a provenance statement next to the image it packs. No tag is created.
**Independent test**: V006, V007, V008.

### User Story 6 — Docs and spec text say what is true (Priority: P3)
A reader of `verification-status.md` sees run 4's observations, not "UNVERIFIED until that run". A
page that names a tool version is compared with `mise.toml`. A page that cites a superseded ADR
fails. Every path the repository map names under the entry's admitted roots exists, and every
admitted top-level directory is in the map.
**Independent test**: V009, V010.

### Edge cases
An unparseable `Location` header and a transport error carrying a secret; a token that appears inside a longer id (redacted there too);
a token split across two writes of a stream (the redactor's hold-back covers it); a crash while a
new credential generation is written, after it is complete, or just before the pointer switch (the
previous set stays the one readers see; the incomplete generation is removed on the
next run); an allow-list name that is also a reserved run-set name; a deadline of zero, negative or
larger than the run's limit; SIGINT or SIGTERM during an apply outside the chain; a destroy-on-exit
that itself times out (reported `fail`, inventory kept); a retained-state read that waits for the
whole budget; a canary create answering 5xx after the bucket exists, and one answering 409 because
it already existed (the existing bucket is not adopted); a bucket answering `ObjectLockConfiguration`
missing or disabled; a stage without a lock file; a Go check whose package imports nothing outside
the standard library; a host with cgroup v1 only, with no user manager, or with delegation that
omits the `pids` controller; a target that legitimately peaks near the limit; a `prepare` input with
a symlink or hard link escaping the root; a commit subject that is not `:gitmoji: type(scope):`; a
release version reused; a page with `verified_against` but no `mise.toml` entry for the tool; an
ADR without a status line.

## Premises

| ID | Premise | Source | Probe | Status |
| --- | --- | --- | --- | --- |
| P1 | `go list -deps` on the tools module resolves in-module packages inside `lz-offline` with the vendored tree and no network | `tools/vendor/modules.txt` exists; Go toolchain behaviour with `-mod=vendor` | T003: run through the entry, record the command, its output for `./internal/live` and the exit code | UNVERIFIED; fallback: a committed dependency list per check, refreshed by a host-side test |
| P2 | On the maintainer host, `systemd-run --user --scope -p MemoryMax=… -p TasksMax=…` places bwrap and its children in a cgroup v2 scope that enforces both limits; bwrap's `--die-with-parent` still ends the tree | KI-002 fix sketch (`docs/known-issues/KI-002-offline-entry-memory-process-limits.md:57-62`); no observation | T029: host-tagged test runs an allocating and a forking child under the scope and reads `memory.events` and `pids.events` | UNVERIFIED; fallback: a delegated cgroup created by the entry directly under the user's delegated subtree |
| P3 | The same works on the GitHub-hosted `ubuntu-24.04` runner | none | a CI run of the revised entry after publication (out of scope, coordinator) | Observed for the scope (2026-10-19, `evidence/T043.md` rows on runs 39249394208 and 39252705025): the hosted-runner probe run 39249394208 passed `TestHostLimits` in a delegated scope, but its entry steps were refused `PIN_PACKAGE` (the bundle then published was r4-e2), so it observed the scope, not the entry; r5 was then pinned in CI (PR 135), and run 39252705025 on PR 135's head was the first green foundation run with the limits |
| P4 | The peak memory and task count of every CI target fit well under the chosen limits | none | T032: per-target `memory.peak` and `pids.peak` from the scope, through the entry, recorded in the evidence | UNVERIFIED; limits are set at twice the observed peak (R5) |
| P5 | OVHcloud S3 answers `GetObjectLockConfiguration` on a governance-locked state bucket in the AWS response shape | D104; T099 set the lock through the S3 API (`specs/005-first-landing-zone-slice/evidence/T099.md`); read path not observed | offline: fake S3 in T021/T026; live: the next owner chain run (out of scope) | UNVERIFIED live; offline controls only |
| P6 | Renovate's `mise`, `gomod` and `terraform` managers cover `mise.toml`, `tools/go.mod` and the provider constraints and lock files | none read (research R8) | none offline: the check of T035/T037 judges only the repository's own rules and never manager names; observed when the owner installs the app (out of scope) | UNVERIFIED (believed, inventory Q5); no task depends on it |
| P7 | `tofu providers schema -json` runs offline inside `lz-offline` against the filesystem mirror of `ovh/ovh` 2.21.0 | entry mirrors the provider (`tools/cmd/lz-offline/prepare.py`); not run | T035 capture through the entry, command and version recorded | UNVERIFIED; fallback: capture on the host with the same pinned binaries and mirror |

No premise here is about OVHcloud behaviour that a task relies on to close. P5 is the only cloud
premise; its offline part is a fake, and its live observation is postponed with a trigger.

## Requirements

### Functional requirements
- **FR-001 Bounded verbs** — `lz-live plan`, `apply` and `destroy` run with a deadline (the
  chain's default unless given) and `apply` appends every created resource id to a run inventory
  outside state, as the chain does. Each of the three verbs prints the summary line (run id, verb,
  outcome; for `apply` also the inventory path) and, for `apply` and `destroy`, the cost reminder. When the deadline
  fires during an `apply`, the verb stops, keeps its inventory and exits non-zero; it never destroys
  a retained instance. [A1, A22]
- **FR-002 Destroy budget** — destroy-on-exit (chain and probe core) gets a budget of at least
  `DefaultDeadline`, whatever `--deadline` the run was given: a run deadline shorter than
  `DefaultDeadline` no longer shortens the destroy. The destroy stays detached from the run's
  cancellation, as today. A destroy that exhausts its budget reports `fail` and keeps the inventory
  and state for `--cleanup`. [A21]
- **FR-003 Quiet binding errors** — an error from the account-binding client (`GET /auth/details`
  and its token request) never quotes response headers or bodies, URLs from redirects, or any
  credential value; it names the call, the status class and the next action. [A2]
- **FR-004 Leftover token redaction** — the bearer token the leftover check obtains is registered
  with the run's redactor before the first listing, so no file of the run record (listings,
  `leftovers.json`, logs) and no stream contains it, including where the token sits inside a longer
  id or is split across two writes. An empty token is never registered. [A3]
- **FR-005 Credential set** — the credential files written after an `account-governance` apply,
  and those written after each `tenant-state` apply, form one set per writing instance. A new set is
  written into a fresh generation directory and becomes current through one atomic switch of a
  pointer file; until that switch every reader sees the previous set whole, and after it the new set
  whole. A crash at any step leaves readers on one complete set; an incomplete generation is removed
  by the next writer. The account binding (`account.env`), the passphrase, `sandbox.env` and the
  bootstrap's `state.env` do not move. Mode 600 and the single-writer rule of 005 FR-010 hold. [A4]
- **FR-006 Companion allow-list** — a probe root's `companion_env` is admitted only for names on an
  allow-list, matched case-sensitively: `TF_VAR_` followed by `[A-Za-z0-9_]+`, except the run's own
  `TF_VAR_state_passphrase`, `TF_VAR_state_path`, `TF_VAR_run_id` and `TF_VAR_project_id` (refused
  in any case); the OVHcloud OAuth2 client pair `OVH_CLIENT_ID`/`OVH_CLIENT_SECRET`; and the S3 key
  pair `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`, each pair complete or absent. Any other name,
  including a lower- or mixed-case `tf_var_` prefix and names that only resemble an admitted one, is
  refused with the name in the error.
  Decided (D115, 2026-10-15): the allow-list replaces the deny-list (inventory Q4; T082 had
  recorded it as the coordinator's deferred decision). [A5]
- **FR-007 Scratch home** — no child process started by `lz-live` on any path (bootstrap phases,
  probe, plan, apply, destroy, chain) inherits the caller's `HOME`; the exported helper that still
  passes `os.Getenv("HOME")` (`tools/internal/live/env.go:26-29`) is removed or takes the home as an
  argument, and the bootstrap path has its own regression row. [A20]
- **FR-008 Retry budget** — one leftover check shares one retry budget across every retained
  instance it reads, and a wait is admitted only if the time for the read and the remaining
  listings is still left after it; otherwise the check reports `blocked` with the instance it was
  waiting for. [A9]
- **FR-009 Canary removal** — the KD-1 canary bucket is registered for removal before its create
  call, so a create that answers an error after the bucket exists still ends with the bucket
  removed or reported as a leftover; a canary name that already existed before the call is never
  adopted or removed. The name is read first, and only a name read as absent (404) is removed
  (`ObserveCanary` in `tools/internal/live/observe.go`, T025). Accepted limit: if another creator
  takes the name between that 404 read and a create that answers 5xx or drops, the trap removes
  their bucket, because T020's `TestObserveCanaryCreateError/500-after-create` requires removal
  after an ambiguous create on a name read as absent; the name carries the run id, so such a
  creator is not credible (coordinator decision 2026-10-16, loop.md triage row L T025;
  `evidence/T025.md` *Harness gaps* 1). [A11]
- **FR-010 Object Lock observed** — the chain's L7 assertions include `state-bucket-object-lock`:
  each deployed state bucket reports Object Lock enabled in governance mode with a default
  retention; missing, disabled or another mode is `fail`; a read error is `blocked`, never `pass`.
  [A12]
- **FR-011 Stage locks** — `stacks:generate` refuses a manifest with rows that `stacks:reconcile`
  has not materialised (`UNRECONCILED_ROW`), copies the stage's `.terraform.lock.hcl` into each new
  stack, and `stacks:check` reports two stages whose locks pin different provider versions or
  hashes (`STAGE_LOCK_MISMATCH`). [A7]
- **FR-012 Registry reads** — for every registered check whose command runs Go tests or Go
  programs, the registry's scope and inputs cover every in-module package directory its packages
  depend on (from `go list -deps`), and `test:traceability` names `tools/harness/checks.yaml` as an
  input. A missing directory is a test failure that names the check and the package. Beyond Go
  dependencies, `TestRegistryInputsCoverReads` (`tools/cmd/lz-check/main_test.go`) fails when a
  listed non-Go file a check reads is outside its scope and inputs; the lists were traced through
  the entry and are kept by hand, not derived (T053). A registry `presence` list digests the
  admitted top-level names a check reads only when they exist, so their appearing changes its
  digest (`TestRegistryDigestSeesAdmittedNames`, T053), and no foundation target's digest covers
  `pipelines/github/foundation-runs.json` (T041, T053). `test:traceability` also holds every
  function declaration under `tools/` to `tools/harness/complexity-baseline.txt` (`tools/vendor`
  and `.go` files named with a leading `.` or `_` are skipped, and package-level function literals
  are not scored, as in gocognit; `tools/cmd/lz-check/complexity.go`, `evidence/T057.md` review
  note m): a new function over 14, or
  a listed one whose score rises above its line, fails (`TestComplexityRealTree`, T057); a line
  raised in the same change as the code passes (`evidence/T057.md` row 11, open). [A8]
- **FR-013 Entry limits** — `lz-offline` runs each child inside a transient cgroup with a memory
  limit and a task limit. A child that exceeds either is stopped and the entry reports
  `RESOURCE_LIMIT <memory|tasks>` and exits non-zero. When the limits cannot be applied, the entry
  refuses before starting the child (`LIMITS_UNAVAILABLE` with the reason). The limits are
  constants of the entry, set from measured peaks (P4). [C1, KI-002]
- **FR-014 Runtime preparation in Go** — the runtime bundle preparation (archive verification,
  link-safe image projection, provider mirror, `resources.json`) is a Go command in the tools
  module with tests, and it reads its pins from `checks.Packages`, which stays the single pin table
  (`tools/internal/bundle` already imports `checks`, so the table cannot move into `bundle`);
  `tools/cmd/lz-offline/prepare.py` is removed. [A25]
- **FR-015 Update lane** — the repository carries a dependency-update configuration that covers
  every pin source (`mise.toml`, `mise.live.toml`, `tools/go.mod`, provider constraints and lock
  files, the image digest in `mise.toml`), never auto-merges, groups the tools the runtime mirrors
  and labels them as needing an entry revision, and groups provider bumps with the schema snapshot.
  A committed snapshot of the pinned provider's schema (`tofu providers schema -json`) carries its
  provider version, which must equal the pin. An offline check enforces all of this.
  Decided (D115, 2026-10-15): Renovate, in `.github/renovate.json` (inventory Q5); installing the
  app stays the owner's later step (C5). [C5]
- **FR-016 Release-train manifest** — a tools command renders the ADR-0022 release-train manifest
  for a commit and a version (`YYYY.MM.N`) to standard output: source commit, the module, component
  and stage closure with tree ids, schemas, toolchain pins, lock files, and a changelog section of
  the commits since the previous train tag (or the root) grouped by commit type, with subjects that
  do not follow the convention listed as `unclassified`, never dropped. The output is byte-identical
  across runs and validates against a committed schema; a malformed version or one that an existing
  tag already carries is refused. It claims no tested tuple, creates no tag
  and writes no file under `releases/`.
  Decided (D115, 2026-10-15): the in-repo renderer and an unsigned in-toto statement (inventory
  Q7); signing is decided at the first tag (C6). [C4]
- **FR-017 Image provenance** — `lz-pack` writes an in-toto statement with a SLSA provenance
  predicate for each image it packs: subjects are the manifest and layer digests; materials are the
  source commit, the `resources.json` digest and the entry digest. The statement is byte-identical
  across the two reproducibility packs and is not signed (D115). [C6]
- **FR-018 Docs anti-rot gates** — `docs:check` fails a page under `docs/` (ADRs and known-issue
  records excepted) whose front matter `verified_against` names a tool version more than two minor
  versions behind its pin (two behind passes, three behind fails), on an older major, or ahead of
  its pin, patch level included (`mise.toml`, `mise.live.toml`, or the provider lock files for
  `ovh`) (T041 readings 1–2: a stamp ahead names a version the repository does not run;
  `staleness` in `tools/cmd/lz-check/docs.go`, rows `ahead of its pin`, `older major` and `ahead
  only in the patch` of `TestDocsGateReadingsRejected`) or names a tool with no pin; fails a page under `docs/how-to/` or `docs/reference/` that names a
  `tofu`, `terramate`, `task` or `ovhcloud` command without `verified_against`; and fails every
  file the offline snapshot admits outside `docs/adr/` and `tools/vendor/` that links to or names
  an ADR whose status is `Superseded`, and prints `ADR_SCAN_UNJUDGED=<n>` with what it did not
  judge, by reason (a top-level name outside the snapshot, a name the snapshot skips, the CI run
  record `pipelines/github/foundation-runs.json`, an entry that is not a regular file, a binary
  file); `.gitignore` is not read (T052). [E4]
- **FR-019 Repository map** — `docs:check` fails when `docs/reference/repository-map.md` names a
  path under one of the entry's admitted top-level names that does not exist, or omits an admitted
  top-level directory present in the checkout. Paths outside the admitted names (hidden files such as
  `.gitignore` and `.sonarcloud.properties`, and any new top-level directory the entry does not admit)
  are not visible inside the entry and stay unjudged; the check reports how many map paths it
  skipped for that reason. [E6]
- **FR-020 Status pages** — `docs/reference/verification-status.md` records what T049 run 4
  observed and no longer says "UNVERIFIED until that run" for it; its plan/apply/destroy and
  destroy-budget lines match FR-001/FR-002 once those land; KI-001 drops its stale claims about the
  tree and lists its references; KI-002 records the limits and its new status once FR-013 lands. [E1,
  E2]
- **FR-021 Spec 005 wording** — spec 005's FR-009 states that a run lock is held only while its
  holder lives and that runs on different workstations are serialised by the state lock only; P9
  names `object/get`; its tasks.md says "dedicated owner clone" where the guard's subject is meant.
  [E3]
- **FR-022 Traceability** — `task check:specs` with no argument and the CI source check trace spec
  006; every requirement and task of this spec is traced through its `006/` registry keys.

### Key entities
Live run record (non-chain verbs), credential set, companion allow-list, retry budget, entry
resource limits, runtime preparation inputs, update-lane configuration, provider schema snapshot,
release-train manifest, provenance statement, docs front matter (`verified_against`). See
`data-model.md`.

## Success criteria
- **SC-001**: Every live-lane item (FR-001 to FR-010) has a regression control that is red on
  234a88d and green after its task; each security guard named in `contracts/checks.md` (G16–G23)
  has a killed mutant (V001, V002, V005).
- **SC-002**: Every task of this spec closes with offline evidence only: no task's Verify needs a
  credential, the network, an OVHcloud call or an owner session, and every item that would is a row
  of the out-of-scope table with its trigger (V010).
- **SC-003**: Under the entry, a candidate that allocates twice the memory limit or forks past the
  task limit is stopped within 10 s of crossing it, with `RESOURCE_LIMIT`, while every CI target
  stays green under the same limits (V005).
- **SC-004**: The update-lane check fails for each uncovered pin source, an auto-merge setting, an
  ungrouped runtime-mirrored tool and a schema snapshot whose version differs from the pin, and
  passes on the committed configuration (V007).
- **SC-005**: `docs:check` passes with the two new gates and the map check active, and each of them
  has a failing control (V009).
- **SC-006**: Rendering the release-train manifest for one commit twice gives identical bytes that
  validate against the schema; the render creates no tag and no file (V008).

## Acceptance and predefined verification
All new targets are **planned**; creators are in `tasks.md`; evidence starts `not-run`. Every
command runs through `lz-offline` as a Task target except two host steps of V005 (the host-tagged limit test and the peak measurement), which an agent runs on
the workstation without credentials. Controls in `contracts/checks.md`.

| Check | Requirements | Verify (planned) |
| --- | --- | --- |
| V001 | FR-003, FR-004, FR-005, FR-006, FR-007, SC-001 | `task test:live-lane; task test:bootstrap` |
| V002 | FR-001, FR-002, FR-008, FR-009, FR-010, SC-001 | `task test:live-lane` |
| V003 | FR-011 | `task test:stacks; task stacks:check` |
| V004 | FR-012, FR-022 | `task test:traceability; task check:specs` |
| V005 | FR-013, SC-001, SC-003 | `task test:runtime-image`; host: `go -C tools test -tags hostlimits -run TestHostLimits ./cmd/lz-offline` |
| V006 | FR-014, FR-017 | `task test:runtime-image` |
| V007 | FR-015, SC-004 | `task test:update-lane` (planned, T035) |
| V008 | FR-016, SC-006 | `task test:release` (planned, T036) |
| V009 | FR-018, FR-019, FR-020, SC-005 | `task docs:check` |
| V010 | FR-021, FR-022, SC-002 | `task check:specs -- specs/006-harden-first-slice` |

## Implementation surface
- `tools/internal/live/`: `apply.go`, `chain.go`, `runner.go`, `binding.go`, `leftovers.go`,
  `files.go`, `env.go`, `observe.go`, `s3check.go` and their tests; `tools/cmd/lz-live/` (`lane.go`,
  `main.go`).
- `tools/internal/stacks/generate.go`, `tools/internal/checks/` (registry reads, update lane),
  `tools/cmd/lz-check/` (docs gates, map check).
- `tools/cmd/lz-offline/` (limits), `tools/internal/bundle/` (preparation), `tools/cmd/lz-pack/`
  (provenance), new `tools/internal/release/` and `tools/cmd/lz-release/`.
- `.github/renovate.json` (default of FR-015), `tests/fixtures/provider-schema/`,
  `schemas/release-manifest.schema.json`, `Taskfile.yml`, `tools/harness/checks.yaml`.
- `docs/reference/{verification-status,repository-map}.md`, `docs/known-issues/KI-00{1,2}-*.md`,
  `specs/005-first-landing-zone-slice/{spec,tasks}.md`.

## Out of scope / postponed (with trigger)
| Item | Why not here | Trigger to start |
| --- | --- | --- |
| A6 redaction of JSON-escaped secrets | accepted in T055 (`evidence/T055.md:97`); today's secrets contain no character JSON escapes | a credential class whose secret may contain `"`, `\`, `<`, `>` or `&`, or a JSON stream that carries a secret |
| A10 injectable `DefaultGrace`, real-timer deadline row, `-race` over serial tests | priority 3 | a chain test that needs another grace, or the next change to the grace code |
| A11 second part: noncurrent `.tflock` versions and delete markers accumulate in state buckets | believed, UNVERIFIED; under Object Lock they may not be deletable before retention | the next owner chain run lists them (one read-only listing) |
| A12 live observation of `state-bucket-object-lock` (P5) | needs a live chain | the next owner chain run after T026 |
| A13, A14, A15, A18, A19, A23 | priority 3 | a change to the file each names |
| A16 reserving forgotten bucket names | owner decision (inventory Q8; recommended: no) | owner answer |
| A17 resolved-project aliasing, A24 narrower run locks | prerequisites of a second tenant | spec 007 (its first tasks) |
| A26 boundary case for a planted `stacks/Taskfile.yml` | closed by 06a8c21 | — |
| A27 SonarCloud hotspots S4790 (MD5 for the S3 `Content-MD5` header, SHA-1 for the OVHcloud API v1 request signature; both dictated by the protocol, not chosen; whether OVHcloud requires `Content-MD5` is UNVERIFIED, `s3check.go:171-172`) and S2083 (test fake) | marking a hotspot reviewed is a SonarCloud action | owner, or a token for the coordinator |
| C1 publication: pack, publish and pin the entry revision carrying FR-013; observe P3 on the runner | network and registry write | Done: r5 published and pinned in CI after P3's probe (PR 135, merged as `28caca6`; `evidence/T043.md`, `evidence/T046.md` stage-B note) |
| C2 KI-001 reassessment now that the repository is public | owner decision (inventory Q6) | owner answer |
| C3 required checks and ruleset on `main` | GitHub setting | owner |
| C4 first train tag, committed `releases/<version>/`, `CONTRACT.yaml` and the breaking-change checker | owner decision on the release model (Q7) | owner answer, or the first library consumer |
| C5 installing the update bot on the repository | GitHub app installation | owner, after FR-015 lands |
| C6 signing (cosign keyless or other) | owner decision; needs CI OIDC and network | owner answer |
| C8 trivy and terraform-docs in the runtime; C9 lowest and newest provider CI job | new tools need a full preparation round; C9 needs a network job | the next preparation round (after FR-014), or a network CI lane |
| E4 remaining ADR-0013 gates: generated reference, include markers, weekly external links, nightly how-to dry run, Vale, honesty page | new tools, network, a scheduled workflow or a missing consumer (`examples/`, `policies/guardrails.yaml`) | the consumer or the tool arrives |
| E5 unwritten ADRs (network and resilience, observability and incident response, docs accessibility) | owner-accepted documents; their consumers are B8/B10 | the spec that needs them, or an owner request |
| E6 generated repository map | constitution VI: one example | the map check of FR-019 fails more than twice in a month, or a second generated reference |
| E7 INBOX housekeeping; re-capture of `foundation-runs.json` | coordination chores, not product | coordinator |
| Theme B (second slice), Theme D (005/T045), Theme F (distant ADR promises) | not hardening | spec 007; owner fresh account; their own triggers |

## Dependencies and stop conditions
Depends on spec 005's closed tasks (the live lane, run core and docs check exist) and on the entry
r4-e2. No task needs the owner; the three owner choices are decided (D115, 2026-10-15). A
refuted premise stops its dependent task and records the fallback in `research.md` (P1 → T003
fallback; P2 → T032 fallback; P7 → T035 host capture). FR-013 must not be pinned in CI while P3 is
unobserved: a refusal on the runner would block every PR. P3 has since been observed and FR-013
is pinned (PR 135; see P3).
