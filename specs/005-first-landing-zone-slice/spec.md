# Feature Specification: First landing-zone slice
Feature: `005-first-landing-zone-slice` · Created: 2026-10-06 · Status: draft
ADRs: 0002, 0003, 0004 (reworded 2026-10-06, Proposed), 0005, 0006, 0007, 0008, 0009, 0017, 0018, 0024
Input: operator decisions D83/D85/D87 (2026-10-06), the 2026-10-06 cost/bootstrap update and
constitution 1.3.0.

## Why and scope
Start writing real OpenTofu. Deliver a working, testable PoC of the ADR-0004 stage chain in the
maintainer sandbox: names and labels, stage modules, Terramate stacks generated from a minimal
`deployments.yaml`, one state per stack, offline tests, and an owner-run live apply→destroy chain.
This is a step towards large, multi-tenant organisations, not the destination: every choice must
leave room for more tenants, environments, regions, runtimes, identity providers, self-service,
GitLab and TACOs (see *Growth seams*).

In: naming module (default pattern), mandatory labels, `modules/` → `components/` → `stages/` →
generated stacks, the stack set below, strict `deployments.yaml` v1alpha1, a minimal reconciler,
S3 state with lockfile and client-side encryption, platform/tenant deployer service accounts, a
host-only live lane, a scripted re-runnable account bootstrap, L0/L1/L2-lite offline tests and one
L7 live chain.

## Stack set (D85: more stacks from the beginning)

| Stage | In/out | What it holds | Authority (runner) | Why |
| --- | --- | --- | --- | --- |
| `account-admin` | in | the `lz-sandbox-admin` OAuth2 service account and its IAM policy | root bootstrap (short-lived AK/AS/CK, fresh account only) or import | Makes the bootstrap re-runnable on a fresh account (2026-10-06 update); ADR-0009 "bootstrap / order" authority |
| `bootstrap` | in | versioned account state bucket and one versioned state bucket per tenant (ADR-0009); platform S3 user, credential and bucket policies; local encrypted state | `lz-sandbox-admin` | State home for every other stack; adding a tenant re-applies it |
| `account-governance` | in | platform-deployer and tenant-deployer service accounts, their IAM policies, a tenant IAM group (no members), tenant S3 user scoped to its tenant's state bucket | `lz-sandbox-admin` | Two credential classes from the first run |
| `account-fabric` | **out** | — | — | Nothing real and cheap is needed: new projects ship with a vRack (`ovhcloud-docs/.../network-services/vrack.mdx:196`), audit sinks need paid LDP. Not in `deployments.yaml`; the stage name stays reserved in the schema enum |
| `project` | in | ADOPT the existing sandbox project (import, `prevent_destroy`, `deletion_protection`), labels via IAM resource tags, optional budget alert, optional quota guard | platform deployer | One project per tenant × environment (ADR-0005) |
| `project-network` | in | one private network and one subnet, no gateway | tenant deployer | Cheapest real network; regional |
| `runtime` | in | `managed-only` runtime: one empty, labelled Object Storage bucket | tenant deployer | Cheapest real runtime (ADR-0017 `managed-only`); consumes `project` only, not `project-network` |
| `observability` | **out** | — | — | LDP is billed and no consumer exists. Not in `deployments.yaml`; name reserved |

## User Scenarios & Testing

### User Story 1 — Names, labels and the first stage, offline (Priority: P1)
A contributor changes the naming template or the bootstrap stage and gets green or red feedback
with no cloud access. Given the default template and a reordered template, names are stable and
every taggable resource carries the mandatory labels; an over-long name, a forbidden character or
an override of a mandatory label key is rejected.
**Independent test**: V001, V002 on `modules/naming`, `modules/object-storage*`,
`components/state-backend`, `stages/bootstrap`; V003.

### User Story 2 — Every in-scope stage planned offline with typed output contracts (Priority: P1)
A contributor runs the full offline suite and sees each stage plan under mocked providers, publish
schema-valid outputs and refuse unsafe input (an IAM-writing tenant policy, an unprotected adopted
project, a secret in a published output).
**Independent test**: V002 for every slice directory, V003, V004.

### User Story 3 — Stacks generated from `deployments.yaml` and planned offline (Priority: P2)
A platform maintainer adds a tenant × environment × region row and gets new stack directories with
generated backend, provider, module call and labels, ordered by real dependencies, each planning
offline against fixture inputs. A stale generated file, an orphan directory or a changed
`instance_id` fails the freshness check.
**Independent test**: V005, V006 (including the two-tenant growth fixture).

### User Story 4 — Scripted, re-runnable account bootstrap (Priority: P2)
The maintainer bootstraps (or re-bootstraps) an OVHcloud account with one command: admin service
account, passphrase file, state bucket and local credential files. A second run changes nothing; a
fresh account follows the same command, which makes it the account-migration path.
**Independent test**: V008 offline; V009 owner session.

### User Story 5 — Live chain apply→destroy in the sandbox (Owner session, Priority: P3)
The maintainer runs the chain from the workstation; it applies in dependency order with the right
credential per stack, asserts real behaviour, destroys ephemeral stacks in reverse order even on
failure, checks for leftovers and reports what to record as cost.
**Independent test**: V007 offline; V010 owner session.

### Edge cases
Empty `deployments.yaml`; duplicate `instance_id`; two rows mapping to one path or state key;
region given for an environment-scoped stage; a stage not implemented in this slice; producer has
no published outputs yet (consumer plan refuses, no placeholder); consumed outputs changed since
the consumer's last apply; interrupted apply (SIGINT) mid-chain; destroy failure of one stack
(others still attempted, exit non-zero); leftover check cannot list a kind (fail, not pass);
`ovhcloud` missing; credentials file missing or world-readable; passphrase file exists (never
overwritten); bucket name taken globally; import of the project would replace it; the run happens
inside `lz-offline` (refused); two runs for one tenant at once (second refused).

## Premises

Evidence column: *doc* = primary source in the local KB, not observed; *UNVERIFIED* = no source or
not observed. Probes run in T007 (offline captures) and T009/T010 (owner session).

| ID | Premise | Source | Probe | Status |
| --- | --- | --- | --- | --- |
| P1 | OVH Object Storage supports the OpenTofu 1.13 S3 backend with `use_lockfile = true` (conditional writes) | `ovhcloud-docs/.../storage-and-backup/object-storage/s3-conditional-writes.mdx:11,85,94`; `.../hosted-private-cloud/cloud-platform/snc-cloud-platform-terraform.mdx:302-303` (Terraform, not OpenTofu) | T010: two concurrent `tofu plan` on one key; second refused with a lock error; lock object gone after | UNVERIFIED |
| P2 | Backend options needed for OVH (`endpoints.s3`, `skip_credentials_validation`, `skip_region_validation`, `skip_requesting_account_id`, `skip_s3_checksum`) work with OpenTofu 1.13 | `.../public-cloud/compute/use-object-storage-terraform-backend-state.mdx:109-125` (Terraform) | T010 init/plan/apply on a probe key | UNVERIFIED |
| P3 | Client-side state encryption (PBKDF2 passphrase, `encryption {}`) works with the S3 backend and lockfile on OVH; conditional writes accept the encrypted object | ADR-0009 context (2026-10-01 note); conditional writes need unencrypted or SSE-S3 objects server-side (`s3-conditional-writes.mdx:85`), client-side encryption is opaque to the server | T010: downloaded state object is an encryption envelope, not plaintext JSON; lock still works | UNVERIFIED |
| P4 | OpenTofu 1.13 evaluates variables in `encryption` and `backend` blocks early (passphrase from `TF_VAR_`, local state path outside the repo) | none in KB | T007 offline capture with local backend | UNVERIFIED |
| P5 | Importing the existing sandbox project into `ovh_cloud_project` (required `ovh_subsidiary`, `plan`) yields no replacement; `prevent_destroy` + `deletion_protection` hold | `terraform-provider-ovh/docs/resources/cloud_project.md` (Import, `deletion_protection`) | T009: plan-only with an import block and `-generate-config-out`; no apply | UNVERIFIED; fallback: data source + `ovh_iam_resource_tags` (research R7) |
| P6 | An `import` block in a generated root can target a nested module address | none in KB | T007 offline (mock) and T009 plan | UNVERIFIED |
| P7 | `lz-sandbox-admin` (policy `account:apiovh:iam/*`, `account:apiovh:me/*`, `publicCloudProject:apiovh:*`, AGENTS.md) may create OAuth2 clients and IAM policies | `me_api_oauth2_client.md`, `iam_policy.md` | T010: create and delete a probe client and policy | UNVERIFIED |
| P8 | An OAuth2 service account is usable as an IAM policy identity via its `identity` URN | `me_api_oauth2_client.md` ("Identity URN … to be used inside an IAM policy") | T010 probe policy grants one read; call succeeds, a non-granted call is denied | doc; UNVERIFIED live |
| P9 | Tenant-deployer allowlist from `api/v1/cloud.json` (`publicCloudProject:apiovh:network/private/*`, `…/network/private/subnet/*`, `…/region/storage/*`, plus reads the provider needs) is sufficient and denies IAM writes | `kb/api/v1/cloud.json` action names | V010 chain (positive) and negative IAM write | UNVERIFIED |
| P10 | `ovh_cloud_project_alerting` works on the trial project | `cloud_project_alerting.md` | T009 plan; T010 apply+destroy | UNVERIFIED; optional |
| P11 | `ovh_cloud_quota` `prevent_automatic_quota_upgrade` can be set on the sandbox project | `cloud_quota.md`; `api/v2/publicCloud.json:5530` | T009 plan only | UNVERIFIED; optional, default off |
| P12 | A private network + subnet without gateway or instances creates without explicit vRack management and costs nothing measurable | vRack free and auto-delivered with new projects (`.../network-services/vrack.mdx:196,201`); network price: none | T010 create/destroy in GRA11 | UNVERIFIED (cost, vRack precondition) |
| P13 | An empty or KB-sized Object Storage bucket costs ~0 (order of 0.01 EUR/GB/month) | indirect: `.../ai-machine-learning/ai-notebooks-billing.mdx:116` | Control Panel after V010 | UNVERIFIED |
| P14 | Destroying `ovh_cloud_project_storage` removes objects first; noncurrent versions too | `cloud_project_storage.md` (warning: "will try to remove all objects") | T010 versioned probe bucket with two versions | UNVERIFIED (versions) |
| P15 | Tag keys with `:` (e.g. `lz:tenant`) are accepted on IAM resource tags and S3 bucket tags | IAM: `iam_resource_tags.md` key pattern `^[a-zA-Z0-9_.:/=+@-]{1,128}$`; bucket tags: none | T010 | doc (IAM); UNVERIFIED (bucket) |
| P16 | `lifecycle { ignore_changes = [tags["lz:run-id"]] }` keeps the creating run id stable on later applies | OpenTofu semantics, none in KB | T007 offline (tofu test apply against mock twice) | UNVERIFIED |
| P17 | Terramate 0.17.3: `terramate create --id --tags --after`, `generate_hcl`, stack `after` with tag filters, `terramate list --run-order` | none in KB | T007 offline captures from the pinned binary | UNVERIFIED |
| P18 | `ovhcloud` CLI 0.15.0 lists buckets, private networks, OAuth2 clients and IAM policies in machine-readable form | AGENTS.md (install line only) | T009 capture with read-only calls | UNVERIFIED |
| P19 | (withdrawn) An S3 user policy can scope access to a key prefix of one bucket | `cloud_project_user_s3_policy.md` (bucket-level ARNs shown) | — | not needed (D87: one state bucket per tenant; tenant S3 users are scoped by bucket ARN) |
| P20 | Features used exist in the pinned provider `ovh/ovh` 2.21.0, though the KB docs are 2.22.0-era | `terraform-provider-ovh/CHANGELOG.md:1,31` (`discard_client_secret` is 2.22.0, line 15) | L0 `tofu validate` against the mirrored 2.21.0 provider | doc; `discard_client_secret` excluded |
| P21 | Bucket names are unique across OVHcloud | `.../object-storage/s3-limitations.mdx:49-53` | — | doc; template carries an org discriminator |
| P22 | A fresh trial account has, or lets the owner create in the Control Panel, one Public Cloud project | none | T045 | UNVERIFIED; project ordering stays manual |

A refuted premise blocks the clauses that rely on it and is recorded in `research.md` with the
fallback taken; it never turns into a silent pass.

## Requirements

### Functional requirements
- **FR-001 Naming** — `modules/naming` is pure (no provider, no data source). Inputs: hierarchy
  coordinates (`org`, `tenant`, `environment`, `region`, `kind`, `role`) and a template object passed
  as data. Only the default template ships; a second template exists in tests to prove the template
  is data. Names are deterministic, independent of labels, validated against per-kind limits for
  the kinds this slice uses, and never silently truncated. An explicit name override (import) is
  passed through unchanged after validation. Supersedes 001/T013–T014 for this scope.
- **FR-002 Labels** — every resource whose API carries tags gets `managed-by=opentofu`,
  `managed-in=<forge>/<org>/<repo>//<stack path>`, `instance=<instance_id>`, `tenant=<tenant>` (empty
  key absent for account scope) and `release` (the constant `unreleased` until ADR-0010's release
  train exists, D87) under the `lz:` namespace; extra labels
  may not override these keys; `tenant` comes only from the platform manifest. Resources without
  tags are listed in an applicability table and recorded in the stack's published outputs. Live runs
  add `lz:run-id` at creation and never update it.
- **FR-003 Layering** — `modules/` (provider-thin) → `components/` → `stages/` → generated stacks.
  Modules, components and stages declare no backend, no provider configuration and never use
  `terraform_remote_state`; only generated stacks configure backend and provider. The dependency
  checker classifies every new directory and rejects any other edge; its ADR-0002 matrix is not
  widened.
- **FR-004 Stages** — the in-scope stages in *Stack set* exist as child modules with typed inputs
  and outputs. The adopted project is never destroyed by OpenTofu. Budget alert and quota guard are
  optional inputs. Tenant-deployer IAM policies grant only the listed project-scoped actions on the
  tenant's project URN and no `account:apiovh:iam/*` action.
- **FR-005 Output contracts** — each stack publishes `outputs.json`: an envelope (`apiVersion`,
  `kind: StageOutputs`, `instance_id`, `stage`, `source_revision`, `values`) validated against
  `schemas/outputs/<stage>.schema.json`. Sensitive outputs are never published. A capability that does
  not exist is absent, never a placeholder. Consumers receive producer values only as typed
  variables from these files.
- **FR-006 Manifest** — `stacks/deployments.yaml`, `apiVersion: lz.platformrelay.dev/v1alpha1`,
  `kind: Deployments`, strict decoding (unknown field, duplicate key, unsupported version rejected).
  Dimensions tenant × environment × region; `instance_id` is immutable, unique and equals the
  Terramate stack id; stage scope rules (account / environment / region) are enforced; dependency
  edges are derived from the stage table, not authored.
- **FR-007 Reconciler and generation** — `task stacks:reconcile` creates a missing stack with
  `terramate create`; any other mismatch (directory without row, changed id, changed dimension) fails
  with `UNSUPPORTED_CHANGE` (retirement, rename and tombstones postponed; the seam is this error).
  Generation renders backend, provider, the one stage call, typed input variables, labels and (adopt
  only) the import block. `task stacks:check` fails on any stale generated file.
- **FR-008 State** — `account-admin` and `bootstrap` use local state outside the repository; every
  other stack uses the S3 backend with `use_lockfile = true`, its own key and `encryption {}`: account
  stacks in the account state bucket, tenant stacks in their tenant's own state bucket (one bucket per
  tenant from day one, ADR-0009, D87; both created by `bootstrap`). Each stack's
  `artifacts/<instance_id>/outputs.json` lives in the same bucket as its state. The `encryption {}`
  block uses a PBKDF2 passphrase from `~/.config/ovh-lz/` (never committed). Every
  generated backend carries `encryption {}`; OKMS and escrow are postponed.
- **FR-009 Order and runs** — Terramate `after` follows derived edges; producers run before
  consumers; a consumer whose consumed `outputs.json` digest differs from the one recorded at its
  last apply (or has no record) is re-planned; one run at a time per tenant.
- **FR-010 Credentials** — each instance names its authority (`root-bootstrap`, `bootstrap`,
  `platform`, `tenant`); the live lane loads only that authority's credentials and refuses a
  mismatch. Secret values never reach stdout, stderr, logs, `outputs.json`, the repository or a
  worktree; credential files are mode 600 under `~/.config/ovh-lz/`. Root AK/AS/CK are accepted only
  by the fresh-account bootstrap phase, which ends with a revocation step.
- **FR-011 Live lane** — `task live:plan|apply|destroy|chain -- <instance|all>` runs on the
  maintainer host only, from the owner's main checkout on a reviewed commit (D87; refused inside
  `lz-offline`, from an agent worktree or without credentials). `chain` applies, asserts,
  then destroys ephemeral instances (`runtime`, `project-network`, `account-governance`) in reverse
  dependency order from a trap that also fires on failure and interruption; `account-admin`,
  `bootstrap` and `project` are retained. It records an inventory of created ids, runs a leftover check
  with `ovhcloud` by name prefix and `lz:run-id`, and prints the run id plus a reminder to record the
  approximate cost in the PR. Cost guards are hygiene: no spend gate, no cost ledger; the budget alert
  is optional.
- **FR-012 Re-runnable bootstrap** — `task bootstrap:account` brings an account from empty (or any
  partial state) to: admin service account and `sandbox.env`; passphrase file (created once, never
  overwritten); account and tenant state buckets and `state.env`; tenant and platform deployer files
  after `account-governance`. Each step detects completed work; a re-run on a bootstrapped account
  changes nothing. The same command on a fresh account is the account-migration path. Bucket names
  are global: a taken name fails with a clear error that names `spec.org` (default `lz`, D87) as the
  override.
- **FR-013 Tests** — L0 (`task lint`) and L1 (`tofu test` with `mock_provider`) for every module,
  component and stage; L2-lite plan assertions per generated stack against fixture inputs; one L7
  live chain. Mutation proof is required only for security guards: credential selection and secret
  redaction, tenant isolation (tenant label, tenant policy scope, tenant state-bucket scope), authority
  (adopted-project protection, IAM-write denial), cleanup (trap, retained set, leftover check).
- **FR-014 Traceability** — `task check:specs -- specs/005-first-landing-zone-slice` traces every
  requirement and task of this spec; requirement ids do not collide with spec 001's.

### Key entities
Deployment manifest, deployment instance, stage, stack, output envelope, label set, naming
template, authority, credential file, live run record. See `data-model.md`.

## Success criteria
- **SC-001**: From a clean checkout, `task test:slice` passes in `lz-offline` with no network and no
  credentials; every module, component and stage directory has at least one L1 test (V002).
- **SC-002**: The sandbox manifest (1 tenant × 1 environment × 1 region) yields 6 stacks; the growth
  fixture (2 tenants × 2 environments × 2 regions) yields unique ids, paths and state keys, one state
  bucket per tenant, and plans
  offline with no stage code change (V005, V006).
- **SC-003**: One owner session runs `task live:chain` to completion in under 60 minutes; the
  leftover check reports zero ephemeral leftovers; run id and approximate cost are in the PR (V010).
- **SC-004**: A second `task bootstrap:account` on the bootstrapped sandbox reports no change; on a
  fresh account the same command yields a working state backend (V009; fresh part blocked until an
  account exists).
- **SC-005**: A seeded secret never appears in any captured output, log, `outputs.json` or
  committed file; each security guard of FR-013 has a killed mutant (V004, V007, V008).

## Acceptance and predefined verification
All commands are **planned**; creators are in `tasks.md`; evidence starts `not-run`. Offline
targets run through `lz-offline`; `live:*` and `bootstrap:account` run on the host by the owner.
Details and controls: `contracts/checks.md`.

| Check | Requirements | Verify (planned) |
| --- | --- | --- |
| V001 | FR-001, FR-002, SC-001 | `task test:unit -- modules/naming; task lint -- modules/naming` |
| V002 | FR-003, FR-004, FR-013, SC-001 | `task test:slice` |
| V003 | FR-003 | `task test:dependencies` |
| V004 | FR-005, SC-005 | `task test:outputs` |
| V005 | FR-006, FR-007, FR-008, SC-002 | `task test:stacks; task stacks:check` |
| V006 | FR-009, FR-013, SC-002 | `task test:stack-plans; task stacks:order -- all` |
| V007 | FR-010, FR-011, SC-005 | `task test:live-lane` |
| V008 | FR-010, FR-012, SC-005 | `task test:bootstrap` |
| V009 | FR-008, FR-012, SC-004 | Owner session: `task bootstrap:account` twice; fresh-account procedure |
| V010 | FR-004, FR-008, FR-009, FR-010, FR-011, SC-003 | Owner session: `task live:chain -- all` |
| V011 | FR-014 | `task check:specs -- specs/005-first-landing-zone-slice` |

## Growth seams (nothing here may block later growth)
- More tenants/environments/regions: rows in `deployments.yaml`; paths and keys are derived.
- Tenant repo: `stacks/` plus `terramate.tm.hcl` lift into a tenant repository unchanged.
- Runtimes and identity providers: `components/runtime/<kind>`, `components/identity/<plane>-<source>`
  behind the same stage outputs; the stage selects the variant at one point.
- Rename/retirement: `UNSUPPORTED_CHANGE` is where tombstones plug in.
- Artefact transaction: `outputs.json` envelope + recorded consumed digests are the inputs that
  generations, fencing and waves will extend.
- OKMS, escrow: generated backend already carries `encryption {}` and a per-stack key; state buckets
  are per tenant from day one.
- GitLab/TACOs: stacks are vanilla OpenTofu roots; the live lane is a Taskfile contract.

## Out of scope / postponed (with trigger)
| Item | Seam kept | Trigger to start |
| --- | --- | --- |
| Artefact transaction (generations, fencing, waves; spec 002) | output envelope, consumed-digest record | second consumer of one producer in CI, or concurrent tenant runs |
| Tombstones, retirement, rename | `UNSUPPORTED_CHANGE` | first instance removal |
| OKMS state key, escrow, backup | `encryption {}` in every backend | first non-sandbox tenant or a second maintainer |
| GitLab | Taskfile contract | operator request or a consumer on GitLab |
| TACOs | vanilla roots | first TACO user |
| Assent / self-service | manifest is platform-controlled | first tenant author who is not the maintainer |
| Scanner (`lz-audit`), generated docs, compliance profiles | labels, outputs, applicability table | first reference environment |
| Guided preconfiguration wizard (spec 004) | `deployments.yaml` schema | real profile schemas (D29) |
| `account-fabric`, `observability` stacks | reserved stage names | a consumer that needs vRack/audit sinks or logs |
| Cost ledger, spend admission, reaper (spec 003 T002–T008) | inventory + leftover check | spend outside the trial or shared sandbox |
| Profiles/golden paths, catalogue | stage variant selection point | second runtime variant |
| Pipeline-run live lanes (ADR-0019) | host-only `live:*` | protected-branch CI with sandbox credentials |
| Full ADR-0019 AgentEx scope (deferred, D87; constitution VI; 001/T017–T019) | existing `lz-check` diagnostics | the second vertical slice, or agents repeatedly misreading check output |

## Dependencies and stop conditions
Depends on 001/T001–T009 and T023 (done). Live tasks additionally need the owner and the sandbox
credentials in `~/.config/ovh-lz/`, and start only from the owner's main checkout on a reviewed
commit. Constitution 1.3.0 (D87) settles the earlier Principle V (cost guards are hygiene) and
Principle VI (AgentEx deferred) conflicts. A refuted premise stops its dependent tasks and records
the fallback.
