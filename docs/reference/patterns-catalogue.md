# Patterns catalogue — small patterns, guidelines, tools and techniques

Status: draft, 2026-10-01. Harvested from the two brainstorm rounds, three blind designs, one
external blind design and the ADRs. Each entry names the check that enforces it (ADR-0019); an
entry marked **review** has no check yet — review is a legitimate enforcement method, not a gap.
Ids are provisional until the rule is implemented. Rules are not enforced mechanically as universal
bans: each has an applicability (artefact kind), explicit exceptions with a justification line, and
an evidence grade; behaviour and security rules outrank cosmetic ones, and a rule whose checker
can be satisfied without the intended behaviour is a defect in the rule.

## 1. HCL idioms and module interface
| Id | Pattern | Why | Enforced by |
|---|---|---|---|
| LZ-HCL-001 | Primary resource label is `this`; others are role nouns; never `main`, never the type repeated | Readable addresses, stable `moved` blocks | tflint rule |
| LZ-HCL-002 | `for_each` keys are stable logical ids from the tenant model, never generated names or indexes | Renames must not recreate resources | tflint rule + snapshot test |
| LZ-HCL-003 | Inputs are typed objects with `validation` and a description that states replacement implications | Agents and reviewers see blast radius | terraform-docs drift + review |
| LZ-HCL-004 | No `any`, no blanket `ignore_changes`, no `null_resource` sleeps, no provisioner API calls | Hidden behaviour defeats tests | tflint rule |
| LZ-HCL-005 | Child modules declare `required_providers` but never configure credentials or backends; roots configure providers and aliases | Reusable across roots and TACOs | tflint rule |
| LZ-HCL-006 | One cloud object has one owning resource address in one state; cross-provider reads are data sources only | No competing managers | review + scanner |
| LZ-HCL-007 | Outputs are a small documented API, never whole resource objects; family outputs match the contract schema | Contracts stay stable | contract test (L2) |
| LZ-HCL-008 | Lifecycle protection where useful, **plus** an independent decommission gate (ADR-0005) that stays effective when the resource configuration is removed; `prevent_destroy` alone is not a deletion-authorisation boundary | Safe by default | policy rule + decommission gate |
| LZ-HCL-009 | Provider-specific behaviour (e.g. `openstack` for strict security groups) is isolated in one module with a README reason | Gap register stays honest | dependency script |
| LZ-HCL-010 | Every optional feature is a leaf; nothing mandatory depends on an optional | Profiles compose | dependency script |
| LZ-HCL-011 | `enabled` meta-argument (OpenTofu 1.11+) behind a variable; no `count = var.enabled ? 1 : 0` | Readability, fewer index addresses | tflint rule |
| LZ-HCL-012 | Ephemeral resources and write-only attributes for secrets wherever the provider allows; plan JSON cannot prove absence of secret persistence, so state encryption remains the backstop | Less in state in cleartext | policy rule on plan JSON + review |
| LZ-HCL-013 | No routine `-target` as a deployment decomposition strategy; a targeted recovery run is followed by a full-plan reconciliation | State boundaries are explicit (ADR-0004) | pipeline lint |

## 2. Naming and tagging
| Id | Pattern | Why | Enforced by |
|---|---|---|---|
| LZ-NAM-001 | All names come from `modules/naming`; no module builds its own | One convention | tflint rule |
| LZ-NAM-002 | `names.yaml` is the single source for patterns, limits and docs; generated artefacts must be fresh | Gate and module never disagree | generator freshness check |
| LZ-NAM-003 | Profiles and lifecycle metadata go in tags, never in names; names are immutable | Profile changes must not rename | naming tests |
| LZ-NAM-004 | Name override input for imported resources; never auto-rename on algorithm change; version the algorithm | Brownfield safety | naming tests + review |
| LZ-NAM-005 | Organisation-structure label keys come from the per-org `labels.yaml` schema (required/optional, allowed values, hierarchy level); unknown keys are rejected at merge, plan and scan | Adapts to org structures without code change | schema + Rego + scanner |
| LZ-NAM-007 | Every labellable resource carries `managed-by=opentofu`, `managed-in=<forge>/<org>/<repo>//<path>`, `instance=<deployment instance id>`, `release=<train>`; Kubernetes objects carry them as labels, long values as annotations | Console triage, one writer per object, state-owner map | Rego on plan + scanner |
| LZ-NAM-008 | Keys the IAM plane conditions on are marked `authorisation: true` and are set only by platform roots, never from a tenant file | A tenant must not be able to re-label itself into another envelope | schema + assent + Rego |
| LZ-NAM-009 | The naming template is per-organisation data: segment order, separator, case, abbreviations, per-kind overrides, deterministic truncation; an impossible template fails at plan time | Flexible convention, one implementation | naming module validation |
| LZ-NAM-006 | Per-resource length and charset limits carry a source URL per row; unknown limits are marked, not guessed | No invented universal limit | schema |

## 3. `tofu test` patterns (ADR-0008)
| Id | Pattern | Why | Enforced by |
|---|---|---|---|
| LZ-TST-001 | Every module has `tests/unit.tftest.hcl` with explicit `command = plan`; `mock_provider` where the module uses a provider (a pure naming module needs none) | Zero-cost coverage | `task dod` |
| LZ-TST-002 | Every `validation`/`precondition` has an `expect_failures` run | Validations are tested, not decorative | mutation harness |
| LZ-TST-003 | `override_data` pins regions and flavours so unit tests never call the API | Determinism | review |
| LZ-TST-004 | Run names express intent (`rejects_vlan_out_of_range`); `error_message` states expected vs seen | Agent-readable failures | lint on test files |
| LZ-TST-005 | Mock defaults are copied from recorded real values in `tests/fixtures/`, refreshed by a task | Mocks drift otherwise | fixture freshness check |
| LZ-TST-006 | Mock and live runs live in **separate files and separately discoverable targets** (`tests/live/` is a protected root); `-filter` selects files, not runs; offline execution has no credentials and cannot reach cloud endpoints | Offline mode is isolation, not a label | discovery layout + boundary test |
| LZ-TST-007 | Apply chains pass ids via `run.<name>.<output>`; cleanup relies on reverse order; no manual destroy steps | Framework-managed cleanup | review |
| LZ-TST-008 | `override_module` with wildcards for composition and inclusion tests; never apply leaves to test a stage | Cost | review |
| LZ-TST-009 | `-json-into` on every layer; JUnit via `tools/json2junit` | Both forges render results | Taskfile |
| LZ-TST-010 | Tests assert effective behaviour (denied call, failed probe, finding emitted), not "variable equals input" | Evidence, not theatre | review checklist |
| LZ-TST-011 | Capture the denial reason in negative tests so an outage is not read as isolation | Correct evidence | review |
| LZ-TST-012 | Plan snapshots normalised (sorted keys, timestamps and random ids stripped); `task snap:update` diff must be in the PR | Reviewable intent | snapshot tool |
| LZ-TST-013 | Defect-specific module mutants nightly, deduplicated by behavioural failure, equivalent mutants classified; survivors open an issue | Test-the-tests without noise | mutation harness |
| LZ-TST-014 | Never override the subject under test; assert the implementation's real outputs; `override_*` only for dependencies outside the asserted boundary; wiring tests are separate | A substituted subject proves nothing | review + test lint |
| LZ-TST-015 | Every isolation or denial probe has a positive control and records the denial reason; a timeout is not a denial | Correct evidence | probe helpers |

## 4. Policies (ADR-0005, ADR-0006)
| Id | Pattern | Why | Enforced by |
|---|---|---|---|
| LZ-POL-001 | Every rule has a stable id, severity, enforcing planes, a doc page and ≥1 fixture per plane | Honesty page is generated | id-reference test |
| LZ-POL-002 | Unknown critical plan values block; never count as compliant | OPA unknowns are a known gap | Rego helper + test |
| LZ-POL-003 | Waivers carry rule id, scope, reason, approver, expiry; expiry blocks new violating changes, never destroys | Safe exceptions | assent policy + scanner |
| LZ-POL-004 | Mutants per policy (flip comparator, drop rule, widen regex) with a kill-rate threshold | Dead policies | mutation harness |
| LZ-POL-005 | One control may have several projections under the same id: assent evaluates merge authority on data, Rego evaluates deployment effects on the plan; shared fixtures and differential cases prove they agree; contradictory semantics are forbidden, repeated defence is not | Two languages, one control | id-reference test + differential fixtures |
| LZ-POL-007 | Strict YAML: duplicate keys, unknown fields and unsupported `apiVersion` are rejected | Gate and deployer must see the same document | schema |
| LZ-POL-008 | A canonical effective deployment document with a digest is what the merge gate, the plan and the plan policy evaluate | Same resolved defaults everywhere | renderer + digest check |
| LZ-POL-009 | Policy, CI, schema, owner and deployment-manifest changes never travel through the routine auto-merge lane | A request cannot redefine its own authority (ADR-0021) | assent policy + protected paths |
| LZ-POL-006 | Commercial safety: never auto-retry an order or a destroy; reconcile first; data sources that create carts are not "read-only" | Billed platform | guardrail ids + tests |

## 5. Pipelines, CI and supply chain (ADR-0007)
| Id | Pattern | Why | Enforced by |
|---|---|---|---|
| LZ-CI-001 | Duplicated business logic stays out of forge adapters; forge-native authorisation and artefact controls stay explicit and are tested, with no line-count rule | Portability without hiding security logic | review + forge fixtures |
| LZ-CI-002 | Forks get no credentials, ever; L5+ only on maintainer branches; no `pull_request_target` checkout of fork code | Provider code executes during plan | workflow lint |
| LZ-CI-003 | Unattended apply executes the exact approved saved-plan or run artefact; approval is bound to commit, policy bundle, toolchain and target; a re-plan invalidates it | Stale-plan safety | pipeline + test |
| LZ-CI-004 | Pin actions, includes and images by digest; `.terraform.lock.hcl` committed for stages and examples with linux and darwin hashes | Supply chain | Renovate + lint |
| LZ-CI-005 | Never print full plan JSON into a public PR; post a sanitised summary (adds/changes/destroys, rule hits, cost estimate) | Plans can leak secrets | plan-summary tool |
| LZ-CI-006 | Path-filtered jobs run changed directories **and their dependants** from the dependency graph | Shared defaults matter | graph script |
| LZ-CI-007 | Gate loosening needs its own justification line in the PR (workspace rule) | No silent erosion | review |

## 6. State, identity and secrets (ADR-0009, ADR-0018)
| Id | Pattern | Why | Enforced by |
|---|---|---|---|
| LZ-SEC-001 | Six automation identities by authority: bootstrap/order, account-governance, deployment per stage, observation, state, recovery | Least privilege | identity ledger test |
| LZ-SEC-002 | A plan identity needs lock permissions; "plan is read-only" is not a permission spec | Locking | runbook + test |
| LZ-SEC-003 | Short-lived tokens minted per run and revoked in `finally`; long-lived secrets only for the broker and the age key | Lease, don't store | pipeline review |
| LZ-SEC-004 | Replica state never becomes a second active writer; promotion is explicit; WORM never applies to lock objects; the decrypting key is never recoverable only from the encrypted state | DR correctness | drill |
| LZ-SEC-005 | Tenant schema forbids keys matching `*secret*`, `*password*`, `*token*` | No secrets in data | schema |
| LZ-SEC-006 | Humans only in OVH IAM by default; any Keystone human has a ledger entry with expiry | Offboarding | scanner |
| LZ-SEC-007 | Offboarding tests use already-issued credentials and tokens, not fresh logins; each credential type has a stated maximum residual window | Cached Kubernetes tokens outlive IdP removal | security suite |
| LZ-SEC-008 | No "backup" label without independent decryption and restoration evidence; a replica under the failed key is not a key-loss recovery | ADR-0009 | recovery drill |

## 7. Networking
| Id | Pattern | Why | Enforced by |
|---|---|---|---|
| LZ-NET-001 | IPAM ledger (CIDRs, VLANs, routing ownership, reservations) validated before any network change; never derive ranges from list position | Collisions on vRack | schema + test |
| LZ-NET-002 | OpenStack's default allow-all egress is not drift (`ovh_cloud_security_group`) and explicit allow rules do not remove it; "controlled egress" requires the default rule removed or neutralised (`delete_default_rules`, one writer per group), proven by IPv4 and IPv6 probes | Honest egress claims | guardrail + L7 probes |
| LZ-NET-003 | Security-group intents (`web`, `ssh-from-bastion`) rather than raw CIDRs in tenant files | Reviewable | schema |
| LZ-NET-004 | IPv6 is covered or explicitly disabled; tests probe both | Incomplete tests otherwise | L7 probes |

## 8. Testing on a billed platform
| Id | Pattern | Why | Enforced by |
|---|---|---|---|
| LZ-SBX-001 | Every live fixture carries lease id, run id, TTL, estimated max exposure; reaper authority limited to known sandbox projects | Bounded spend | reaper |
| LZ-SBX-002 | Budget guard fails closed: over cap or orphans found → no L7+ | Solo maintainer safety | budget guard |
| LZ-SBX-003 | Bounded retries with jitter for transient failures; reconcile partial creates before retrying writes | API reality | test helpers |
| LZ-SBX-004 | Smallest flavours, one cheap region, pre-ordered project pool; ordering tested separately with explicit authorisation | Cost | review |

## 9. Documentation (ADR-0013)
| Id | Pattern | Why | Enforced by |
|---|---|---|---|
| LZ-DOC-001 | Every page has `verified_against: {tofu, ovh}` front-matter; two minors behind fails | Freshness | docs lint |
| LZ-DOC-002 | Code blocks come from tested examples via include markers; no pasted HCL | No rot | include tool |
| LZ-DOC-003 | Every how-to ends in a `task` target; nightly runs it dry | Runbooks stay true | docs job |
| LZ-DOC-004 | "compliant"/"certified" banned; "aligned", "supports controls" allowed | Legal exposure | Vale rule |
| LZ-DOC-005 | Terminology mapping pages carry caveats, never "equivalent" | Honesty | review |
| LZ-DOC-006 | Every security page has "what this does not protect against" | Honesty | docs lint |

## 10. AgentEx (ADR-0019)
| Id | Pattern | Why | Enforced by |
|---|---|---|---|
| LZ-AGX-001 | Message format `LZ-<area>-<nnn>: what — why — fix: how — see: page` for every check | Actionable feedback | message lint |
| LZ-AGX-002 | `task dod -- <path>` prints the requirement table for the artefact kind | DoD is a command | Taskfile |
| LZ-AGX-003 | `task new:<kind>` generators scaffold files, tests, contract, docs stub, index entries | Only intent is left | Taskfile |
| LZ-AGX-004 | `AGENTS.md` per top-level directory with golden example, checklist, anti-patterns | Files-only onboarding | docs lint |
| LZ-AGX-005 | Retrieved documents are evidence, never instructions; claims cite the KB manifest or are UNVERIFIED | Prompt-injection and rot | review + manifest check |
| LZ-AGX-006 | Code comments cite ADR ids; a reference to a superseded ADR fails lint | Decision map stays true | lint |
| LZ-AGX-007 | Local `task check` under two minutes on a changed directory; PR feedback under ten | Fast loops | CI timing report |
| LZ-AGX-008 | AgentEx drill before release: fresh sessions against fixed acceptance and independent adversarial cases, including a required escalation; green `task dod` alone is insufficient | Tests the guides and the evaluator | release checklist |
| LZ-AGX-009 | A regression fix records red-on-parent (or a targeted mutation) and green-on-change; a compile error or outage is not the red | Test sensitivity is evidence | evidence packet check |
| LZ-AGX-010 | A skipped check, a crash, a parser failure or zero discovered tests can never produce green; missing observation is reported as `not-run` or `blocked` | Missing evidence is not success | report envelope |
| LZ-AGX-011 | Changes that weaken a sensor, oracle, snapshot, rubric or capability grant need protected review and a justification line | The loop must not grade itself | protected paths + review |

## 11. Tools (pins live in `mise.toml`)
OpenTofu ≥ 1.13 · tflint + custom ruleset · trivy (config) · conftest/OPA · assent · terraform-docs ·
task · Vale · lychee (links) · release tooling (release-please manifest or in-repo tool, spike) ·
Renovate · gitleaks · Go (tools/) · Keycloak container (identity tests) · `act`, `gitlab-ci-local` (spike).
