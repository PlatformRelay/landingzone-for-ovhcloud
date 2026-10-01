# ADR-0010: Versioning, release and distribution
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0002, ADR-0003, ADR-0004

## Context
Registries expect one repository per module (`terraform-<provider>-<name>`). A monorepo can be consumed
by git source (`git::https://…//modules/x?ref=<tag>`), mirrored to per-module repositories, or published
as OCI artifacts (OpenTofu supports OCI module sources; production maturity **UNVERIFIED**). How
release-please handles per-package component tags and whether the OpenTofu registry accepts monorepo
modules are both **UNVERIFIED**. The commit convention in this workspace is gitmoji + conventional
commits, rebase-merge, no squash.

## Options considered
1. Git-source consumption, per-module tags.
2. Split-mirror to `terraform-ovh-<name>` repos and publish to the OpenTofu/Terraform registry.
3. OCI artifacts per module.
4. One version for the whole repo.

## Decision (proposed)
- **Start with option 1**, designed so that 2 can be added without changing tags: tags are
  `<layer>/<name>/v<semver>` (e.g. `modules/private-network/v1.2.0`, `blueprints/core/v0.3.0`).
  Consumers pin the tag; docs show the `ref=` form.
- **Semver per module, component and blueprint**, plus a repo-wide *release train* tag
  (`release/YYYY.MM`) that records a compatible set. Pre-1.0 modules may break on minor versions and say so.
- **Interface = contract:** `CONTRACT.yaml` lists inputs (name, type, required), outputs, and variable
  validations. `tools/` diffs it on every PR and fails on a breaking change without a major bump.
  Breaking = removed/renamed input or output, type narrowing, new required input, changed default
  that alters resources. Resource address changes require `moved` blocks and a migration note.
- **Commits drive releases:** conventional commit scope = module path; release automation (release-please
  manifest mode or a small in-repo tool — chosen by the spike) computes per-module versions and
  changelogs. Release notes written for humans via the workspace changelog skill, not auto-dumped.
- **Mirror/registry (option 2/3):** decided after the spike, if demand exists. Until then docs state
  "consumed from git".
- Provider constraints use compatible ranges (`~> 2.21`), tested at the lower and upper edge in CI.

## Consequences
- Tag names are long but unambiguous and parseable.
- No registry browsing or `version =` constraints at first; documented trade-off.

## Counterpoints
- Per-module release machinery in a monorepo is the usual place where tooling drifts; a small in-repo
  tool may beat configuring a third-party release bot.
- A release train tag duplicates information; kept because users want one number for "a known-good set".

## Verification
- Spike (cheap): dummy monorepo with 3 modules; try release-please manifest mode with `component` tags;
  consume via `//modules/x?ref=` in OpenTofu; attempt registry publication from a monorepo tag.

## Review log
_(empty)_
