# Planned commands and boundaries

None of the commands below is implemented by the early written journey (T014). D29
authorizes prose, a bounded renderer experiment and a minimal in-memory controller only.
Prototype implementation/execution waits for actual 001/T003 safety/capture qualification
and review of the exact experiment. There is no public prototype launch command yet.
See the [early walkthrough](../../../docs/how-to/preconfigure-repository.md).

## Active prototype interaction contract (not yet implemented)

Show a choice's suggested value, versioned rule/source, reason and consequence, with
optional detail. Keep explicit choices distinct from suggestions even when values match.
Back/edit previews changed defaults, retains valid explicit overrides and invalidates
dependent answers that no longer fit. Recompute valid-completed/relevant-required progress;
neither a count nor an in-memory review implies deployment or export readiness.
Help and back must not commit unfinished input. Cancel exits without saving; plain/no-TTY
and narrow-terminal behavior need actual capture qualification, with readable reasons,
counts and actions. Unsupported combinations/platforms remain blocked or unqualified.
No save/resume, helper integration, persistence or export claim follows from this contract.

## Eventual command contract (deferred dependencies retained)

Persistence/export, including synthetic versions, wait for real profile schemas. The
full session/bundle model and owned write roots in data-model.md describe this later
contract, not active prototype write permissions. All controls below remain required.

- `task configure`: launch the local guided helper. Back/edit/detail/save/exit/review are local
  interactions. Reports prerequisites/choice consequences; no cloud calls or configuration-file
  mutation. Only owned local checkpoint/staging/evidence paths in data-model.md may change.
- `task configure:resume`: load/revalidate the fixed local session location; refuse foreign,
  unsupported, corrupt or symlinked data. No session value becomes a command or arbitrary path.
- `task test:configure-flow`: controller/help/dependency invalidation tests (T003 creator).
- `task test:configure-resume`: atomicity, revisions, scope, permission and lock controls (T005).
- `task test:configure-checks`: bounded trusted read-only tool checks/injection controls (T007).
- `task verify:configure-journey`: actual pinned PTY/plain captures and recorded human rubric (T009).
- `task test:configure-export`: reviewed deterministic complete draft, untouched existing repo (T011).
- `task verify:configure`: full V001–V005 aggregate; missing human/terminal/dependency evidence
  reports blocked/not-run rather than pass (T013).
Interactive and noninteractive modes share strict schema/controller. A partial session is never
ready output. Export confirmation binds the exact reviewed digest and is invalidated by edits.
Every command above remains planned; targets exist only after their creating tasks execute.
Refresh cheap helpers on resume and before final review/export; actual dirty source/tool changes
invalidate review. Persist explicit/default provenance even when the effective values are equal.
Inventory all paths outside the narrow owned write roots, including ignored files. Sessions are
revalidated input, not signed authority. Publish complete drafts atomically; interrupted/changed
bundles cannot qualify as successful exports and identical verified retries are idempotent.
