# Known issues

Known defects, security gaps, accepted limitations and debt in the OVHcloud Landing
Zone Accelerator. Each entry records what is wrong, how it was checked and what
would close it. An optional issue link can track delivery; no external tracker is required.

Read this index before changing the areas it names to avoid duplicate findings.

## Rules

- Use one `KI-NNN-short-slug.md` file per issue, following [TEMPLATE.md](TEMPLATE.md).
  IDs are never reused. Add its row below, ordered by severity then ID.
- Every entry must be re-checkable. `verified.at` pins the commit and `verified.how`
  distinguishes code reading from executed tests; `claimed` means not re-derived.
  Pin source line references to that commit. Do not present planned evidence as proof.
- Accepted limitations use `kind: limitation` and `status: accepted`. Keep the dated
  decision, rationale and reassessment conditions in the entry itself; `decision`
  links to that section so the record is usable on its own.
- The change that fixes an issue removes its entry and index row after verification.
  Git history retains the record. Acceptance alone does not establish a fix.
- Severity is derived from likelihood and impact using this matrix:

  | likelihood ↓ / impact → | low | medium | high |
  |---|---|---|---|
  | **high** | medium | high | critical |
  | **medium** | low | medium | high |
  | **low** | note | low | medium |

## Index

| Id | Severity | Kind | Status | Title | Issue |
|---|---|---|---|---|---|
| [KI-001](KI-001-ci-source-admission-unqualified.md) | medium | limitation | accepted | CI workflow/evaluator source admission is unqualified (C2) | |
| [KI-002](KI-002-offline-entry-memory-process-limits.md) | low | security | open | The offline entry bounds disk and time but not memory or process count | |
