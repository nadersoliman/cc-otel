# CLAUDE.md

Architecture Decision Records (ADRs) for the cc-otel observability stack. See root `CLAUDE.md` for overall architecture.

## Format

ADRs are numbered sequentially as `0001-short-title.md`, `0002-short-title.md`, etc. Each ADR has YAML front matter with required fields:

```yaml
---
adr: "NNNN"
title: "Short descriptive title"
status: "proposed | accepted | implemented | superseded | deprecated"
date: "YYYY-MM-DD"
author: "Name"
supersedes: "NNNN"  # optional — ADR number this replaces
superseded_by: "NNNN"  # optional — ADR number that replaced this
---
```

### Status Lifecycle

- **proposed** — Under discussion, awaiting feedback
- **accepted** — Approved, ready for implementation
- **implemented** — Fully built and live
- **superseded** — Replaced by a newer ADR (set `superseded_by`)
- **deprecated** — No longer relevant, not replaced

## ADR Index

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [0001](0001-ci-quality-gates.md) | CI Quality Gates | proposed | 2026-02-14 |

## When to Write an ADR

- Introducing a new architectural pattern or convention
- Choosing between alternatives with meaningful trade-offs
- Changing an existing convention or workflow
- Adding infrastructure (CI, tooling, new services)

ADRs are not needed for routine dashboard additions, config tweaks, or bug fixes.
