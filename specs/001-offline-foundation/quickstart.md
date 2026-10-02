# Validation guide: Offline foundation, verification and naming

Status: planned. These targets do not exist yet; tasks.md identifies their creating tasks.
Do not run acceptance commands until their implementation and prerequisites have landed.

1. Read spec.md, plan.md, contracts/checks.md and constitution II–V.
2. Use separately approved tool preparation and T003's installed absolute-path `lz-offline`
   entry outside the candidate tree; verify source/binary/image identities. Never run
   `task check` on a candidate checkout on the host. Task/Go commands in this guide/spec
   execute inside the child via `<approved-absolute-path>/lz-offline --candidate <checkout>
   -- task <target>`. T002 first qualifies this entry with reviewed bounded stubs/driver.
3. Follow tasks.md dependencies. Test-authoring tasks record valid-case and behavioural
   red evidence; implementation tasks rerun the exact suite for green evidence.
4. Run this spec's V checks in table order after their creators exist. Each emits the
   001 report envelope to private .local/evidence/001/; preserve sanitized summaries.
5. Run the appropriate negative controls, clause mutations, crash and concurrency cases.
6. Keep absent tools, unavailable forge/credentials and unobserved mechanisms blocked,
   except C010.5/P4 explicitly DEFERRED to KI-001 within the scoped exception below.
7. Complete independent review and record every non-docs check before changing to done.

Full 001 exit needs actual forge evidence, naming implementation and the
exclusive-runner benchmark. The current goal (2026-10-02, local decision D61) authorizes
all locally implementable portions of specs 001–004 whose dependencies and required
decisions are satisfied. T001–T009 remain minimum safety before dependent work, followed
by conditional minimal CI T023 after evidenced completion. Publish independently reviewed
coherent increments, including stacked PRs with explicit parent branches and review order;
continue other eligible work when one task is blocked. No PRs may be merged during this
goal; earlier merge permission is superseded. D29's hardened persistence/export remains
deferred until real profile schemas exist. The 2026-10-02
operator priority exception (local decision D60) accepts C2 (C010.5/P4) as DEFERRED,
nonblocking [KI-001](../../docs/known-issues/KI-001-ci-source-admission-unqualified.md)
for current private maintainer development, publication, review PR creation and merge.
Frozen workflows, a no-bypass ruleset and the disposable source-admission experiment
remain later obligations. Whole-feature hardened guarantees remain unqualified; neither
the constitution nor ADRs change. Review PR creation does not establish merge readiness.
Exact pins, read-only CI authority and T003 isolation remain required. TDD, predefined evidence,
independent review, actual required checks/PR-head CI and mergeability remain required
for eventual merge readiness; they do not override this goal's no-merge instruction.
Structure, constitution and committed scaffold decisions are confirmed.
Create directories with their first real artifact. Exact pins, image/cache and actual
tool/isolation premises remain unobserved until their proof tasks run. The subset uses
real fixture modules for static checks; absent modules/naming stays not-run. Later
schema/forge/latency duties cannot be closed by the subset's partial V observations.
A planning document grants no purchase, deletion or IAM authority. Docs-only tasks
need content review rather than invented acceptance tests.
