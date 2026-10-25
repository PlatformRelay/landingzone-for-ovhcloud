---
references:
  - path: tools/cmd/lz-configure/main.go
    blob: 1c7c17842d0badfd155a037e745041c289b938c6
  - path: tools/internal/configure/answers.go
    blob: 47b21c94ba559dc0ccde021c3c09623e86d8e79c
  - path: tools/internal/configure/export.go
    blob: 01c3a294cd586723d620f826019c60dd76426b2b
  - path: tools/internal/configure/diff.go
    blob: f9b66733c983ac1d65593251cbd8004f0c6d5c68
  - path: tools/internal/configure/messages.go
    blob: 8abb3161c2ec48b3e4343777083ca2a33b482ad1
  - path: tools/internal/configure/notes.go
    blob: 18b4a3a45d031b7c408f7fbd9832c1debc58cc6a
  - path: tests/fixtures/answers/sandbox.yaml
    blob: 8f1488b84f0e509c9065a197c4c352b81bc8fc73
  - path: specs/004-guided-preconfiguration/spec.md
    blob: 901d053342e31299c95e5cc44c9febaf914ae723
  - path: Taskfile.yml
    blob: 7f63fcd4c12db8be85537b62f49efbd11f368210
verified_against: {task: "3.53"}
last_verified: 523d965edfb57dc698f61e0fa3b46b8236025972
---

# Prepare deployments.yaml from an answers file

This guide is for an operator who has not written `stacks/deployments.yaml` before. You write a
short answers file. One command turns it into a complete, checked manifest. You review it, copy it
into place and run the three stack commands.

Nothing here deploys anything. No step needs a credential, `account.env` or the network. Applying
the result to an OVHcloud account is a separate step that only the maintainer runs today
([run the first slice](run-the-first-slice.md)).

The design is in [spec 004](../../specs/004-guided-preconfiguration/spec.md). The interactive
setup journey is not built; the [guided setup walkthrough](../explanation/guided-setup-walkthrough.md)
explains why.

## What the answers file makes easier

- Fewer values: 11 for the sandbox and 15 for two tenants, against 41 and 67 in the manifest.
- No instance rows. Their ids, stages, dimensions and the tenant-state rows are derived for you.
- Defaults for the fields that rarely change: the header, the scope, the stage source, the state
  ref and endpoint, the project ref and mode, VLAN 0, and both toggles off.
- Each refusal is one plain sentence with the next action.
- A dry-run diff of the instance rows added and removed. Each row says what `stacks:reconcile` is
  expected to do, judged against the current manifest.

## Where it does not help

- If you already know the manifest well, you gain little beyond the diff and the notes.
- The diff shows rows only. A changed CIDR, VLAN id, toggle or header value is not listed.
- The diff's prediction assumes the stack tree matches the current manifest.
- Some manifests cannot be expressed: tenant scope, `external` entries, runtime slots, a region
  without a runtime row, or custom instance ids. Edit those by hand.
- Renames and removals stay refused by `stacks:reconcile`, with or without the answers file.
- The project ids still go into `account.env` by hand. The command lists only the names.
- Comments in a hand-edited manifest are not carried over.
- The file uses the JSON-like flow form of YAML, not block YAML.

## Before you start

- A clone of this repository, with the pinned tools installed: run `mise trust`, then
  `mise install`, at its root. Without the trust, `mise exec` refuses the clone's `mise.toml`.
- Run every command below from the repository root, on your own machine. The offline entry
  (`lz-offline`) does not see `.local/`, so these commands run host-side.
- Your answers file lives in `.local/configure/`. That directory is not committed. The
  answers file is a local input; `stacks/deployments.yaml` stays the one source of truth.

## The answers file

The file is YAML in its JSON-compatible flow form: braces, brackets, double quotes and commas.
Comments are whole lines that start with `#`. A comment after a value on the same line is refused.
Keys are lower case with `_`, except `apiVersion`. Each key appears once. The file is at most 64 KiB.

| Field | Required | Form | Default |
| --- | --- | --- | --- |
| `apiVersion` | yes | `lz.platformrelay.dev/v1alpha1` | none |
| `kind` | yes | `DeploymentAnswers` | none |
| `name` | yes | lower case letter, then up to 62 of `a-z`, `0-9`, `-` | none |
| `org` | yes | lower case letter, then up to 15 of `a-z`, `0-9` | none |
| `forge` | yes | host and path, such as `github.com/owner/repo` | none |
| `state_region` | yes | lower case letters, such as `gra` | none |
| `state_endpoint` | no | `https://` and a host name | `https://s3.<state_region>.io.cloud.ovh.net` |
| `state_ref` | no | upper case letter, then up to 62 of `A-Z`, `0-9`, `_` | `STATE` |
| `shared_state_project` | no | `true` or `false` | `false` |
| `tenants` | yes | a list of tenants; may be empty | none |
| tenant `name` | yes | as `org` | none |
| tenant `environments` | yes | a list of environments; may be empty | none |
| environment `name` | yes | as `org` | none |
| environment `regions` | yes | a list of regions; may be empty | none |
| environment `project_ref` | no | as `state_ref` | `<TENANT>_<ENV>`, such as `DEMO_DEV` |
| environment `project_mode` | no | `reference` or `adopt` | `reference` |
| environment `budget_alert` | no | `true` or `false` | `false` |
| environment `quota_guard` | no | `true` or `false` | `false` |
| region `name` | yes | upper case letters, then digits, such as `GRA11` | none |
| region `cidr` | no | an IPv4 range with its prefix, such as `10.40.0.0/24` | no network |
| region `vlan_id` | no | a whole number from 0 to 4000; needs `cidr` | `0` |

`null` is not a value: leave an optional field out instead. A region without `cidr` gets no
private network, only its runtime row.

## 1. Start from the sandbox answers

Copy the sandbox answers file. Its comments explain the two values you are most likely to change.

```sh
mkdir -p -m 0700 .local/configure
cp tests/fixtures/answers/sandbox.yaml .local/configure/answers.yaml
```

Run the check once on the copy, unchanged:

```sh
mise exec -- task configure:check -- .local/configure/answers.yaml
```

It prints `diff: none`, seven `note:` lines and `configure: ok`. The copy describes exactly the
committed manifest. The check writes nothing.

## 2. Add a tenant

Open `.local/configure/answers.yaml`. Add a second tenant after `demo`, inside the `tenants` list.
Mind the comma after the closing brace of `demo`:

```yaml
    },
    {
      "name": "lab",
      "environments": [
        {
          "name": "dev",
          "regions": [
            {"name": "GRA11", "cidr": "10.40.0.0"}
          ]
        }
      ]
    }
  ]
}
```

This example contains a mistake on purpose. The next step shows how the check reports it.

## 3. Check, and fix a refusal

Run the check again:

```sh
mise exec -- task configure:check -- .local/configure/answers.yaml
```

It refuses the file with one line:

```text
configure: INVALID_VALUE: tenants[1].environments[0].regions[0].cidr. A value does not have the allowed form. Next: Use the form the how-to gives for that field.
```

Every refusal has this shape: `configure: <CODE>: <where>. <what it means>. Next: <what to do>`.
The place counts from 0, so `tenants[1]` is the second tenant, `lab`. The table in
[the answers file](#the-answers-file) says a CIDR needs its prefix. `task` then reports
`task: Failed to run task "configure:check": exit status 1`. Nothing was written.

Fix the value to `"10.40.0.0/24"` and run the check again.

Some refusals name a manifest rule, not a field. For example, `DUPLICATE_VLAN` means two regions of
one environment share a VLAN id, and `SHARED_STATE_PROJECT` means an environment uses the state
project without `shared_state_project`. The sentence and the `Next:` action say what to change.
A refusal that calls itself a defect of the tool is not your mistake: report it with the answers
file, and edit `stacks/deployments.yaml` by hand meanwhile.

## 4. Read the diff

With the fix, the check prints the rows it adds or removes, by id:

```text
+ row lab-state (tenant-state lab): expected: created by stacks:reconcile
+ row lab-dev-project (project lab/dev): expected: created by stacks:reconcile
+ row lab-dev-gra11-network (project-network lab/dev/GRA11): expected: created by stacks:reconcile
+ row lab-dev-gra11-runtime (runtime lab/dev/GRA11): expected: created by stacks:reconcile
diff: 4 added, 0 removed
```

The diff compares your answers with today's `stacks/deployments.yaml`. A `+` row is new, and
`stacks:reconcile` is expected to create its stack. A `-` row would be removed. `stacks:reconcile`
refuses removals, so a `-` row is marked `expected: refused by stacks:reconcile
(UNSUPPORTED_CHANGE)`. The check then prints a warning line, such as
`configure: warning: stacks:reconcile is expected to refuse this export (1 removed rows)`.
It still ends in `configure: ok`, because the answers themselves are valid. Copied, such an export
stops at `stacks:reconcile`. Removing a stack is a hand edit for the maintainer, not a step of
this guide.

Changes inside a kept row are not shown: a new CIDR, VLAN id, toggle or header value.

## 5. Read the consequence notes

After the diff come the notes, then `configure: ok`. For the two-tenant answers they say:

- `account.env needs LZ_PROJECT_ID_STATE, LZ_PROJECT_ID_DEMO_DEV, LZ_PROJECT_ID_LAB_DEV`. These
  are names only. The command never reads `account.env`. The maintainer adds the new project id.
- One project per tenant and environment is the only isolation boundary. Tags do not scope bucket
  access. Two refs whose project ids name one project are refused at bind. So `configure: ok` and
  a green `stacks:check` do not mean the bind passes. The sandbox account has one project, so
  `lab/dev` needs a second project before this manifest can be applied there. Sources: D107, after
  the live probe that refuted tag-scoped bucket access (P25 in [spec 005](../../specs/005-first-landing-zone-slice/spec.md)); the bind
  refusal is A17 in [spec 007](../../specs/007-cleanup-first-live-slice/spec.md).
- KD-1, because `shared_state_project` is `true`: the state buckets share a project with tenant
  resources. Tenant state isolation is not claimed.
- The project mode of each environment. `reference` uses the existing project. `adopt` plans a
  replacement of it.
- Each private network in a project needs its own VLAN id
  ([spec 005 T098](../../specs/005-first-landing-zone-slice/evidence/T098.md)).
- Bucket names are unique across all of OVHcloud. `org` keeps them apart. A taken name fails at
  apply time; change `org` if that happens. Source: the OVHcloud Object Storage S3 limitations
  guide, P21 in [spec 005](../../specs/005-first-landing-zone-slice/spec.md).
- State buckets are Object-Locked in governance mode (D104). Tearing one down needs the
  governance bypass or the end of the retention. That teardown path has not been run on OVHcloud
  (UNVERIFIED).

A note appears only when its trigger holds. The KD-1 note, for example, needs
`shared_state_project`.

## 6. Export

```sh
mise exec -- task configure:export -- .local/configure/answers.yaml
```

The export runs the same check, then writes `.local/configure/deployments.yaml` and nothing else.
The directory ends with mode 0700 and the file with 0600. After `configure: ok` it prints:

```text
wrote .local/configure/deployments.yaml
next: copy .local/configure/deployments.yaml to stacks/deployments.yaml
next: task stacks:reconcile
next: task stacks:generate
next: task stacks:check
```

If the write fails, the export prints only the refusal line. `STAGING_WRITE` means the previous
staging file is unchanged. `STAGING_NOT_DURABLE` means the new file was written, but a crash could
bring the old one back: look at the file before you copy it, or export again. `STAGING_SYMLINK`
means `.local`, `.local/configure` or the staging file is a symlink; remove it and export again.

## 7. Review and copy

The staging file is the complete manifest. Read it before you copy it. Compare it with the current
one, for example with `diff stacks/deployments.yaml .local/configure/deployments.yaml`. Expect
the new tenant, its environment and region, and the four instance rows from the diff. The export
is equal in value to a hand-written manifest, not byte for byte: comments and layout may differ.

Then copy it. This is your step; the command never writes `stacks/`.

```sh
cp .local/configure/deployments.yaml stacks/deployments.yaml
```

## 8. Reconcile, generate and check the stacks

```sh
mise exec -- task stacks:reconcile
mise exec -- task stacks:generate
mise exec -- task stacks:check
```

- `stacks:reconcile` creates the stack directories the manifest names and the tree lacks. It
  refuses every other mismatch, such as a removed or renamed row.
- `stacks:generate` writes each stack's generated files with the pinned Terramate. If it reports
  another Terramate version, set `LZ_TERRAMATE` to the absolute path of mise's install.
- `stacks:check` compares the tree with the manifest and prints `STACKS_CHECK_OK` when they match.

## What to do next

- Commit `stacks/deployments.yaml` with the new and generated stack files on a branch. Open a pull
  request; CI runs `stacks:check` again.
- Tell the maintainer which `LZ_PROJECT_ID_<REF>` names are new. They fill in the project ids in
  `account.env` ([local files](run-the-first-slice.md#local-files-never-in-a-repository)) and run
  any apply.
- Keep your answers file if you will change the manifest again. It is the input for the next
  export; it is never committed.

## When a line is not a refusal of your answers

Two lines stop the check without blaming the content of your answers:

| Line | Meaning | What to do |
| --- | --- | --- |
| `configure: CURRENT_MANIFEST_INVALID: …` | Today's `stacks/deployments.yaml` does not decode. The check and the export refuse, exit 1: no diff, no notes, nothing written. | Run `mise exec -- task stacks:check`; it names the problem. Fix the manifest first. |
| `configure: error: <error>` | An I/O failure, such as an answers file you may not read. It is not in the catalogue, and exit is 1. | Fix what the error names, often a permission, and run the command again. |

A usage mistake, such as a missing path, prints the usage line and exits 2.

## Check again

After the copy, your answers and the manifest describe the same rows. Confirm it:

```sh
mise exec -- task configure:check -- .local/configure/answers.yaml
```

It prints `diff: none`, the notes and `configure: ok`.
