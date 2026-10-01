# Independent adversarial review: direction and guided-preconfiguration plan

Date: 2026-10-01. Target: current uncommitted changes in
`the first-phases worktree`,
branch `codex/first-phases`, HEAD `0e128f3acd157126a59effa202485f4ebbcb8f8b`,
planning base `116405b98960dcc78b3843985a4a49f35afae08a`.

Method: independent adversarial review with four personas;
workspace AGENTS.md and agent-context/GUIDELINES.md were read. No other reviewer
report was opened. This is a design/prompt/scaffold review, not an implementation
qualification. No product files were edited. The absence of 004 tasks is deliberate:
plan → independent adversarial review → tasks.

## Verdict: CLEAN

No CRITICAL or WARNING finding survived attack/defend/revise. This verdict permits
progressing the design lane to task generation; it does not qualify the wizard,
filesystem handling, renderer, cloud combinations or any acceptance target. Those
remain explicitly unimplemented/not-run.

### CRITICAL

None surviving.

### WARNING

None surviving.

### NOTE

- **Make saved answer provenance concrete when generating tasks** —
  `specs/004-guided-preconfiguration/data-model.md:4`,
  `specs/004-guided-preconfiguration/data-model.md:8` [New Hire].
  Attack: journey A accepts suggested value X; journey B explicitly chooses the same
  X. Both save/resume, then change the use case whose new suggestion is Y. Serializing
  only the effective value makes the two histories indistinguishable and can erase B's
  override or freeze A's old default. Defense: the model explicitly distinguishes
  explicit/default provenance and FR-010 requires preserving valid explicit overrides;
  the current design therefore already forbids that implementation. Optional refinement:
  spell out the persisted answer origin/default-rule reference and put this equal-value,
  save/resume/use-case-edit sequence in V001/V002 task controls. Do not persist a computed
  percent or treat completed-step ids as independent validity proof.

- **Choose one explicit local-observation refresh rule** —
  `specs/004-guided-preconfiguration/data-model.md:11`,
  `specs/004-guided-preconfiguration/spec.md:38` [Saboteur].
  Attack: save after a passing prerequisite check, change a tool or dirty local choice
  file without changing Git HEAD, resume and export from the old observation. Defense:
  FR-004 requires revalidation against current sources and stale-observation reruns;
  the shared 001 envelope binds source/input/policy digests, tool versions and environment.
  The plan does not authorize accepting a matching version string or old completion flag
  as proof. Optional refinement: define the source-content bindings and refresh point in
  the task contract. Always rerunning the cheap bounded checks on resume and immediately
  before final review/export is a simpler first implementation than a general cache-age
  policy. Include dirty-content and post-review-input-change controls, not only a schema
  version bump.

- **Define the owned local-write area and the meaning of tamper rejection precisely** —
  `docs/adr/0023-guided-repository-preconfiguration.md:56`,
  `specs/004-guided-preconfiguration/spec.md:55` [Security Auditor].
  Attack: a verification inventory includes `.local/configure/`, so a legitimate checkpoint
  replacement violates the literal unchanged-repository assertion; alternatively, a broad
  ignored-file exclusion could hide an unintended write. A same-user edit that produces
  another schema-valid answer also cannot be distinguished from intentional user input
  merely by strict decoding or a self-computed hash. Defense: this helper deliberately owns
  local checkpoint/staging writes; sessions are untrusted convenience rather than authority,
  and exact reviewed output is separately bound before export. No authenticity guarantee
  is needed for a local draft. Optional refinement: name the narrow owned checkpoint,
  temporary-file and staging roots; inventory everything outside them. Describe rejection
  as structural/binding/revalidation rejection unless a separate authenticity mechanism is
  actually introduced. Avoid adding an authentication/key lifecycle just for local answers.

These are implementation traps and optional precision, not claims that the current design
allows the bad result. The governing requirements already reject the relevant behavior.

## Load-bearing premises checked first

The most load-bearing premise is that the polished terminal flow can remain an adapter to
a single controller, while sessions and draft exports never confer deployment authority.
If rendering, persisted completion or a reviewed session were treated as authority, the
rest of the design would be unsafe regardless of prompt quality. I traced that premise
through 004 plan:5–8, spec FR-003–FR-008, contracts:13–14 and ADR-0023:64–70. The documents
consistently assign decisions/revalidation to the shared controller, regard session data
as untrusted, invalidate review after edits and bind export to the exact reviewed digest.
There is no implementation to observe; this premise is correctly a required future
behavior, not an observed claim.

Other premises were checked against source/system rather than repeated from the plan:

| Premise | Actual inspection | Result and boundary |
| --- | --- | --- |
| The helper can reuse one Go tools module | Searched target for go.mod, Taskfile, mise.toml and schema files; read 001 T001 and T005–T007 | No tools module exists yet; 001 T001 explicitly creates it. “Existing tools module” is a future integration assumption, and the dependency gate prevents treating it as delivered. No second module is required. |
| Both renderer options are credible starting points | Read primary [Huh](https://github.com/charmbracelet/huh) and [Gum](https://github.com/charmbracelet/gum) documentation | Their library/subprocess roles match the comparison. Exact pins, platform behavior, cancel/plain/narrow-terminal captures remain unverified. Huh's documented accessible mode does not qualify this wizard's accessibility. |
| OVH already has network landing-zone code and an end-to-end example | Verified local reference HEAD, then read landing-zone README, modules and OrbitalEdge documentation | Clone is exactly `0ab581a39afbe61a2daa55b39238530f38665cc2`; hub/spoke, both network topologies, existing-project HA and four-spoke story match the map. “Deployable” describes the published example, not a successful local deployment. |
| The inspected upstream has visible lint but no landing-zone suite/CI workflow | Tracked-path searches for test/spec directories, tftest and test files, .github/workflows and GitLab CI; read .pre-commit-config.yaml and .github/release.yml | No matches for the scoped suite/workflow searches; pre-commit includes fmt/TFLint. .github/release.yml is changelog categorization, not CI. This supports the narrowly worded snapshot observation, not absence across OVH or a successful lint run. |
| Upstream security/recovery mechanisms need adaptation | Read security guide, operations guide, provider/encryption configuration and storage policy | Disabled TLS identity checks, process-list API secrets, manual rotation, state-only encryption configuration and `s3:*` policy are present; the map warns about them and requires separate qualification. It does not mistake ephemeral inputs for a JIT issuer. |
| Naming reference claims are legitimate comparison inputs | Read primary Cloud Posse, Azure naming/AVM, azurecaf and Kubernetes documentation | Context/order/shortening, candidate catalogues, legacy migration, separate availability and label/annotation semantics match the exploration. None establishes OVH constraints; the exploration explicitly says so. Scalar/batch choice remains pending. |

The actual worktree has a distinct Git worktree administrative directory and shared Git
common directory. The future repository binding must intentionally choose its scope;
an origin URL or common directory alone is not proof that a session belongs to this
working directory. Current documents require the binding and foreign-session rejection
without claiming that a concrete binding mechanism has been qualified.

## What each persona actually tried

### The Saboteur

I traced save/restart, process death mid-write, disk full, a second writer, stale schema,
copied session, earlier-answer edits, newly relevant branches and review-then-edit through
the 004 requirements, transitions and V002/V004 controls. The attempted failure was losing
the prior valid checkpoint, counting stale answers as progress or exporting a different
bundle than the reviewed one. Atomic replacement, owner-only data, one writer, strict
foreign/symlink rejection, controller revalidation, derived progress and review-digest
invalidation are already required. No live deployment step exists, so there is no hidden
merge/apply ordering graph to test here. The concrete filesystem implementation and
export publication/retry behavior remain owed; they were not executed.

I also tried treating a completed synthetic questionnaire as a deployable profile and
using a regulated/use-case preset as qualification. Spec:72–78, FR-008/FR-010 and the
quickstart expressly retain creator, qualification and customer-action gates. Defense
holds; absence of real profile data is a blocking integration dependency, not a reason
to discard useful credential-free controller experiments.

### The New Hire

I reconstructed the main decisions without relying on conversation history: approved
reference-baseline ambition, deliberate differentiators, proposed ADR status, pending
naming cardinality, proposed renderer comparison and future command creators. I followed
004's dependency references to 001: T001 bootstraps the module/pins; T005 creates the report
schema and T007 consumes it through a transitive dependency; T016 supplies naming schemas.
No existing test/command was misrepresented as implementation proof. Provisional 004
creator IDs refer to a future task list; completing that list is the next authorized phase.

I attacked explicit override preservation with the equal-value histories described in
NOTE 1, and “invalidate downstream answers” against “preserve valid explicit overrides.”
FR-010 and the provenance distinction resolve the latter: dependency edits must recompute
validity/defaults, and valid explicit overrides survive. A future test must exercise both
clauses together. I did not retain an artificial contradiction by ignoring the more
specific preservation requirement.

### The Security Auditor

I followed the trust boundaries for answer/session-supplied commands, repository hooks,
unknown/duplicate fields, arbitrary paths, symlinked checkpoint/export locations, secret
input, helper timeout/network attempts and review-confirmation reuse. FR-003/FR-005/FR-008
and V002–V004 explicitly reject those behaviors; plan:14 prohibits arbitrary commands from
repository/session/answers. The trusted command identity is part of the observation model.
The renderer is not a second authorization engine, and export supplies no commit, merge,
procurement or cloud authority.

I tried inheriting an unsafe upstream default merely because it is a reusable topology.
The actual source contains the risky mechanisms named above, but the map demands adapted
TLS/secret handling, scoped policy, locking, independent recovery and protected-sandbox
controls before reuse. Defense holds. Copying no upstream implementation in this change
avoids a present redistribution/supply-chain expansion; license/provenance review remains
a future reuse prerequisite.

### The Budget Holder

I attempted to remove each proposed component and ask what requested outcome would fail:
controller supports back/edit/defaults/progress and noninteractive reuse; session supports
save/resume; bounded checks explain prerequisites; staged export preserves existing config;
renderer supplies the explicitly requested terminal journey. Each has a current consumer.
The plan excludes a portal, daemon, general CLI framework, direct multi-file application
and migration engine. Huh/Gum selection is a bounded comparison rather than two production
renderers. Fixture-first work is a cheap useful experiment and does not require market
research or cloud qualification before it can begin.

I also tested whether the upstream comparison dismisses existing value to justify a second
project. It credits network code, IAM/backend examples and the worked story, restricts the
additional-value claim to workflows still to be delivered, and preserves upstream
contributions/integration as useful outcomes. No objection survived on that ground.

## Scope and evidence

Read the changed constitution, prompt additions and override templates; changed README,
ADRs and patterns; new product/naming/reference documents; all six 004 design artifacts;
and relevant 001 contracts/data/tasks plus changed 001–003 plan context. Relevant local
upstream modules/guides were inspected statically. `git diff --check` returned clean.
This is formatting evidence only, not behavioral proof.

No severity promotion applies: there are no surviving WARNING defects independently
found by multiple personas. Hypothetical future implementations that violate already
binding requirements were revised to optional task-generation precision rather than
manufactured CRITICAL findings.

## What I could not check

- No wizard, controller, renderer integration, session schema, local-check adapter,
  export publisher or 004 task list exists. I could not run behavioral tests, kill
  clause mutants, fault writes, race directory swaps, prove permissions/lock recovery,
  check bounded child-process cleanup or verify atomic complete-bundle publication.
- No pinned Huh/Gum version, PTY capture, non-TTY capture, terminal matrix or independent
  human walkthrough exists. Library documentation is not evidence of those behaviors.
- No real profile/configuration schemas or supported cloud naming catalogue exist.
  The controller can be tested with synthetic data, but real exports remain gated.
- No upstream plan, script, installation, cloud/API probe or remote write was performed.
  Network isolation, HA, IAM effectiveness, token revocation, pricing, backend locking,
  cleanup, recovery and resource constraints remain unqualified.
- I did not rerun or audit the entire earlier 001–003 planning lane, read its reviewer
  reports or review every Spec Kit shell path. The review target is the current direction
  and 004 design changes, with relevant surrounding context.
