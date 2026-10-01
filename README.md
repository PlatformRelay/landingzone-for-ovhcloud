# OVHcloud Landing Zone Accelerator

> **Unofficial.** This is an independent, community project by PlatformRelay. It is **not** an
> OVHcloud product, is **not** endorsed, sponsored or supported by OVHcloud, and OVHcloud will not
> help you with it. "OVHcloud" and related names are trademarks of their owner and are used here
> only to say which platform the code targets.

**Status: design phase.** No modules exist yet. The design documents come first and are reviewed
several times in independent sessions before any code is written.

## What this will be

An open-source set of OpenTofu/Terraform modules, blueprints and pipeline templates for building a
governed landing zone on OVHcloud Public Cloud: project factory, IAM and naming conventions,
networking, observability and pipeline-side guardrails, with extensive automated testing and
documentation.

OVHcloud has no management-group hierarchy and no organisation-wide preventive policy engine, so
this is not a port of Azure's CAF or AWS's Landing Zone Accelerator. What it does instead is part
of the design and will be documented up front.

## Layout

Monorepo. The structure is decided in the design documents, not here.
