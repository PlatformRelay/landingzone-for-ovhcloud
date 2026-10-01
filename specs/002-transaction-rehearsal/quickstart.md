# Validation guide: Deployment and self-service transaction rehearsals

Status: planned. These targets do not exist yet; tasks.md identifies their creating tasks.
Do not run acceptance commands until their implementation and prerequisites have landed.

1. Read spec.md, plan.md, contracts/checks.md and constitution II–V.
2. Use the pinned toolchain prepared by 001; verify versions before fixtures/tests.
3. Follow tasks.md dependencies. Test-authoring tasks record valid-case and behavioural
   red evidence; implementation tasks rerun the exact suite for green evidence.
4. Run this spec's V checks in table order after their creators exist. Each emits the
   001 report envelope to private .local/evidence/002/; preserve sanitized summaries.
5. Run the appropriate negative controls, clause mutations, crash and concurrency cases.
6. Keep absent tools, unavailable forge/credentials and unobserved mechanisms blocked.
7. Complete independent review and record every non-docs check before changing to done.

Local V001–V006 completion and actual forge qualification are distinct. Missing
required adapter/setup is an explicit per-forge blocked record; it cannot complete
FR-008/SC-003 or authorize merge. 003 needs only local T008/T012/T016 evidence.
A planning document grants no purchase, deletion or IAM authority. Docs-only tasks
need content review rather than invented acceptance tests.
