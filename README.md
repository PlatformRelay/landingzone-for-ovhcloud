# Landing Zone for OVHcloud

An open-source landing zone for OVHcloud Public Cloud, written in OpenTofu. It sets up the shared
base that teams deploy onto (state storage, identity, projects, networks, and a runtime placeholder), and checks
each piece with tests that run offline before anything touches an account.

> [!IMPORTANT]
> **Not an OVHcloud product.** This is an independent project by PlatformRelay. OVHcloud does not
> provide, endorse or support it. "OVHcloud" is a trademark of its owner and is used here only to
> name the target platform. See [ADR-0001](docs/adr/0001-scope-positioning-and-unofficial-status.md).

## Who it is for

- **For** platform engineers who want to read, run or compare a tested landing-zone baseline for
  OVHcloud, and for contributors.
- **Not for** production use today, nor for anyone who needs vendor support, a finished product, or
  features listed below as not delivered.

## Status

Pre-release. No version has been published.

- **Delivered:** the first slice ([spec 005](specs/005-first-landing-zone-slice/spec.md)): the
  modules, components, stages and generated stacks listed in the
  [repository map](docs/reference/repository-map.md), with naming and labels. Its offline checks pass. Its
  live apply-and-destroy chain passed once in the maintainer's sandbox account (run
  `20261013T075309Z-443f`, [evidence](specs/005-first-landing-zone-slice/evidence/T049.md)).
- **Open:** bootstrapping a fresh account (task T045) has not run yet. Tenant state isolation is
  not demonstrated: the sandbox shares one project between tenants and state (see the
  [exit record](specs/005-first-landing-zone-slice/evidence/T051.md)).
- **Not delivered:** golden-path profiles, tenant self-service merge requests, per-run deployment
  credentials, generated installation docs and the guided setup are plans, described in the
  [product direction](docs/explanation/product-direction.md).

## Try it offline (about 5 minutes, no credentials)

Every check runs inside `lz-offline`, a sandboxed entry with the pinned toolchain (OpenTofu,
Terramate, Go, TFLint) and no network. You need Linux on x86-64, `curl`, `jq`, `tar`, `sha256sum`
and `sudo`. The install step replaces `/usr/bin/bwrap` with a pinned build and, where AppArmor
restricts them (recent Ubuntu), allows unprivileged user namespaces: prefer a VM or a spare machine.

1. **Get the entry.** It is published by digest on GitHub's container registry. In a shell, export
   the five `LZ_IMAGE`, `LZ_MANIFEST`, `LZ_LAYER`, `LZ_ENTRY_SHA256` and `LZ_BWRAP_SHA256` values
   from the `env:` block of [`.github/workflows/foundation.yml`](.github/workflows/foundation.yml),
   then set two directories **outside your clone** (the entry refuses to run from inside it):

   ```sh
   export RUNNER_TEMP=$(mktemp -d) LZ_RUNTIME=$HOME/lz-runtime   # LZ_RUNTIME must not exist yet
   ```

   Then run the `run:` scripts of the steps *Fetch the runtime layer by digest* and *Install the
   pinned launcher* from the same workflow.
2. **Run the checks** from the root of your clone:

```sh
E=$LZ_RUNTIME/runtime/lz-offline
"$E" --candidate "$PWD" -- task test:slice         # lint + unit tests of every module, component, stage (~80 s)
"$E" --candidate "$PWD" -- task stacks:check       # generated stacks match stacks/deployments.yaml
"$E" --candidate "$PWD" -- task test:stack-plans   # every stack plans against mocks
"$E" --candidate "$PWD" -- task stacks:order -- all  # the order the stacks would be applied in
```

A green run ends with `SLICE_PASS` (`test:slice`) or `pass`; `stacks:order` prints the order.
With [Task](https://taskfile.dev) installed, `task --list` shows every target; CI runs the ones
in `LZ_TARGETS` of the same workflow. Applying to a real account
(`task bootstrap:*`, `task live:*`) is maintainer-only today; see the
[runbook](docs/how-to/run-the-first-slice.md).

## Map of the repository

The short version; the [repository map](docs/reference/repository-map.md) has every entry and its
subdirectories, and the [architecture overview](docs/explanation/architecture-overview.md) shows how
the layers fit together.

**The landing zone** (what gets deployed)

| Path | What it holds |
| --- | --- |
| `modules/` | Layer 1: small building blocks over OVHcloud resources (IAM, storage, network), plus naming. |
| `components/` | Layer 2: building blocks composed from modules (state backend, identity, network, runtime). |
| `stages/` | Layer 3: one reusable module per deployment step, without state or credentials: bootstrap, tenant state, account governance, project, network, runtime. |
| `stacks/` | Deployable instances of the stages, each with its own state, generated from `stacks/deployments.yaml`. |
| `schemas/` | JSON schemas for the deployment manifest, stage outputs and check reports. |

**How it is verified**

| Path | What it holds |
| --- | --- |
| `tools/` | Go command-line tools: `lz-check` (checks), `lz-offline` (sandboxed entry), `lz-stacks` (stack generation), `lz-live` (live runs), `lz-pack` (runtime packing). |
| `harness/` | The check registry (`checks.yaml`) and what the offline entry may do (`capabilities.yaml`). |
| `tests/` | Check fixtures, captured tool output and the live probe roots. |
| `pipelines/` | The CI source the GitHub workflow is rendered from, and recorded CI runs. |
| `Taskfile.yml`, `mise.toml` | Every check as `task <target>`; the pinned tool versions. |

**Why it is the way it is, and the delivery record**

| Path | What it holds |
| --- | --- |
| `docs/` | Architecture decision records (ADRs), how-tos, explanations, references and known issues. |
| `specs/` | The delivery record: one folder per feature with its spec, plan, tasks and evidence. You do not need it to use the landing zone. |
| `AGENTS.md`, `.github/CONTRIBUTING.md` | Working rules for contributors, human or automated; how to contribute. |

## Where to go next

- [Documentation index](docs/README.md) — every page, sorted by what you need from it.
- [Glossary](docs/reference/glossary.md) — the project's terms and ids (stage, stack, KD-1, T049 …).
- [Architecture decisions](docs/adr/README.md) — start with ADR-0001, 0002, 0004, 0008 and 0009.
- [Run the first slice](docs/how-to/run-the-first-slice.md) — the maintainer's runbook for a real account.
- [Specs and delivery status](specs/README.md) — what each spec covers and where it stands.
- [Known issues](docs/known-issues/README.md) — verified findings and accepted limitations.

## Licence

Code, policies and schemas: [Apache-2.0](LICENSE). Documentation under `docs/`:
[CC BY 4.0](docs/LICENSE). Quoted OVHcloud documentation keeps its own terms; see [NOTICE](NOTICE).
