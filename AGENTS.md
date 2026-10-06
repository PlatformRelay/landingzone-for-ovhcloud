# AGENTS.md

Working contract for anyone, human or automated, changing this repository. Keep it short; the
design lives in [`docs/adr/`](docs/adr/README.md).

## Sandbox budget — read this first

**We have a one-off 200 € OVHcloud free-trial credit. Use it, but do not waste it.**

- **Spend it on what only a live run can tell us**: the feasibility spikes (IAM deny floor,
  tag-conditioned envelopes, credential issuance and revocation, state bootstrap), and the L7/L8
  layers of [ADR-0008](docs/adr/0008-testing-strategy.md) when the change actually acts on OVHcloud.
- **Be selective about testing.** A docs-only or ADR-only change runs no live layer. A change to a
  module, stage or policy runs its offline layers (L0–L5) plus at most the live tests of the parts
  it touches. The full live suite runs before a release and after a change to IAM, state or
  credentials — not on every PR.
- **A defect that only shows up a few PRs later is acceptable.** We trade detection latency for
  credit. Do not add a live run to a PR "just to be safe".
- **Every live run cleans up after itself** (destroy in `finally`; check that nothing tagged for the
  run is left over). Forgotten resources are the most likely way to burn the credit.
- **Record what a live run cost** in the PR description (approximate, from the Control Panel or
  `ovhcloud`), so the rate of spend stays visible.
- The credit is one-off, not monthly. Until ADR-0008 is amended, this section overrides its
  nightly L7/L8 rotation.

## Sandbox credentials and environment

Credentials never enter the repository, a worktree, a command line or any output.

- The maintainer's sandbox credentials live **outside the repository** in
  `~/.config/ovh-lz/sandbox.env` (mode 600):

  | Variable            | Meaning                                                      |
  |---------------------|--------------------------------------------------------------|
  | `OVH_ENDPOINT`      | API endpoint (`ovh-eu`)                                      |
  | `OVH_CLIENT_ID`     | OAuth2 client id of the `lz-sandbox-admin` service account   |
  | `OVH_CLIENT_SECRET` | its secret                                                   |

- Load them with a local, gitignored `.envrc`:

  ```sh
  dotenv_if_exists "$HOME/.config/ovh-lz/sandbox.env"
  ```

  then `direnv allow`. The OpenTofu `ovh` provider and the `ovhcloud` CLI both read these variables
  directly.
- `lz-sandbox-admin` is an OVHcloud IAM **service account**, not an API key tied to the account
  root: its rights are bounded by an IAM policy (`account:apiovh:iam/*`, `account:apiovh:me/*`,
  `publicCloudProject:apiovh:*`) and it can be revoked. Root-bound application keys (AK/AS/CK)
  bypass IAM entirely; do not create them except as a short-lived bootstrap, and revoke them after.
- The service account was created by a one-off OpenTofu config in
  `~/.config/ovh-lz/bootstrap/`. Its state holds the client secret in plain text; it stays there,
  never in a repository.
- **Known deviation:** [ADR-0019](docs/adr/0019-agent-experience-sensors-and-guides.md) says
  automated sessions never hold sandbox-apply credentials and that live lanes run only from
  protected branches. Until those pipeline lanes exist, live spikes run from the maintainer's
  workstation with the credentials above. This is a temporary exception, not the design.

## Tools

- **OpenTofu** — the design targets ≥ 1.13
  ([ADR-0011](docs/adr/0011-provider-strategy-and-opentofu-first.md)); check `tofu version` before relying on a
  newer feature.
- **`ovhcloud` CLI** ([ovh/ovhcloud-cli](https://github.com/ovh/ovhcloud-cli)) — the official
  command line for the OVHcloud API. Useful for quick inspection during spikes and for checking
  for leftover resources after a live run, e.g. `ovhcloud cloud project list`. Install pinned
  through mise: `mise use -g github:ovh/ovhcloud-cli@0.15.0` (set `GITHUB_TOKEN=$(gh auth token)`
  if GitHub rate-limits the download).

## Working rules

- Work on a branch in a git worktree; land on `main` by rebase-merge, no squash, no merge
  commits.
- Commit messages: `:gitmoji: type(scope): summary` with an ASCII gitmoji shortcode; types
  `feat fix docs style refactor test chore ci build`; one logical change per commit.
- Nothing in commits, pull requests or committed files names the tool or model that authored it.
- Claims about OVHcloud behaviour cite a source (docs.ovhcloud.com guide, API schema or provider
  docs) or are marked UNVERIFIED.
