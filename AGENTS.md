# AGENTS.md — working contract for assistant sessions

Scope: this repository. Authoritative sources: the
[constitution](.specify/memory/constitution.md), [specs/README.md](specs/README.md) and the
ADR index ([docs/adr/README.md](docs/adr/README.md)). Phase 001 T019 expands this file into
the progressive router with per-kind guides (ADR-0019); until then, follow the rules below.

1. **Isolated worktree.** Work in a disposable git worktree, branched from the current base;
   never commit straight to `main` and never push without explicit instruction.
2. **Explicit lane claim.** Name the spec and task ids you are executing before starting;
   keep every change inside that lane's scope and record cross-feature dependencies you hit.
3. **Test-first engineering.** Write failing behaviour tests before implementation
   (constitution III); test-authoring tasks close with valid-case plus behavioural-red
   evidence; implementation tasks close the same checks green.
4. **Atomic conventional commits.** One logical change per commit in gitmoji-conventional
   form (`:memo: docs(...):`, `:gear: chore(...):`); no agent mentions in product text
   or commit messages (ADR-0019).
5. **Linear history.** Rebase onto the base instead of merging; keep history a
   fast-forward chain; reopened decisions get their own commit with the reason.

Acceptance checks are predefined in each spec's acceptance table and stay planned until
their creating tasks land. Decisions and findings are recorded where the specs say: ADRs
under `docs/adr/`, review records under `specs/reviews/`, dispositions in
`specs/reviews/dispositions.md`. Evidence is private under `.local/evidence/`
(gitignored); sanitised summaries may be committed.
