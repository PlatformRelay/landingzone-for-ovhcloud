# Validation guide: Protected platform and recovery feasibility

Status: planned. These targets do not exist yet; tasks.md identifies their creating tasks.
Do not run acceptance commands until their implementation and prerequisites have landed.

1. Read spec.md, plan.md, contracts/checks.md and constitution II–V.
2. Use the pinned toolchain prepared by 001; verify versions before fixtures/tests.
3. Follow tasks.md dependencies. Test-authoring tasks record valid-case and behavioural
   red evidence; implementation tasks rerun the exact suite for green evidence.
4. Run this spec's V checks in table order after their creators exist. Each emits the
   001 report envelope to private .local/evidence/003/; preserve sanitized summaries.
5. Run the appropriate negative controls, clause mutations, crash and concurrency cases.
6. Keep absent tools, unavailable forge/credentials and unobserved mechanisms blocked.
7. Complete independent review and record every non-docs check before changing to done.

For 003, every live target is a probe root run by spec 005's `task live:probe` as an owner
session (D113); RunConfig and leases are superseded by spec 005's run core. A planning document is not permission for a purchase, deletion or IAM change.
Docs-only tasks need content review, not invented acceptance tests.

The per-spike timeboxes, cost estimates and refutation stops are in spec.md. A run's deadline
is enforced by spec 005's runner, which then destroys and checks for leftovers; cost is an
estimate, never an admission condition (D113). A complete decision packet is not a
qualification pass. Spec 002 is retired (D113): its items report `blocked` in the aggregate.
