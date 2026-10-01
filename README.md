# OVHcloud Landing Zone Accelerator

> **Unofficial.** This is an independent, community project by PlatformRelay. It is **not** an
> OVHcloud product, is **not** endorsed, sponsored or supported by OVHcloud, and OVHcloud will not
> help you with it. "OVHcloud" and related names are trademarks of their owner and are used here
> only to say which platform the code targets.

**Status: design phase.** No modules exist yet. The design documents come first and are reviewed
several times in independent sessions before any code is written.

## What this will be

An open-source set of OpenTofu/Terraform modules, composable components, golden-path profiles and
pipeline templates for building a governed landing zone on OVHcloud Public Cloud: a declarative
tenant model with self-service merge requests gated by [assent](https://github.com/PlatformRelay/assent),
identity across the OVHcloud IAM, OpenStack and Kubernetes planes with a pluggable identity
provider, naming conventions, networking, observability, cost and quota guardrails, with testing at
every layer and documentation that is checked for rot.

It is opinionated at the seams (identity, state, pipeline, naming, guardrails, audit) and free in the
middle: several **golden paths** (solo, team on Managed Kubernetes, team on VMs, federated
organisation, regulated) are supported from one codebase, and several base stacks and IAM providers
are first-class.

OVHcloud has no management-group hierarchy and no organisation-wide policy engine, so this is not a
port of Azure's CAF or AWS's Landing Zone Accelerator. What it does instead, and what it cannot do,
is part of the design and is documented up front.

## Layout

Monorepo. The proposed structure and every other design decision are in
[`docs/adr/`](docs/adr/README.md); all are still `Proposed` until independently reviewed.
