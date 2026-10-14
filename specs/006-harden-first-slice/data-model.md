# Data model: Harden and tidy the first slice
Feature: 006-harden-first-slice · 2026-10-15 · Status: draft

Only what this spec adds or changes. Spec 005's entities (`specs/005-first-landing-zone-slice/data-model.md`)
are unchanged unless named here.

## Live run record, non-chain verbs (FR-001)
Same directory and files as the chain's run record (005 data model, *Live run record*), written by
`plan`, `apply` and `destroy` as well:
- `run.json`: `run_id`, `verb` (`plan|apply|destroy|chain`), `instances`, `deadline_seconds`,
  `outcome` (`pass|fail|blocked|not-run`), `reviewed_sha`.
- `inventory.jsonl` (`apply` only): one line per created resource, appended as `apply_complete`
  arrives (005 P24).
- Summary line on stdout: `LZ_RUN <run_id> <verb> <outcome>`, plus ` inventory=<path>` for `apply`; `apply` and
  `destroy` add the cost reminder line the chain prints.
Destroy budget (FR-002): `destroy_budget_seconds` in `run.json`, at least `DefaultDeadline`.

## Credential set (FR-005)
Per writing instance, under `~/.config/ovh-lz/accounts/<account>/sets/<instance_id>/`:
- `gen-<run_id>/` (mode 700): one complete generation, holding the set's files at the relative paths
  readers use today (`platform-deployer.env`, `tenants/<tenant>/deployer.env` for
  `account-governance`; `tenants/<tenant>/state.env`, `tenants/<tenant>/platform-state.env` for
  `tenant-state`), each mode 600.
- `current` (mode 600): a regular file whose whole content is the name of the current generation,
  e.g. `gen-20261015T101500Z-1a2b`; replaced only by writing `current.tmp`, fsync, and one
  `rename(2)` over `current`.
- At most two generations after a write: the current one and the previous one (manual rollback);
  anything else is removed by the writer after its switch.
Write steps and what a crash at each leaves:

| Step | Crash here leaves |
| --- | --- |
| 1 create `gen-<run_id>/`, write and fsync its files | previous `current` → previous set; partial generation removed by the next writer |
| 2 fsync the generation directory | same |
| 3 write and fsync `current.tmp` | same; stale `current.tmp` removed by the next writer |
| 4 `rename(current.tmp, current)` — the switch | before: previous set; after: new set; never a mix |
| 5 remove older generations and, on first migration, the flat files | new set current; leftovers removed by the next writer |

Readers resolve `current` once per load, accept only `^gen-[0-9A-Za-z-]+$` naming an existing
sibling directory, and read every file of the set from that generation (`CREDENTIAL_POINTER_INVALID`
otherwise). While `current` does not exist, readers use the flat paths of 005 (migration).
`account.env`, the passphrase, `sandbox.env` and the bootstrap's `state.env` are not part of any set
and do not move.

## Companion allow-list (FR-006)
Admitted names, matched case-sensitively: `^TF_VAR_[A-Za-z0-9_]+$` except `TF_VAR_state_passphrase`,
`TF_VAR_state_path`, `TF_VAR_run_id`, `TF_VAR_project_id` (refused in any case); `OVH_CLIENT_ID`,
`OVH_CLIENT_SECRET`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`. Any other name, including
`tf_var_…` and `Tf_Var_…` → `COMPANION_NAME_REFUSED <name>`. Identity pairs must be complete (both
members or neither).

## Retry budget (FR-008)
One budget per leftover check: `budget_seconds` (from the run deadline), `reserve_seconds` (read
plus remaining listings, estimated from the slowest listing so far, floor 30 s). A wait of `w` is
admitted only if `remaining − w ≥ reserve`; otherwise outcome `blocked waiting-for=<instance>`.

## Entry resource limits (FR-013)
Constants in `tools/cmd/lz-offline/limits.go`: `memoryMax` (bytes), `tasksMax`. Output lines:
`RESOURCE_LIMIT memory|tasks target=<target>` and `LIMITS_UNAVAILABLE <reason>` where reason is one
of `no-systemd-run`, `no-user-manager`, `no-memory-controller`, `no-pids-controller`,
`limit-not-applied`.

## Runtime preparation inputs (FR-014)
One Go table, `checks.Packages` (it exists today and `tools/internal/bundle` already imports
`checks`), extended where `prepare.py:20-68` holds pins it lacks: per artefact `name`, `version`,
`url`, `sha256`, `kind` (`archive|apk|provider|image`). `bundle`'s `prepare` reads it;
`resources.json` keeps its present shape (read by `bundle.Admit`).

## Update-lane configuration (FR-015)
`.github/renovate.json` (default): `automerge: false`; `packageRules` with groups `runtime-tools`
(label `needs-entry-revision`), `provider-ovh` (with `tests/fixtures/provider-schema/**`),
`host-tools` (`mise.live.toml`), `go-modules`. The check maps each pin source to a covering rule:

| Pin source | Pins |
| --- | --- |
| `mise.toml` `[tools]` | go, opentofu, terramate, task, tflint |
| `mise.toml` `LZ_OFFLINE_IMAGE` | base image digest |
| `mise.live.toml` | ovhcloud CLI |
| `tools/go.mod` | Go modules (vendored) |
| `**/.terraform.lock.hcl`, `required_providers` | `ovh/ovh` |

## Provider schema snapshot (FR-015)
`tests/fixtures/provider-schema/ovh-<version>.json`: output of `tofu providers schema -json` for a
root requiring only `ovh/ovh` at the pin, plus `ovh-<version>.command.txt` with the command, the
`tofu` version and the date. The check compares `<version>` with the lock files' pin.

## Release-train manifest (FR-016)
Flow-form YAML on stdout, validated by `schemas/release-manifest.schema.json`:
`apiVersion: lz.platformrelay.dev/v1alpha1`, `kind: ReleaseTrain`, `version` (`^\d{4}\.(0[1-9]|1[0-2])\.\d+$`),
`source_commit` (40-hex), `closure` (list of `{path, tree}` for each directory under `modules/`,
`components/`, `stages/`), `schemas` and `lockfiles` (`{path, blob}`), `toolchain` (from
`mise.toml` at the commit), `tested: []`, `changelog` (`{type: [subjects]}` plus `unclassified`).
Keys sorted; no timestamps.

## Provenance statement (FR-017)
`provenance.json` beside the packed image: in-toto `_type` Statement v1; `subject`: manifest and
layer `{name, digest: {sha256}}`; `predicateType: https://slsa.dev/provenance/v1`; `buildDefinition`
with `externalParameters` (source commit) and `resolvedDependencies` (`resources.json` and entry
digests); `runDetails.builder.id` naming `lz-pack` and its version. Sorted keys, no timestamps.

## Docs front matter (FR-018)
Optional `verified_against:` mapping, e.g. `verified_against: {tofu: "1.13", ovh: "2.21"}` (minor
precision). Two minor versions behind the pin passes; three or more fails (ADR-0013 "more than two"). Required on pages under `docs/how-to/` and `docs/reference/` that name a `tofu`,
`terramate`, `task` or `ovhcloud` command. Rule ids: `VERIFIED_AGAINST_MISSING`,
`VERIFIED_AGAINST_STALE`, `VERIFIED_AGAINST_UNKNOWN_TOOL`, `SUPERSEDED_ADR`, `MAP_MISSING_DIR`,
`MAP_PATH_GONE`; the map check also prints `MAP_UNJUDGED=<n>` for map paths outside the entry's
admitted top-level names.

## Fixed signatures for the test tasks
A test task writes its tests against these signatures, with a stub whose body returns "not
implemented" where the symbol is new; the implementation task keeps the signature. Existing symbols
keep their present signature unless a change is listed here. Package paths are under `tools/`.

| Req | Symbol (package) | Signature or contract |
| --- | --- | --- |
| FR-003 | `Bind` (`internal/live`) | unchanged: `func Bind(ctx context.Context, api API, c Credential, b Binding, manifestOrg string) error`; tests drive it with `API{HTTP: …}` against an `httptest` server |
| FR-004 | `LeftoverCheck` (`internal/live`) | new field `Red *Redactor`: when non-nil, the bearer token is passed to `Red.Add` before the first listing; `Check(ctx, inv) Report` unchanged |
| FR-005 | `SetStep`, `CredentialSet`, `WriteCredentialSet`, `CredentialSetPath` (`internal/live`) | `type SetStep int` with `SetStepFirstFile, SetStepFiles, SetStepDirSync, SetStepPointerTmp, SetStepSwitch, SetStepCleanup` (1…6, in write order); `type CredentialSet struct { Root, Account, Instance, RunID string; Files map[string]map[string]string }` (relative path → variables); `func WriteCredentialSet(s CredentialSet, failAfter SetStep) error` (0 in production; a non-zero step makes it return `ErrInjectedCrash` right after that step, leaving the disk as a crash would); `func CredentialSetPath(root, account, instance, rel string) (string, error)` (resolves through `current`, falls back to the flat path while `current` is absent, error text starts `CREDENTIAL_POINTER_INVALID`) |
| FR-006 | `admitCompanionName` (`internal/live`) | `func admitCompanionName(name string) error`: nil for an allowed name, else an error whose text is `COMPANION_NAME_REFUSED <name>`; pair completeness stays in `companionEnv` |
| FR-007 | `Command` (`internal/live`) | changed: `func Command(a Authority, creds map[string]string, home, name string, arg ...string) *exec.Cmd` (the child's `HOME` is `home`; the caller's is never read) |
| FR-001 | `ApplyOptions.Deadline` (`internal/live`) | new field `Deadline time.Duration` (0: `DefaultDeadline`), honoured by `runLane(ctx, o, nil)` for `plan`, `apply` and `destroy`; summary line `LZ_RUN <run_id> <verb> <outcome>[ inventory=<path>]` |
| FR-002 | `destroyBudget` (`internal/live`) | `func destroyBudget(run time.Duration) time.Duration`: `max(DefaultDeadline, run)`; `run <= 0` gives `DefaultDeadline` |
| FR-008 | `retryBudget` (`internal/live`) | `type retryBudget struct{…}`; `func newRetryBudget(end time.Time, reserve time.Duration) *retryBudget`; `func (b *retryBudget) admit(now time.Time, wait time.Duration) bool`; `readRetained` gains it: `func (r *laneRunner) readRetained(ctx context.Context, in stacks.Instance, into map[string][]string, b *retryBudget) error`, whose refusal text is `blocked waiting-for=<instance>` |
| FR-009 | KD-1 canary (`internal/live`) | no new symbol: tests drive the existing collector through `ChainOptions` with the fake API, as `TestObserveCanaryTrap` does |
| FR-010 | `ObjectLock`, `S3Checker.ObjectLock`, `judgeObjectLock` (`internal/live`) | `type ObjectLock struct { Enabled bool; Mode string; Days, Years int }`; `func (c *S3Checker) ObjectLock(ctx context.Context, bucket string) (ObjectLock, error)`; `func judgeObjectLock(l ObjectLock, err error) string` returning `pass`, `fail` or `blocked`; assertion id `state-bucket-object-lock` |
| FR-011 | `Generate`, `CheckStageLocks` (`internal/stacks`) | `Generate(opts GenerateOptions) error` unchanged, refusing with `*GenerateError{Code: "UNRECONCILED_ROW"}`; `func CheckStageLocks(root string) []string` (each finding starts `STAGE_LOCK_MISMATCH`), called by `lz-stacks check` |
| FR-013 | `limits`, `scopeArgv`, `limitsUnavailable`, `checkScope`, `limitTripped` (`cmd/lz-offline`, package main) | `type limits struct { memoryMax int64; tasksMax int }`; `func scopeArgv(systemdRun, unit string, l limits, child []string) []string`; `func limitsUnavailable(lookPath func(string) (string, error), userManager func() error, controllers string) string` (reason or ""); `func checkScope(cgroupDir string, l limits) error`; `func limitTripped(memoryEvents, pidsEvents string) string` (`memory`, `tasks` or "") |
| FR-014 | `Project`, `VerifyArchive`, `Prepare` (`internal/bundle`) | `func Project(archive io.Reader, dest string) error` (link-safe; refuses escaping symlinks and hard links); `func VerifyArchive(path, sha256 string) error`; `type PrepareOptions struct { Root, Toolchain, Crane string; Archives map[string]string }`; `func Prepare(o PrepareOptions) error` (pins from `checks.Packages`) |
| FR-017 | `provenance` (`cmd/lz-pack`, package main) | `type provenanceInput struct { Commit, Manifest, Layer, Resources, Entry, Builder string }`; `func provenance(in provenanceInput) ([]byte, error)` |
| FR-015 | `CheckUpdateLane` (`internal/checks`) | `func CheckUpdateLane(root string) []Finding` (existing `Finding`); rules `UPDATE_UNCOVERED`, `UPDATE_AUTOMERGE`, `UPDATE_RUNTIME_GROUP`, `UPDATE_PROVIDER_GROUP`, `SCHEMA_VERSION`, `SCHEMA_RECORD`, `UPDATE_SYNTAX`; `lz-check -root <dir> update-lane` prints them |
| FR-016 | `Manifest` (`internal/release`) | `func Manifest(repo, commit, version string) ([]byte, error)`; refusals start `RELEASE_VERSION` or `RELEASE_VERSION_TAKEN` |
| FR-018, FR-019 | `lz-check docs` | no new Go symbol: tests run the existing `lz-check -root <fixture> docs` command; new rule ids as in *Docs front matter* |
