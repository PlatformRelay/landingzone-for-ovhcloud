# Explore repository preconfiguration

Start with what you want to learn or build. The planned setup helper will explain the choices,
suggest a starting point and help you see what changes when you choose something else. You
remain in control of the answers; finishing the questions will not deploy anything.

**Available now: this written walkthrough.** There is no setup helper to run yet. The next
increment is a bounded terminal experiment with answers held only in memory. Saving, resuming
and exporting are deferred until real profile schemas exist and their safety controls are
implemented and verified. No credentials, cloud connection or terminal commands are needed
to read this guide.

Every example below is a **synthetic, authored illustration**, not captured terminal output,
a supported profile, a naming contract or evidence of implemented behavior. The wording
describes the intended experience for review.

## Start with your use case

Imagine trying the journey to understand how naming choices work for one team. An opening
question could ask whether you are exploring conventions or preparing to discuss tenant
self-service. A short explanation should make the choice useful: it selects which questions
are relevant and which defaults to suggest. It does not qualify a production or regulated setup.

You should be able to read the explanation, inspect optional detail and change your answer.
If a choice is unsupported, the journey should say which part is unavailable and offer a
useful next action, such as returning to an exploration example. Selecting a regulated or
experimental option must keep its qualification and customer-action gates visible; accepting
a suggestion cannot satisfy them.

## Understand a suggestion before accepting it

A default should answer three questions: where did this come from, why is it suggested here,
and what happens if I use it? For this illustration, imagine a versioned rule called
`example-names-v1` with these two suggestions:

| Use case | Suggested display order | Reason | Consequence in this example |
| --- | --- | --- | --- |
| One-team exploration | Environment before service: `dev-api` | Put the environment first so it is easy to spot | Example names sort by environment first |
| Tenant self-service exploration | Team before service: `sample-api` | Make the team visible when several teams share a list | The example now needs a team label |

These invented rules illustrate explanations; they do not prescribe actual resource names,
provider limits or an available catalogue. A real journey must display the applicable rule
version and source for each suggested field, alongside the effective value and its reason.
Optional detail should explain trade-offs without forcing everyone through a long lesson.

Accepting a suggestion and choosing the same value explicitly are different intentions.
Suppose two people both see `dev-api`: one follows the suggested order; the other explicitly
chooses environment-first because that is their team's convention. Their effective values
match, but the journey must remember which was suggested and which was explicit. That is
what lets a later use-case change offer new defaults without silently replacing a valid override.

## Go back without losing unrelated choices

Now change the use case to tenant self-service exploration. Before accepting the edit, you
should see which suggestions change, why they change and which answers need another look.
In the synthetic example, the suggested naming order changes to team-first and a team-label
question becomes relevant. A valid explicit environment-first override stays explicit; if an
override becomes incompatible, the journey should explain the conflict and ask for a new
choice rather than silently replacing it.

Only dependent answers that no longer fit should be invalidated. An unrelated valid answer
should remain. Any earlier review of the effective choices must be invalidated when those
choices change, so an old confirmation cannot approve the new result. Help and back should
leave an unfinished answer uncommitted.

## Read progress as questions answered

Progress measures valid answers in the current journey. It should show both a progress bar
and a readable completed/required count. Irrelevant questions do not belong in the total;
invalidated answers do not count as complete.

For example, imagine **3 of 4 relevant questions complete**. An edit adds a fifth relevant
question and invalidates one previous answer, so progress becomes **2 of 5 complete**. The
journey should explain that the edit added a question and made an earlier answer need review.
These numbers illustrate the counting rule; they are not observed output or a fixed step list.

Even **5 of 5 complete** would mean only that these questions have valid answers. Missing local
prerequisites, unavailable profiles and unverified platform behavior must remain visible.
A completed questionnaire never establishes deployment, compliance or export readiness.

## Ask for help, change course or stop

The renderer experiment must establish how these interactions actually work before keyboard
shortcuts or platform support are documented. The intended behavior is:

| Interaction or mode | What to expect from the future prototype |
| --- | --- |
| Help/detail | Explain the current choice, its default and consequences; return without changing the answer |
| Back/edit | Revisit an earlier choice, preview its effects and revalidate dependent answers |
| Cancel | Stop the in-memory journey and discard its answers; no saved session or resume promise |
| Plain/no-TTY | Keep explanations, choices, errors and progress readable without relying on color or cursor movement; share validation with interactive input |
| Narrow terminal | Keep reasons and actions readable without hiding essential text; qualify actual behavior at recorded widths |
| Unsupported terminal/platform | State the limitation and leave that combination unqualified; do not imply a successful journey |

No renderer, key binding, plain-mode flag or terminal matrix is qualified by this document.
The in-memory prototype will not save a checkpoint. If you stop it, expect to begin again.
The eventual product should offer safe save-and-exit and resume, but those features need
separate persistence, interruption and revalidation checks before this guide can recommend them.

## Review what is still missing

A useful in-memory review should distinguish effective choices from unanswered questions,
unsupported selections and checks that have not run. It must not claim a file was exported
or a local tool passed when there is no observation. A blocked check should explain the
missing prerequisite and the next action; a timeout is not a pass.

The eventual export flow will show a deterministic draft diff, require explicit confirmation
of that exact content and publish a complete schema-valid bundle into controlled staging.
It must leave existing repository files unchanged. Edits or relevant source/tool changes
invalidate review; retry must not bless a partial or changed bundle. Even that future draft
will carry no authority to deploy, procure resources or commit changes.

These later controls remain part of the [full specification](../../specs/004-guided-preconfiguration/spec.md).
Real profile schemas are a prerequisite for persistence and export, including synthetic
implementations. Real export also needs its configuration, catalogue and naming contracts.
Writing this guide does not supply those contracts or qualify save/resume, helper checks or export.

## Commands you may see in the plan

**All of these commands are planned and not yet implemented. Do not run them as setup
instructions.** There is no public command for the bounded prototype yet.

| Planned command | Intended purpose | Creating task in spec 004 |
| --- | --- | --- |
| `task configure` | Start the full guided helper | T009 |
| `task configure:resume` | Revalidate and continue a saved session | T009, after T005 persistence |
| `task test:configure-flow` | Check controller/default/progress behavior | T003 |
| `task test:configure-resume` | Check checkpoint and revalidation controls | T005 |
| `task test:configure-checks` | Check bounded local helper controls | T007 |
| `task verify:configure-journey` | Inspect actual terminal captures and the human walkthrough | T009 |
| `task test:configure-export` | Check reviewed draft publication controls | T011 |
| `task verify:configure` | Aggregate the full V001–V005 evidence | T013 |

The [task list](../../specs/004-guided-preconfiguration/tasks.md) keeps the implementation
dependencies and deferred controls. T014 owns this early written journey. T012 will turn it
into the final setup/resume/review/export guide after the full terminal and export work exists.
The [prototype preparation plan](../../specs/004-guided-preconfiguration/plan.md#next-prototype-packet-requirements-only)
records the next experiment's requirements; implementation and captures wait for actual
foundation safety/capture qualification. This walkthrough supplies no terminal evidence.

## Clarity review

Review the prose independently of runtime tests. A reader should be able to explain:

- Which parts exist today and which remain planned or deferred.
- Where a suggested default comes from, why it fits and what changes if it is accepted.
- Why an explicit choice can survive a use-case edit while an invalid dependent answer cannot.
- Why progress can go backwards, and why complete answers do not mean readiness.
- How to ask for detail, return to a choice or stop, including the loss of in-memory answers.
- Which prerequisite blocks the next step, without interpreting an illustration as a supported profile.

Record unclear wording and missing explanations as findings. Link and whitespace checks can
catch document defects; they cannot establish tone, keyboard behavior, accessibility or runtime
safety. A later independent human walkthrough must use actual qualified terminal behavior and
the final guide; a prose review cannot substitute for it.
