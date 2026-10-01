# Research and decisions
Primary examples inspected 2026-10-01: [Gum](https://github.com/charmbracelet/gum) provides prompt
commands for shell scripts; [Huh](https://github.com/charmbracelet/huh) provides Go forms. Neither
choice establishes safe persistence or authorization. The existing Go tools language favors a
controller shared by interactive and noninteractive modes; real prototypes must qualify renderer
behavior before choosing exact pins. No tool/library was installed or terminal fixture captured.

Resume is application state, not an executable command. A session binds repository and dependency
revisions, carries only allowed non-secret answers and is revalidated. Review/export is distinct
from deployment or automatic repository mutation. Friendly explanations are checked for coverage
by controller/terminal tests and reviewed by a human for tone and usefulness.

The naming interface is pending joint decision; collect organisation convention data without
silently inventing the module input contract. Real profile/configuration exports require real
schema/catalogue creators. Synthetic fixture walkthroughs remain useful without that qualification.

## Upstream input trace
Pinned upstream story and region-default inputs are traced in ../../docs/reference/upstream-reference-map.md. T002–T003 may exercise synthetic explained defaults only after scope approval; current region availability and replacement impact need real catalogue evidence.
