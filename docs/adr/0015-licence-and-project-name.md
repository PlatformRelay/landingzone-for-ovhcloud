# ADR-0015: Licence and project name
- Status: Proposed
- Date: 2026-10-01
- Related: ADR-0001, ADR-0014

## Context
**Licence.** The `ovh/ovh` provider is MPL-2.0. Other PlatformRelay repos: _check each repo's LICENSE
before deciding_. The accelerator contains code (modules, tools), policies, schemas and documentation.
**Name.** Working name `ovh-landing-zone-accelerator`. Using a third party's trademark in a project name
can imply endorsement; descriptive/nominative use with an unofficial notice is common but not risk-free.
Terraform registry convention for mirrored modules is `terraform-ovh-<name>`, which uses the provider
name by necessity. The word "accelerator" echoes AWS's and Azure's product names.

## Options considered
Licence: **Apache-2.0** (patent grant, widely accepted by enterprises) · **MPL-2.0** (file-level copyleft,
matches the provider) · MIT (minimal). Docs: same licence or CC BY 4.0 (avoid NC/SA mixing with ADR-0014).
Name: `ovh-landing-zone-accelerator` · `landing-zone-for-ovhcloud` (nominative, reads as "for") ·
neutral brand (e.g. `<brand>-lz` with "for OVHcloud" in the tagline) · keep generic `landing-zone-ovh`.

## Recommendation (for the operator to accept or overrule)
- Licence **Apache-2.0** for code, policies and schemas; docs under **CC BY 4.0**. Rationale: enterprises
  adopt it with least friction, patent grant, compatible with MPL-2.0 provider use (the provider is a
  dependency, not linked source).
- Name: **keep `ovh-landing-zone-accelerator` for now**, with the unofficial notice, because the repo is
  private and cheap to rename before first publication; revisit before the first public release after a
  trademark-risk read of OVHcloud's brand policy. A neutral brand is the safer long-term option.
- Provenance and the applicable licence are recorded for any retained third-party material;
  attribution never overrides its licence. Original executable examples carry the code licence even
  when included in documentation; REUSE-compatible file-level metadata resolves mixed and generated
  content. A published trademark-use decision precedes the first public release.

## Consequences
- Choosing Apache-2.0 closes off copyleft protection against closed forks.
- A rename after publication is costly (module sources, links); decide before the first public tag.

## Counterpoints
- MPL-2.0 would keep improvements to modules open at file level; chosen against because enterprise
  legal teams often prefer Apache-2.0 and adoption is the goal. The operator may weigh it differently.

## Verification
- Read OVHcloud's trademark/brand usage policy; check what other community OVH projects do.

## Review log
- 2026-10-01: round-2 external adversarial review applied.

## Name decision (2026-10-04)
The operator chose **`landingzone-for-ovhcloud`** (display title "Landing Zone for OVHcloud"), the
nominative "for" form, and made the repository public the same day. The former name
`ovh-landing-zone-accelerator` redirects on GitHub, and the dependency check still treats it as
this repository. The unofficial notice heads README and NOTICE. A neutral brand remains the safer
long-term option and was considered; the licence recommendation is unaffected.
