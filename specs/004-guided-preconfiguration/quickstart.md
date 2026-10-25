# Quickstart: Answers file to deployments.yaml

**Planned, not runnable yet.** `test:configure` arrives with T016, `configure:check` and
`configure:export` with T020. Until then, edit `stacks/deployments.yaml` by hand as
[run the first slice](../../docs/how-to/run-the-first-slice.md) describes.

## Offline (any contributor, no credentials, no network)
Through the offline entry, from `$HOME`:

```sh
lz-offline --candidate <checkout> -- task test:configure
lz-offline --candidate <checkout> -- task configure:check -- tests/fixtures/answers/sandbox.yaml
lz-offline --candidate <checkout> -- task check:specs -- specs/004-guided-preconfiguration
```

## An operator's own answers file (host-side, still no credentials)
The entry does not see `.local/`, so run these on the host:

```sh
mkdir -p .local/configure
cp tests/fixtures/answers/sandbox.yaml .local/configure/answers.yaml   # then edit it
mise exec -- task configure:check -- .local/configure/answers.yaml     # diff, notes, or a plain refusal; writes nothing
mise exec -- task configure:export -- .local/configure/answers.yaml    # writes .local/configure/deployments.yaml only
cp .local/configure/deployments.yaml stacks/deployments.yaml           # your reviewed step
mise exec -- task stacks:reconcile
mise exec -- task stacks:generate
lz-offline --candidate <checkout> -- task stacks:check                  # STACKS_CHECK_OK
```

A refusal reads `configure: <CODE>: <where>. <what it means>. Next: <what to do>`. The notes list
the `LZ_PROJECT_ID_<REF>` names your `account.env` needs; the command never reads that file.

## What changes for the owner
One choice is open, with a default: the export writes only the staging file, not
`stacks/deployments.yaml` (spec *Clarifications*). T022 needs a second reader.
