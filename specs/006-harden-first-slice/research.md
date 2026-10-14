# Research: Harden and tidy the first slice
Feature: 006-harden-first-slice · 2026-10-15 · Status: draft

Each entry: decision, rationale, alternatives rejected, evidence. Code references are at 234a88d.
Every inventory item was re-checked against the code before it was admitted; the status found is
recorded here, because three items differed from the inventory.

## R1 Scope cut: what the inventory said and what the code shows
**Decision**: admit A1, A2, A3, A4, A5, A7, A8, A9, A11 (first part), A12, A20, A21, A22, A25, C1,
C4 (tooling), C5, C6 (statement), E1, E2, E3, E4 (two gates), E6 (as a check).
**Findings that changed the cut**:
- A21 is **open**, not "likely closed", but narrower than the inventory says. The destroy context is
  already detached from the run's cancellation (`context.WithTimeout(context.WithoutCancel(ctx),
  deadline)`, `tools/internal/live/chain.go:285-287`; probe core `runner.go:921-923`). The gap is the
  budget: `deadline` is the run's `--deadline`, defaulted to `DefaultDeadline` only when unset
  (`chain.go:226-228`, `runner.go:830-832`), so a run given a short deadline also gets a short
  destroy. Admitted as FR-002 because destroy-on-exit is a constitution V prerequisite and a
  too-short budget turns a cleanup into a leftover.
- A26 is **closed**: `TestTrustedTargetsIgnoreCandidate` (`tools/internal/probes/security/offline_test.go:256-300`,
  06a8c21) plants `stacks/Taskfile.yml` and asserts no `LZ_OBSERVATION`.
- A20 is **closed in production code** (every child gets `scratchHome`, `files.go:261`; callers
  `bootstrap_state.go:152-155,232`, `apply.go:247`, `chain.go:219,487`, `runner.go:855`), but the
  exported `Command` (`env.go:26-29`) still passes the caller's `HOME`, and no bootstrap row pins it.
  Admitted as FR-007: a small residue on a priority-1 item.
- A6 is marked "Accepted, latent" in `evidence/T055.md:97`; the inventory's own mitigation refuses
  accepted items. Postponed with a trigger.
- A22 is priority 3 but lives in the same function as A1 (`apply.go:142-158`, `runLane`); admitted
  under the inventory's "shares a file" rule.
- A27: both hashes are dictated by the protocol, not chosen: MD5 for the S3 `Content-MD5` header
  (`s3check.go:175`; whether OVHcloud requires it is UNVERIFIED, `s3check.go:171-172`) and SHA-1 for
  the OVHcloud API v1 request signature (`rootkeys.go:74`; no live use until 005/T045). No code
  change; marking the hotspots reviewed is a SonarCloud action.
- E3: two of the five wording fixes are already done (R21 plan-all block at
  `specs/005-first-landing-zone-slice/research.md:496`; G7 component part at `contracts/checks.md:43`).
  "Main checkout" remains only in `specs/005-first-landing-zone-slice/tasks.md:78`; the T071 Verify
  at `:128` is D92 history and stays.
**Rejected**: taking the inventory's statuses as given (three were wrong); admitting priority-3
items wholesale (inventory counterpoint: gold-plating a PoC).

## R2 Bounded verbs outside the chain (FR-001)
**Decision**: `runLane` builds the same run wrapper for `plan`, `apply` and `destroy` as for the
chain: deadline (default `DefaultDeadline`), inventory for `apply`, summary line for all three, cost
reminder for `apply` and `destroy`. The entry passes the deadline it receives instead of 0
(`tools/cmd/lz-live/main.go:173`).
**Rationale**: the chain already has a tested wrapper (`laneRunner.summary`, `chain.go:117-155`);
reuse keeps one implementation. Today `r.inv` is set only by the chain (`chain.go:222`) and the
wrapper only when `ch != nil` (`apply.go:156-158`).
**Rejected**: a separate wrapper per verb (two implementations of the same guard); making `apply`
destroy on deadline (an `apply` outside the chain may target a retained instance; the run stops
and keeps its inventory instead).
**Evidence**: `specs/005-first-landing-zone-slice/evidence/T059.md:89`; `docs/reference/verification-status.md:64`.

## R3 Destroy budget (FR-002)
**Decision**: the destroy budget becomes `max(DefaultDeadline, run deadline)`; the context stays
detached from the run's cancellation, as today.
**Rationale**: a run started with `--deadline 5m` (for a quick plan-and-apply) must not leave five
minutes, or less, to destroy what it created. The floor keeps the behaviour predictable and testable
with a fake clock.
**Rejected**: a separate `--destroy-deadline` flag (one more knob the owner must remember; the floor
covers the observed case); an unbounded destroy (a hung provider would hold the workstation forever;
constitution V asks for a bounded runtime).
**Evidence**: `evidence/T047.md:79`; `chain.go:226-228,285-287`; `runner.go:830-832,921-923`.

## R4 Secret hygiene in errors and records (FR-003, FR-004, FR-005)
**Decision**:
- FR-003: route `API.do`'s two `%w` paths (`binding.go:54` transport, `:63` decode) through the
  existing `quiet` helper (`leftovers.go:154-165`) so `Bind` reports the call and status class only.
  The status path (`:59-61`) already omits the body. The unsafe case is a transport error that
  quotes server-chosen text: the client does not follow redirects (`binding.go:45`), but Go's
  client parses the `Location` of a 3xx answer before consulting `CheckRedirect`, and an
  unparseable `Location` comes back as an error that quotes the header.
- FR-004: `LeftoverCheck` receives the run's redactor; `apiLister` calls `Add` with the token as soon
  as it obtains it, before the first listing.
- FR-005: generation directories plus one pointer. Each writing instance (`account-governance`, and
  each `tenant-state`) owns `~/.config/ovh-lz/accounts/<account>/sets/<instance_id>/`. A write
  creates `gen-<run_id>/` (mode 700) holding the set's files at their usual relative paths, fsyncs
  each file and the directory, then writes `current.tmp` (mode 600, content `gen-<run_id>`), fsyncs
  it and renames it over `current`: one `rename(2)` is the switch. After the switch the writer keeps
  the previous generation (one step of manual rollback) and removes older ones and any generation not
  named by `current`. Readers (`credentials.go:49-53` paths) read `current` once, accept only a value
  matching `gen-[0-9TZa-f-]+` that names an existing sibling directory, and open the file under that
  generation; no symlink is involved, so the files' no-symlink and mode rules (G3) hold unchanged.
  Migration: while `current` is absent, readers use today's flat paths; the first write creates a
  generation and the pointer, and removes the flat files only after the switch (a crash between the
  two leaves the pointer authoritative; the next write removes the flat files).
**Rationale**: `quiet` is already tested (`TestLeftoversAPINoEchoInErrors`,
`leftovers_api_bounds_test.go:245`); redaction already covers credential values (`chain.go:389`); the
gap is only that the token is not registered. POSIX rename is atomic per file, not per set; a
pointer renamed once is the only single atomic switch, and the old generation stays intact and
readable until it.
**Rejected**: per-file rename into the live paths with a commit marker renamed last (round-1 design:
a crash after some files are renamed leaves the previous set lost, since its files are already
overwritten); a single combined file (changes 005 FR-010's layout and every reader); a symlink as
pointer (needs an exception to the files' no-symlink rule); renaming the account directory (it also
holds the binding and passphrase, which must not move).
**Evidence**: `evidence/T084.md:137-149`; `evidence/T059.md:94`; `apply.go:597-608`; `files.go:168`.

## R5 Entry limits (FR-013)
**Decision**: run bwrap inside a transient cgroup v2 scope with `MemoryMax` and `TasksMax`, via
`systemd-run --user --scope --quiet --collect -p MemoryMax=<m> -p TasksMax=<t> -- <bwrap …>` from the
entry; read `memory.events` (`oom_kill`) and `pids.events` (`max`) after the child ends to report
`RESOURCE_LIMIT memory|tasks`. If `systemd-run` is absent, the user manager is unreachable, or the
scope lacks the `memory` or `pids` controller, refuse with `LIMITS_UNAVAILABLE <reason>` before the
child starts. `systemd-run` is resolved from a fixed system path and its digest is not pinned (it is
host software, like bwrap's dependencies), but the entry verifies after start that the child's cgroup
has the requested `memory.max` and `pids.max` and refuses otherwise.
**Limits**: measured, not guessed. T032 records per-target `memory.peak` and `pids.peak` for every CI
target through the entry; the limits are twice the largest peak, rounded up to a power of two, and
are constants of the entry (P4). Initial placeholders for the tests: 8 GiB and 1024 tasks.
**Rejected**: `setrlimit` (`RLIMIT_AS` is per process and breaks Go's large virtual reservations;
`RLIMIT_NPROC` counts every process of the real user, so it would also kill the operator's shell
work and is not namespace-local); a host-wide cgroup created by root (needs privilege the entry must
not hold); "warn and continue" when limits are unavailable (KI-002's fix sketch says refuse; a
warning is a silent pass under constitution V).
**Risk**: GitHub runners may not delegate user cgroups (P3). The revised entry is therefore not
pinned in CI until a CI run observes P3; publication is out of scope (coordinator, D89). Fallback if
P2 is refuted on the host: create a child cgroup directly in the user's delegated subtree
(`/sys/fs/cgroup/user.slice/user-<uid>.slice/user@<uid>.service/…`) and move the bwrap pid into it.
**Evidence**: `docs/known-issues/KI-002-offline-entry-memory-process-limits.md:22-62`;
`tools/cmd/lz-offline/main.go:303-311,456-567`.

## R6 Runtime preparation in Go (FR-014)
**Decision**: port `tools/cmd/lz-offline/prepare.py` (341 lines) to a `prepare` subcommand of
`lz-pack`, with the link-safe projection (`unpack`, `refuse_links`) and archive verification in
`tools/internal/bundle`, reading its pins from `checks.Packages`, which stays the one pin table
(`tools/internal/bundle/bundle.go:18` already imports `checks`, so the table cannot live in `bundle`
without an import cycle). The
out-of-repo drivers (`prep.sh`, `build-runtime.sh`) call the Go command at the next preparation
round; `prepare.py` is deleted in the same task.
**Rationale**: ADR-0002 (`:81-82`) chose Go and rejected Python; the Python file has no in-repo tests
(`specs/001-offline-foundation/evidence/T025.md:86`), and Python is not in the runtime, so CI could not
run its tests anyway. The pins are duplicated today between `prepare.py:20-68` and `checks.Packages`.
**Rejected**: a host-side Python test target (keeps the duplication and a second language); leaving
`prepare.py` beside the Go port (two preparers drift).
**Note**: the network fetch of archives stays host-side and out of the tests: the Go tests run on
local fixture archives (a minimal tar with a symlink escape, a hard link, a valid file) and assert
the refusals and the `resources.json` shape.

## R7 Companion allow-list (FR-006)
**Decision (D115, 2026-10-15, owner Q4)**: allow, case-sensitively, `TF_VAR_[A-Za-z0-9_]+` minus the
run's own `TF_VAR_state_passphrase`, `TF_VAR_state_path`, `TF_VAR_run_id` and `TF_VAR_project_id`
(refused in any case, as today), and the complete pairs `OVH_CLIENT_ID`/`OVH_CLIENT_SECRET` and
`AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`; refuse anything else by name.
**Case**: OpenTofu reads variables from environment names starting exactly `TF_VAR_`; a `tf_var_` or
`Tf_Var_` name sets nothing, so admitting it only widens what passes. Today's `passed/neighbours`
subtest (`probe_identity_test.go:1158-1168`) admits such names and `HOMEPAGE`, `XPATH`, `TMPDIRS`,
`MY_TF_LOG` and the like; T010 inverts it: names outside the allow-list are refused, while
`TF_VAR_` neighbours that are not reserved (`TF_VAR_run_id_suffix`, `TF_VAR_state`, …) stay admitted.
**Rationale**: `companionEnv` (`runner.go:530-547`) refuses a reserved list plus a `TF_*` rule; any
name not foreseen passes. An allow-list fails closed. T082 recorded this as the coordinator's
deferred decision (`evidence/T082.md:74-75`; loop.md of spec 005).
**Rejected**: extending the deny-list (the next unforeseen name passes again).

## R8 Update lane (FR-015)
**Decision (D115, 2026-10-15, owner Q5)**: Renovate, configuration in `.github/renovate.json`, with
`automerge: false`, one group for the tools mirrored in the runtime (Go, OpenTofu, Terramate, Task,
TFLint, the base image digest) labelled `needs-entry-revision`, one group for `ovh/ovh` together with
`tests/fixtures/provider-schema/`, and `mise.live.toml` separately. The offline check
(`tools/internal/checks/updatelane.go`) reads the configuration and the pin files and enforces the
clauses of FR-015; it does not run Renovate.
**Manager names** (P6): believed to be `mise`, `gomod`, `terraform` and `regex` (for the image
digest in `mise.toml`). No source was read for this spec, so they are **UNVERIFIED**, and no task
reads external documentation: the offline check of T035/T037 judges only the repository's own rules
(every pin source has a rule naming its file, no auto-merge, the groups and the label, the snapshot)
and does not check manager names. Whether Renovate accepts the file is observed when the owner
installs the app (out of scope).
**Rationale**: a bump of a runtime-mirrored tool in `mise.toml` alone desynchronises the repository
from the pinned entry (the entry's `checks.Packages` and resources); grouping and a label make the
required entry revision visible instead of a red CI with no explanation. ADR-0011:51-52 names Renovate
and the schema snapshot.
**Rejected**: Dependabot (no mise support believed; would leave `mise.toml` uncovered);
no bot (ADR-0021 asks for a reviewed update lane).

## R9 Release-train manifest and provenance (FR-016, FR-017)
**Decision (D115, 2026-10-15, owner Q7)**: `tools/cmd/lz-release manifest --commit <sha> --version
<YYYY.MM.N>` prints YAML in the JSON-compatible flow form used by the registry (D96), decoded and
validated with `checks.DecodeStrict` against `schemas/release-manifest.schema.json`. It reads git
objects only (no working-tree files), so the output depends on the commit alone. The entry's
snapshot does not carry `.git` (`tools/cmd/lz-offline/main.go:197`), so the checks run inside the
entry on a fixture repository the test builds with the runtime's `git`; rendering the real
repository is a read-only host step with no credentials, not part of any Verify. The changelog groups
commits by type (`feat fix docs style refactor test chore ci build`) from the previous train tag, or
from the root when none exists; non-conforming subjects are listed under `unclassified`.
`tested` is always an empty list in this spec: no tuple is claimed without live evidence.
For FR-017, `lz-pack` writes `provenance.json` (in-toto Statement v1, predicate
`https://slsa.dev/provenance/v1`) next to its image output, serialised with sorted keys.
**Rationale**: ADR-0010's counterpoint favours a small in-repo tool; release-please needs the network
and a GitHub app. An unsigned statement is still useful: it binds the image digest to the source
commit and resources, and signing can wrap it later without changing it.
**Rejected**: committing `releases/<version>/` now (that is the first release, an owner decision);
generating tags (owner); a timestamp in the statement (breaks reproducibility of the two packs).

## R10 Docs gates and the repository map (FR-018, FR-019)
**Decision**: extend `tools/cmd/lz-check/docs.go`: (1) parse an optional `verified_against:` mapping
(`tofu`, `ovh`, `terramate`, `task`, `tflint`, `go`, `ovhcloud`) and compare each with `mise.toml`,
`mise.live.toml` and the `ovh/ovh` lock-file pin by minor version: two minors behind passes, three
fails (ADR-0013 "more than two"); (2) scan text files outside `docs/adr/` and `tools/vendor/` for
`ADR-NNNN` or links to `docs/adr/NNNN-*.md` and fail when that ADR's status line starts
`Superseded`; (3) compare `repository-map.md` with what the entry can see.
**What the entry can see**: the snapshot admits a fixed list of top-level names
(`tools/cmd/lz-offline/main.go:197`); hidden files (`.gitignore`, `.sonarcloud.properties`) and any
top-level directory outside that list are absent inside the entry. So (3) judges only map paths under
admitted names (they must exist) and admitted top-level directories present in the checkout (they
must be mapped), and reports the number of map paths it skipped. A path inventory passed in as input
was rejected: it would have to be produced on the host and committed, which is the generator the
map check avoids, and a stale inventory would hide exactly the drift it should find.
**Registry**: `docs:check` gains `mise.toml`, `mise.live.toml`, the stage and module lock files and
the files the superseded scan reads as inputs (T041, T042), each with a row in
`TestRegistryInputsCoverReads`.
**Rationale**: all three are offline and need no new tool (ADR-0013:36-37, :50-51; ADR-0002:90).
Requiring `verified_against` only on pages that name a tool command keeps the churn to the pages
that make version claims.
**Rejected**: a generator for the map (constitution VI: generators follow two real examples); Vale,
terraform-docs and external links (new tools or network; postponed).

## R11 Registry reads from `go list -deps` (FR-012)
**Decision**: a test in `tools/cmd/lz-check/main_test.go` runs `go list -deps -f '{{.Dir}}'` for the
packages each Go-running check names (parsed from its Taskfile command), keeps directories inside
the tools module, and asserts each is covered by the check's scope or inputs. The registry is
widened to the missing directories in the same task (adding reads, never removing a check).
**Rationale**: T111 widened only non-`tools/` trees (`evidence/T111.md:65-66`); a change to an
imported package (e.g. `tools/internal/report` for `test:stacks`) does not change the check's digest
today.
**Note on the chore rules**: widening registry *inputs* to what a check provably reads makes freshness
stricter; it is not the forbidden widening of an allow-list, exclusion list or baseline.
**Fallback (P1 refuted)**: a committed list per check, refreshed by a host-side test.

## R12 Stage locks and unreconciled rows (FR-011)
**Decision**: `Generate` (`tools/internal/stacks/generate.go:59-82`) refuses first when reconcile
would create a stack (`UNRECONCILED_ROW <id>`), then copies `stages/<stage>/.terraform.lock.hcl`
into each newly created stack; `stacks:check` compares every stage lock's provider versions and
hashes and reports `STAGE_LOCK_MISMATCH`.
**Rationale**: the offline planner copies a missing lock only into scratch (`plan.go:277-286`);
stacks share one plugin cache (`plan.go:190-196`), so disagreeing stage locks race.
**Evidence**: `evidence/T039.md:73`, `T038.md:101,104`.
