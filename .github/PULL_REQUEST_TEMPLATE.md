## What and why

<!-- One paragraph. Link the spec task (Tnnn), ADR, known issue (KI-nnn) or issue it closes. -->

## Checks run

<!-- Every check runs inside the offline entry:
     lz-offline --candidate "$PWD" -- task <target>
     Paste each target with its result line (DOCS_OK, TRACE_OK, ...). -->

- [ ] `task check -- <path>` on each path I changed
- [ ] Docs only: `docs:check`, `check:specs`, `test:traceability`
- [ ] Code or configuration: every target in `LZ_TARGETS` of `.github/workflows/foundation.yml`
- [ ] A page whose `references:` files changed has new blob ids and `last_verified`

## Live run

<!-- Most changes need none. A docs-only or ADR-only change never runs a live layer, and a
     live run is not added "just to be safe" (AGENTS.md, Sandbox budget). Contributors never
     run task live:* or task bootstrap:*; the maintainer does, from a reviewed SHA on main. -->

- [ ] No live run needed
- [ ] Needs a live run because it changes IAM, state, credentials, or how a stage acts on OVHcloud: <!-- which -->

<!-- When a live run happened, record it here:
     Run id: ...
     Approximate cost: ... EUR (from the Control Panel or ovhcloud)
     Leftover check after destroy: clean / what was left -->

## Before merging

- [ ] Commits follow `:gitmoji: type(scope): summary` with an ASCII shortcode, one logical change each, every commit green
- [ ] No credentials, account ids or project ids in the diff, the commits or this description
- [ ] Claims about OVHcloud behaviour cite a source or say UNVERIFIED
- [ ] Reviewed by someone who did not write it; lands by rebase and merge (no squash, no merge commit)
