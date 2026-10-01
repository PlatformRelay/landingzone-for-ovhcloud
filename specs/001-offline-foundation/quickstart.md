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
6. Keep absent tools, unavailable forge/credentials and unobserved mechanisms blocked.
7. Complete independent review and record every non-docs check before changing to done.

Full 001 exit needs actual forge evidence, naming implementation and the
exclusive-runner benchmark. The operator authorized only T001–T009 for the current
run, followed by conditional T023 after evidenced completion. T023 first creates and
qualifies pre-execution CI source admission with Actions disabled during source publication;
its setup is tested in a disposable repository and independently reviewed before target writes. Candidate publication waits for approved source plus
active no-bypass workflow-path rules and actual valid/denied push controls; no such
workflow/ruleset is qualified now. An owned-branch filter or post-run source check is insufficient. TDD, predefined evidence,
independent review, actual required checks/PR-head CI and mergeability gate the granted
merge permission. Structure, constitution and committed scaffold decisions are confirmed.
Create directories with their first real artifact. Exact pins, image/cache and actual
tool/isolation premises remain unobserved until their proof tasks run. The subset uses
real fixture modules for static checks; absent modules/naming stays not-run. Later
schema/forge/latency duties cannot be closed by the subset's partial V observations.
A planning document grants no purchase, deletion or IAM authority. Docs-only tasks
need content review rather than invented acceptance tests.
