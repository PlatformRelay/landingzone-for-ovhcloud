# ADR-0008: Testing strategy — every `tofu test` feature, and beyond
- Status: Proposed (rewritten 2026-10-01 after brainstorm round 2)
- Date: 2026-10-01
- Related: ADR-0005, ADR-0006, ADR-0007, ADR-0010, ADR-0016, ADR-0017

## Context
Requirement: "very extensive testing of everything" and "very heavy use of all the tofu testing
features we can use and features beyond that". The platform bills by the hour with no hard cap, project
creation is a slow billed order, and one human plus agents maintain the repo, so extent must be bought
with cheap layers and the gates themselves must be tested.
Verified against opentofu.org and the OpenTofu changelog on 2026-10-01: OpenTofu 1.13.0 (2026-09-30)
offers `run` blocks with `command = plan | apply`, `assert`, file- and run-level `variables`, the
`module` override, test-scoped `providers` and aliases, `mock_provider` with `mock_resource` /
`mock_data`, `override_resource` / `override_data` / `override_module` (**instance keys and `[*]`
wildcards new in 1.13**), `expect_failures`, `plan_options { mode, refresh, replace, target }`,
cross-run references (`run.<name>.<output>`, after `apply` runs), `-filter`, `-test-directory`,
`-var(-file)`, `-json`, `-json-into`, `-verbose`; `*.tofutest.hcl` overrides `*.tftest.hcl` by base
name. 1.14 (unreleased) adds `source` on `mock_provider`. OpenTofu has **no `-junit-xml`**; convert
from `-json`. Parallel `run` blocks: not available (parallelise at the CI matrix level).
The go-ovh client accepts an arbitrary endpoint URL, which makes a record/replay proxy possible.

## Options considered
Native `tofu test` only; Terratest only; a layered taxonomy where ~80 % of tests cost nothing and the
sandbox pays only for what mocks cannot prove.

## Decision (proposed)
The layered taxonomy. Every layer has a Taskfile target (ADR-0007), a stated cost, and a cadence.

| # | Layer | Mechanism | Catches | Cost | Cadence |
|---|---|---|---|---|---|
| L0 | Static | `tofu fmt -check`, `tofu validate`, tflint (+ custom naming/tag rules), trivy config, terraform-docs `--output-check`, JSON-Schema validation of profiles/tenants/matrix, `assent lint`, dependency-direction script, markdown/link lint | syntax, types, undocumented inputs, layering violations, schema drift | 0 | pre-commit, PR |
| L1 | Unit | `tofu test` `command = plan` with `mock_provider` (+ `mock_resource` defaults for computed attributes), `override_data` for regions/flavours, `expect_failures` for every `validation`/`precondition`, run-scoped `variables` for edge cases, table-driven runs generated from `names.yaml` for the naming module | wiring, conditionals, validation rules, naming function | 0 | PR (changed dirs), pre-commit for the changed module |
| L2 | Contract | shared `*-contract.tftest.hcl` run against every variant of a family with `override_module` (wildcards for `for_each`); `can()`/`regex` on every output field; `CONTRACT.yaml` diff (ADR-0010); **inclusion tests** between profiles (ADR-0016) | interface breaks, forked golden paths, breaking changes without a major bump | 0 | PR |
| L3 | Snapshot | normalised plan JSON per example and per profile, committed; diff on PR; `task snap:update` regenerates and the diff must be in the PR | unintended plan changes from refactors and provider bumps | 0 | PR |
| L4 | Policy | Conftest tests for every Rego rule; `assent test` fixtures for every assent policy; **mutation harness** (flip comparators, drop a rule, widen a regex) with a kill-rate threshold; guardrail-id reference test (ADR-0006) | dead policies, gates that pass everything, rules with no enforcing plane | 0 | PR; mutation nightly |
| L5 | Live plan | `tofu test` `command = plan` with the real `ovh` provider and a **read-only** service account (`providers = { ovh = ovh.live }` in those runs only; selected with `-filter`), across a region matrix | auth/API breakage, data-source regressions, product-per-region availability | API calls only | PR label `live-plan`, weekly, release |
| L6 | Replay | the same runs against a **record/replay proxy** (cassettes of real API traffic; the provider's endpoint pointed at the proxy); cassettes refreshed from the sandbox nightly | provider behaviour at zero cost, deterministic | 0 after recording | PR, if the spike passes |
| L7 | Apply | `tofu test` `command = apply` chains (`run.project` → `run.network` uses `run.project.id`; cleanup in reverse order) on module examples in the sandbox; Go/terratest only for behaviour HCL cannot assert (HTTP/TCP probes, OIDC login, S3 PUT) | real resource behaviour, eventual consistency, provider bugs | billed, minutes | nightly **rotation** (a quarter of modules per night), all on release |
| L8 | Golden-path e2e | apply one profile from zero via the tenant-repo template, probe, destroy; timing recorded as a metric | composition errors, bootstrap ordering | billed, ~1 h | nightly rotation of profiles; all before release |
| L9 | Drift and conformance | `tofu plan -detailed-exitcode` on a long-lived reference environment; `lz-audit` scan; **upgrade test** (apply release N, plan N+1: zero destroys of protected resources) | manual changes, provider default changes, breaking upgrades | API only | nightly; upgrade test on release candidates |
| L10 | Drills and canaries | DR restore of state from a replica; chaos (delete a gateway by hand, expect a finding and an additive repair plan); break-glass login alert; credential rotation; **provider canary**: Renovate bumps of `ovh/ovh` run L3 + L5 + an L7 subset before merge, with a `tofu providers schema -json` snapshot diff | recovery procedures that rot, alerting that never fired, provider breaking changes | billed, scheduled | quarterly; canary on every bump |

**Test-the-tests.** Nightly **module mutation**: five canned HCL mutations per module (drop a tag,
swap a default, delete a `validation`, set a `count` to 0, break an output) must each be caught by
L1–L3; a survivor opens an "untested behaviour" issue. Policy mutation (L4) uses the same harness.

**Zero-cost contributor mode.** L0–L6 run without an OVH account; L5 needs read-only credentials
only on the maintainers' lane; forks cannot reach L7+.

**Sandbox safety (mandatory, not optional).** Dedicated projects `lz-sandbox` (apply tests) and
`lz-sandbox-ref` (drift); every resource tagged `lz_test`, `lz_run`, `lz_ttl`; an hourly **reaper**
destroys past-TTL resources and files an issue; a **budget guard** refuses L7+ when month-to-date
spend exceeds the cap or the last reaper run found orphans (fail-closed); the smallest flavours and
one cheap region; a **pre-ordered project pool** instead of ordering per test; spend published weekly
to `docs/reference/test-costs.md`. Cap: operator decision (INBOX).

**Reporting.** Every layer emits `-json-into`; `tools/json2junit` renders it for both forges;
coverage is a module × layer table in CI, not a percentage.

**Coverage rule.** Every module: L0, L1, L2, L3. Every family variant: L2 contract. Every profile: L3,
L8, inclusion test. Every policy: L4 with fixtures and mutants. Every how-to: its ending `task`
target runs in dry-run nightly (ADR-0013).

**Mocking limits.** `mock_provider` masks provider-side validation; L5/L6/L7 exist for that reason;
mock defaults are copied from recorded real values (`tests/fixtures/`), refreshed by a task.

## Consequences
- Contributors get fast, free feedback; the sandbox bill is bounded and visible.
- Snapshots, mutation reports and freshness gates produce noise; the pre-mortem says this is where a
  solo maintainer disables gates. Mitigation: thresholds are tuned by data in the first month, and a
  gate may be loosened only with a written justification (workspace rule).
- OpenTofu 1.13 becomes the minimum (wildcard overrides).

## Counterpoints (kept even if overruled)
- Record/replay is a second piece of infrastructure; kept as a spike, not a dependency.
- Terratest everywhere would be one language for everything; rejected because `tofu test` is free,
  provider-aware and the operator asked for it; Go is used only where HCL cannot assert.

## Verification
- Spike: mock fidelity — L1 for one module, then L5 and L7; count defects only L7 finds.
- Spike: replay proxy — provider with `endpoint` set to a local recorder; record one apply, replay the
  plan offline; pass if the plan is byte-identical after normalisation.
- Spike: `override_module` wildcards (1.13.0) in an inclusion test.
- Spike: one week of the nightly rotation; record cost against the cap; reaper leaves nothing.
- Spike: `tofu test -json` → JUnit rendered in GitHub and GitLab.

## Review log
- 2026-10-01 rewrite: L0–L10, every `tofu test` feature mapped, mutation, canary, replay spike,
  fail-closed budget guard. Source: agent-context/research/BRAINSTORM-2026-10-01-round2.md.
