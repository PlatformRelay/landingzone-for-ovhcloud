# Data model
- StepDefinition: stable id, dependencies, question, brief explanation, default provenance,
  consequence, optional detail, allowed choice references, validation and next-action messages.
- UseCaseDefaults: versioned rule id, declared use-case dimensions, compatible choices and
  per-field suggested value/reason. Explicit answers and derived defaults have distinct provenance.
- Progress: derived valid completed-step count and relevant required-step count; recomputed from
  the current branch/answers, never trusted from a persisted percent or a prior completion flag.
- Session: schemaVersion, repository binding, source/schema/catalogue/prompt revisions,
  allowlisted valid answers with explicit/default origin and default-rule references, completed
  step ids, last completed step, status and stale observations. Binding identifies this working
  tree and actual relevant source contents, not merely an origin URL, branch or Git HEAD.
  No secret field, executable code or arbitrary path/command. Size is bounded; unknown keys reject.
- CheckObservation: shared phase-001 report envelope plus check id, trusted command identity,
  deadline, observed prerequisites and pass/fail/blocked status. A timeout never passes.
  Rerun cheap checks on resume and before final review/export; compare actual source/tool bindings
  and effective results. Changed inputs/results invalidate review. Observation timestamps alone
  are not draft configuration inputs and do not make an otherwise identical draft nondeterministic.
- DraftBundle: selected effective data, source bindings, unresolved actions, qualification status,
  file digests and complete bundle manifest; explicit review confirmation tied to its input digest.
Transitions: start → answering → saved → resume/revalidate → answering → review → exported-draft.
Cancel preserves only previously validated answers. Editing a dependency invalidates downstream
answers and old review confirmation. Source changes trigger revalidation, not silent defaulting.
Use-case edits preview changed suggestions, preserve valid explicit overrides and recompute progress.
Only checkpoint/temp/staging under `.local/configure/` and private `.local/evidence/004/` reports
may be written. Complete-bundle publication is atomic; partial/changed existing staging refuses.
