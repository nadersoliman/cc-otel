---
adr: "0001"
title: "CI Quality Gates"
status: "proposed"
date: "2026-02-14"
author: "Claude Opus 4.6"
---

# ADR-0001: CI Quality Gates

## Problem

The repo has no CI checks. Configuration errors — malformed JSON, missing provisioning entries, duplicate dashboard UIDs — are only caught at runtime when Grafana fails to load a dashboard. Today's session surfaced a concrete example: the Prometheus costs dashboard needed entries in both `claude-code-dashboards.yaml` and `docker-compose.yml`, and forgetting either would silently break provisioning.

As the number of dashboards and config files grows, manual verification becomes unreliable.

## Proposed Solution

Three tiers of checks, ordered by speed and complexity. Each tier builds on the previous one.

---

### Tier 1 — Static Lint Checks

Fast checks that validate file syntax. No Docker required. Should run on every push and PR.

| Check | What it catches | Tool |
|-------|----------------|------|
| JSON validation | Trailing commas, missing brackets, invalid escapes in dashboard files | `jq empty dashboards/*.json` |
| YAML validation | Indentation errors, invalid syntax in config files | `yamllint` or `python -c 'import yaml; yaml.safe_load(open(f))'` |
| Docker Compose validation | Invalid service definitions, bad volume paths, unknown keys | `docker compose config --quiet` |
| ShellCheck | Unquoted variables, bash pitfalls in hook scripts | `shellcheck hooks/*.sh` |

**Estimated run time:** < 10 seconds.

---

### Tier 2 — Structural Consistency Checks

Custom checks that enforce repo conventions. Still fast, still no Docker at runtime.

#### 2a. Dashboard UID Uniqueness

Extract `uid` from every `dashboards/*.json` and fail if any duplicates exist. Duplicate UIDs cause Grafana to silently overwrite one dashboard with another.

```bash
# Pseudocode
jq -r '.uid' dashboards/*.json | sort | uniq -d | grep . && exit 1
```

#### 2b. Provisioning Completeness

Every `dashboards/*.json` file must have:
1. A matching provider entry in `dashboards/claude-code-dashboards.yaml` (path references the filename)
2. A matching volume mount in `docker-compose.yml` (binds the file into the Grafana container)

Missing either one means the dashboard exists on disk but never reaches Grafana — a silent failure.

```bash
# Pseudocode: for each JSON file, check both provisioning and volume mount exist
for f in dashboards/*.json; do
  basename=$(basename "$f")
  grep -q "$basename" dashboards/claude-code-dashboards.yaml || fail
  grep -q "$basename" docker-compose.yml || fail
done
```

#### 2c. Required Dashboard Fields

Every dashboard JSON must contain:
- `uid` (non-empty, unique — covered by 2a)
- `title` (non-empty)
- `panels` (array, non-empty)
- `schemaVersion`
- A `$service_name` template variable (repo convention — all dashboards filter by service)

```bash
# Pseudocode
jq 'select(.uid and .title and (.panels | length > 0) and .schemaVersion
    and (.templating.list[] | select(.name == "service_name")))' "$f"
```

**Estimated run time:** < 5 seconds.

---

### Tier 3 — Container Smoke Test

Spins up the full stack, waits for health, and validates dashboards load via the Grafana API.

#### Steps:
1. `docker compose up -d`
2. Wait for all 6 containers to report healthy (poll `docker compose ps` or use `--wait`)
3. For each provisioned dashboard UID, hit `GET /api/dashboards/uid/<uid>` and assert HTTP 200
4. `docker compose down`

#### What it catches:
- Image version breakage (pinned tags removed from registry)
- Port conflicts or networking issues
- Config errors that only surface at Grafana/Loki/Prometheus startup
- Provisioning mismatches that pass static checks but fail at runtime

#### Trade-offs:
- **Slow** — Docker pull + startup takes 30-60 seconds, more on cold cache
- **Requires Docker** — GitHub Actions supports this, but it adds runner cost
- **Flaky risk** — Container startup timing, network pulls

#### Recommendation:
Run on `main` merges only (not on every PR push). Alternatively, run on a nightly schedule. This keeps PR feedback fast while still catching runtime issues before they linger.

**Estimated run time:** 60-90 seconds.

---

## What We Skip

| Idea | Why not |
|------|---------|
| LogQL query validation | No standalone LogQL linter exists. Would require a running Loki instance, which overlaps with Tier 3 smoke test. |
| Panel rendering tests | Generating fake telemetry to verify panels actually display data is high effort for a personal observability stack. |
| Prometheus query validation | Same issue as LogQL — needs a running Prometheus with loaded data. |

## Implementation

All checks would live in a single GitHub Actions workflow file (`.github/workflows/ci.yml`). Tier 1 and 2 run as one job (no Docker dependency). Tier 3 runs as a separate job gated on branch (`main` only) or triggered manually.

## Open Questions

1. **yamllint config** — Should we enforce a specific YAML style (indentation, line length) or just validate syntax?
2. **Tier 3 trigger** — `main` only, nightly schedule, or manual dispatch?
3. **Dashboard field strictness** — Should we enforce additional fields beyond the minimum (e.g., `tags` must include `claude-code`, `refresh` must be set)?
