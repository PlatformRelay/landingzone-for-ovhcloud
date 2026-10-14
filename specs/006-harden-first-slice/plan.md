# Implementation Plan: Harden and tidy the first slice
Branch: worktree of `main` per task · Date: 2026-10-15 · Spec: [spec.md](spec.md) · Status: draft

## Summary
Close the recorded safety and operability gaps of spec 005 in the code that already exists, add
memory and process limits to the offline entry, move runtime preparation into tested Go, give
dependency bumps, releases and the runtime image an offline-checked lane, and correct the documents
that drifted. Nothing new is deployed and no task needs the owner. Decisions and evidence:
[research.md](research.md).

## Technical context
- **Language/version**: Go 1.27.1 (tools module, vendored), OpenTofu 1.13.0, Terramate 0.17.3,
  Task 3.53.1 (`mise.toml`); provider `ovh/ovh` 2.21.0 from the entry's mirror.
- **Runtime**: offline entry r4-e2 (`pipelines/github/foundation-source.json`); every check runs
  through `lz-offline`. The one exception is the host-tagged limit test (FR-013), which needs the
  host's user manager and runs on the workstation without credentials.
- **Testing**: Go tests with the fakes spec 005 built (fake git, `tofu`, `ovhcloud`, OVHcloud API,
  S3 endpoint, terminal); local fixture archives for preparation; captured tool output for the
  provider schema.
- **Cost**: none. No live run, no API call (AGENTS.md sandbox budget: docs and offline changes run
  no live layer).
- **Constraints**: no secret in output, argv, files or the repository; no network inside the entry;
  credentials directory off limits to agents.

## Constitution check
Before research and after design (unchanged by design).

| Principle | Status | Note |
| --- | --- | --- |
| I Evidence, UNVERIFIED | pass | P1–P7 named with probes; P2, P4 and P7 are observed by the task that needs them before implementation closes; P3 and P5 (live/runner) gate only out-of-scope publication and a live assertion run |
| II Traceability | pass after T002 | per-task Requirements/Verify/Evidence; spec registered with `006/` keys; T001/T002 make the default trace and CI's source check read spec 006 |
| III Tests first | pass | each code task records red before the fix and green after (regression rule); new targets (`test:update-lane`, `test:release`) are created by test tasks (T035, T036) before their implementation tasks (T037, T038); every story lists its test tasks before its implementation tasks, against the signatures fixed in data-model.md |
| III Mutation proof | pass (scoped) | mutation claimed only for guards G16–G23 (`contracts/checks.md`) |
| IV One owner, explicit authority | pass | no authority change; FR-006 narrows what a probe root may hand its companion |
| V Fail closed; cleanup | pass | FR-002 gives cleanup its own time; FR-008 reports `blocked` instead of a late pass; FR-010 read errors are `blocked`; FR-013 refuses when limits are unavailable |
| V Cost | pass | no live run |
| VI Smallest increment; generators after two examples | pass | the repository map gets a drift check, not a generator (R10); the release renderer is not a generator of a committed artefact kind: it prints, and nothing under `releases/` is committed |
| VI AgentEx | deferred (unchanged) | not a vertical slice; the trigger does not fire |
| VII Planned vs implemented | pass | new commands planned with creators |
| VIII Decisions | pass | the three owner choices (Q4, Q5, Q7) are decided under D115 (2026-10-15), so no dependent work is gated; counterpoints in research |

## Alternatives and decisions
Detail per entry in `research.md`. Summary of the interface choices an implementer could get wrong:

| Choice | Chosen | Rejected because |
| --- | --- | --- |
| Entry limits | transient cgroup v2 scope, refuse when unavailable (R5) | `setrlimit` cannot bound the total and counts the whole user |
| Credential set | one generation directory per write and a `current` pointer file replaced by one `rename(2)` (R4) | per-file rename into live paths with a marker last loses the previous set on a crash between files; one combined file changes 005's layout; a symlink pointer needs a no-symlink exception; renaming the account directory moves the binding |
| Destroy budget | `max(DefaultDeadline, run deadline)`, still detached from the run's cancellation (R3) | a separate `--destroy-deadline` flag (one more knob); an unbounded destroy |
| Preparation | Go in `lz-pack prepare` + `tools/internal/bundle` (R6) | Python target keeps two languages and duplicated pins |
| Repository map | drift check (R10) | generator before a second example |
| Release | in-repo renderer to stdout, unsigned statement (R9) | release bot needs network and an app; signing is an owner choice |

Owner choices decided under D115 (2026-10-15): Q4 allow-list (T010/T016), Q5 Renovate (T035/T037), Q7
in-repo renderer and unsigned statement (T036/T038, T031/T034); signing waits for the first tag (C6).

## Project structure

### Documentation
```text
specs/006-harden-first-slice/
├── spec.md  plan.md  research.md  data-model.md  quickstart.md
├── checklists/requirements.md
├── contracts/checks.md
├── tasks.md
└── evidence/T0NN.md          # per task, created when the task closes
```

### Source (changed or new paths)
```text
tools/internal/live/{apply,chain,runner,binding,leftovers,files,env,observe,s3check}.go  + tests
tools/cmd/lz-live/{lane,main}.go                                                       + tests
tools/internal/stacks/generate.go                                                      + tests
tools/internal/checks/updatelane.go                         # new, T037 (tests T035)
tools/cmd/lz-check/{docs,main}.go                                                      + tests
tools/cmd/lz-offline/{main,limits}.go                       # limits.go new, T032 (tests T029)
tools/cmd/lz-offline/prepare.py                             # removed, T033
tools/internal/bundle/prepare.go                            # new, T033 (tests T030)
tools/cmd/lz-pack/{main,provenance}.go                      # provenance.go new, T034 (tests T031)
tools/internal/release/  tools/cmd/lz-release/              # new, T038 (tests T036)
schemas/release-manifest.schema.json                        # new, T036
tests/fixtures/provider-schema/                             # new, T035
.github/renovate.json                                       # new, T037 (Q5, D115)
tools/harness/checks.yaml  Taskfile.yml
docs/reference/{verification-status,repository-map}.md  docs/known-issues/KI-00{1,2}-*.md
specs/005-first-landing-zone-slice/{spec,tasks}.md
```

## Delivery phases
1. **Setup** (T001–T004): trace 006 by default and in CI's source check; registry reads complete.
2. **US1 secrets** (tests T005–T010, then T011–T016): binding errors, leftover token, credential set, scratch home,
   companion allow-list. Shared files are ordered by `Depends on` (see *Dependencies*).
3. **US2 bounded verbs** (tests T017–T021, then T022–T026): tests then implementation for FR-001; destroy budget; retry
   budget; canary; Object Lock assertion.
4. **US4 generation** (T027, T028).
5. **US3 entry and runtime** (tests T029–T031, then T032–T034): limits tests (with the peak measurement) then
   implementation; preparation tests then port; provenance.
6. **US5 supply chain** (tests T035–T036, then T037–T038): schema snapshot, update-lane tests and target, then the check and configuration;
   release manifest tests and target, then the renderer.
7. **US6 docs** (tests T039–T040, then T041–T042; docs T043–T044): gate tests then gates; map check test first; status pages; spec 005
   wording.
8. **Exit** (T045 CI admission, T046 exit checks).

## Verification strategy
V001–V010 in `contracts/checks.md`. Every Go task records the failing control on the unfixed tree
(behavioural red, not a compile error) and the same control green after; the evidence file names
the commit of each. New targets are registered in `tools/harness/checks.yaml` by their creator task
with scope and inputs that cover what they read (FR-012 makes that checkable). CI: the new targets
join `pipelines/github/foundation-source.json` only after their creator closes (`ci-source` refuses
an open creator), through `generate:foundation-ci`; that is done once in T045. The entry
limits (T032) run in CI only after the revised entry is published and P3 observed (out of scope).

## Dependencies and live change ordering
No live change. External gates: none from the owner (Q4, Q5, Q7 decided, D115); entry
publication for FR-013 to take effect in CI (coordinator, out of scope). Inside the spec the
`Depends on` fields in `tasks.md` form a DAG; files shared by two tasks are ordered (`chain.go`:
T012 → T022 → T023 → T024; `apply.go`: T014 → T022; `observe.go`: T025 → T026; `runner.go`:
T016 → T023; `docs.go`: T041 → T042; status pages after the code they describe).

## Risks
| Risk | Effect | Mitigation |
| --- | --- | --- |
| Runner lacks user cgroup delegation (P3) | revised entry refuses every CI target | not pinned in CI until observed; fallback path in R5 |
| Limits too tight for a slow target | false `RESOURCE_LIMIT` | limits from measured peaks × 2 (P4); evidence records the peaks |
| Go port of preparation diverges from the published r4-e2 bundle | next revision differs unexpectedly | T030 compares the Go port's `resources.json` on fixtures with the shape `bundle.Admit` reads; the first real preparation is a coordinator round with its own review |
| Update bot bumps a runtime tool alone | CI red without explanation | group + `needs-entry-revision` label, enforced by the check |
| Docs gate churn | many pages need `verified_against` | required only on pages naming a tool command (R10) |

## Complexity tracking
| Addition | Why | Simpler alternative rejected |
| --- | --- | --- |
| `systemd-run` dependency of the entry | only unprivileged way to bound total memory and tasks | `setrlimit` (R5) |
| Generation directories plus a pointer file for credential sets | POSIX has no multi-file atomic rename; only one renamed pointer keeps the old set readable until the switch | per-file rename with a marker (R4) |
| `lz-release` command | ADR-0022 manifest needs the closure from git objects | hand-written manifest (drifts; no reproducibility check) |
| Provider schema snapshot fixture | reviewable diff on every provider bump (ADR-0011) | none: the bump would be unreviewable |
