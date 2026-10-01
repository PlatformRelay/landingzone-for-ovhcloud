# Planned validation journey
No setup helper exists yet. Use synthetic independent choices until real schema/catalogue creators
land; never use fixture success as a deployable-profile claim.

After the tasks create their targets: `task configure` → choose a convention → inspect why/default/
consequences → back/edit → save-and-exit → `task configure:resume` → inspect changed dependencies
and local prerequisite findings → review the deterministic diff → export a local draft bundle.
Configuration files and cloud resources remain unchanged; only owned local checkpoint/staging/
evidence paths in data-model.md may change. Inventory everything outside those roots.

Run all V001–V005 targets, inspect private evidence and perform the documented human walkthrough.
Repeat with malformed/foreign sessions, write interruption, stale schemas, missing tools, invalid
choices and unreviewed export. Confirm each fails for its intended behavior while valid unusual
choices still work. No docs-only test is required for this prose guide.
