# Plan: Migrate Cost & Token Dashboards from Prometheus to Loki

**Status:** Implemented
**Date:** 2026-02-07

## Problem

Prometheus `increase()` on cumulative counters produces **phantom costs** after container restarts. The WAL (Write-Ahead Log) replays old counter values that interleave with live data, creating ~60 phantom counter resets per hour. Each reset adds the pre-reset value to `increase()`, producing $532/hour during idle time (actual: $0). See [Experiment 10](../experiments/dashboard-query-experiments.md#experiment-10-phantom-532hour--counter-resets-from-wal-interleaving).

This is a fundamental incompatibility between Prometheus cumulative counters and processes that survive container restarts. It cannot be fixed by query tuning.

## Solution

Replace all Prometheus counter queries in the Costs and Tokens dashboards with **Loki log-based aggregation** using `sum_over_time({} | unwrap <field>)`.

Each `api_request` log event already contains `cost_usd`, `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_creation_tokens` as individual fields. These are independent log lines — no counters, no resets, no staleness, no warm-up delay.

### Verified working queries

```
# Total cost (24h) — returned $42.32 (correct) vs Prometheus's $2,760 (phantom)
sum(sum_over_time({service_name="claude-code"} | event_name = `api_request` | unwrap cost_usd [24h]))

# Cost per hour by model
sum by (model) (sum_over_time({service_name=~"$service_name"} | event_name = `api_request` | unwrap cost_usd [1h]))

# Cost per hour by service
sum by (service_name) (sum_over_time({service_name=~"$service_name"} | event_name = `api_request` | unwrap cost_usd [1h]))

# Input tokens per hour by model
sum by (model) (sum_over_time({service_name=~"$service_name"} | event_name = `api_request` | unwrap input_tokens [1h]))
```

---

## Migration: Costs Dashboard (`claude-code-costs.json`)

### Panel: Total Cost (stat)

```
# Before (Prometheus — broken)
sum(increase(claude_code_cost_usage_USD_total{service_name=~"$service_name"}[$__range]))

# After (Loki)
sum(sum_over_time({service_name=~"$service_name"} | event_name = `api_request` | unwrap cost_usd [$__range]))
```
Datasource: change from `prometheus` to `loki`

### Panel: Cost by Model (stat)

```
# Before
sum by (model) (increase(claude_code_cost_usage_USD_total{service_name=~"$service_name"}[$__range]))

# After
sum by (model) (sum_over_time({service_name=~"$service_name"} | event_name = `api_request` | unwrap cost_usd [$__range]))
```

### Panel: Cost by Service (stat)

```
# Before
sum by (service_name) (increase(claude_code_cost_usage_USD_total{service_name=~"$service_name"}[$__range]))

# After
sum by (service_name) (sum_over_time({service_name=~"$service_name"} | event_name = `api_request` | unwrap cost_usd [$__range]))
```

### Panel: Cost per Hour by Model (barchart)

```
# Before
sum by (model) (increase(claude_code_cost_usage_USD_total{service_name=~"$service_name"}[1h]))

# After
sum by (model) (sum_over_time({service_name=~"$service_name"} | event_name = `api_request` | unwrap cost_usd [1h]))
```
`interval: "1h"` stays the same.

### Panel: Cost per Hour by Service (barchart)

```
# Before
sum by (service_name) (increase(claude_code_cost_usage_USD_total{service_name=~"$service_name"}[1h]))

# After
sum by (service_name) (sum_over_time({service_name=~"$service_name"} | event_name = `api_request` | unwrap cost_usd [1h]))
```

---

## Migration: Tokens Dashboard (`claude-code-tokens.json`)

### Stat panels (4 panels: input, output, cacheRead, cacheCreation)

```
# Before (e.g., input tokens)
sum(increase(claude_code_token_usage_tokens_total{service_name=~"$service_name",type="input"}[$__range]))

# After
sum(sum_over_time({service_name=~"$service_name"} | event_name = `api_request` | unwrap input_tokens [$__range]))
```

Field mapping:

| Prometheus label `type=` | Loki field to `unwrap` |
|--------------------------|----------------------|
| `input` | `input_tokens` |
| `output` | `output_tokens` |
| `cacheRead` | `cache_read_tokens` |
| `cacheCreation` | `cache_creation_tokens` |

### Bar chart panels (4 panels: per-hour by model for each token type)

```
# Before (e.g., input tokens by model)
sum by (model) (increase(claude_code_token_usage_tokens_total{service_name=~"$service_name",type="input"}[1h]))

# After
sum by (model) (sum_over_time({service_name=~"$service_name"} | event_name = `api_request` | unwrap input_tokens [1h]))
```

---

## What Stays on Prometheus

Not everything needs to move. These metrics don't suffer from the phantom reset problem (low values, infrequent resets, or not affected by container restarts):

| Metric | Keep on Prometheus? | Reason |
|--------|-------------------|--------|
| `claude_code_cost_usage_USD_total` | **Move to Loki** | Phantom resets make it unusable |
| `claude_code_token_usage_tokens_total` | **Move to Loki** | Same WAL interleaving problem |
| `claude_code_session_count_total` | Keep | Low value (always 1), reset adds 1 — negligible error |
| `claude_code_active_time_seconds_total` | Keep | Used for rate monitoring, not totals |
| `claude_code_lines_of_code_count_total` | Keep | Rarely queried, low values |
| `claude_code_commit_count_total` | Keep | Rarely queried |
| `claude_code_pull_request_count_total` | Keep | Rarely queried |
| OTel Collector metrics | Keep | Collector self-scrape, no resets |

---

## Implementation Steps

### Step 1: Update Costs dashboard JSON

Change all 5 panels:
- Switch `datasource` from `{"type": "prometheus", "uid": "prometheus"}` to `{"type": "loki", "uid": "loki"}`
- Replace PromQL queries with LogQL queries (as shown above)
- Change `targets[].expr` to `targets[].expr` (same field name for Loki in Grafana)
- Keep `interval: "1h"` for bar charts
- Keep all panel types, grid positions, colors, overrides unchanged
- Bump `version`

### Step 2: Update Tokens dashboard JSON

Change all 8 panels (4 stats + 4 bar charts):
- Same datasource and query changes as Costs
- Map `type="input"` → `unwrap input_tokens`, etc.
- Bump `version`

### Step 3: Update $service_name variable

The `$service_name` variable currently queries Loki labels, so it should work as-is. Verify it works with the new Loki queries.

### Step 4: Restart container

```bash
docker compose down && docker compose up -d
```

### Step 5: Verify

- Check that cost bars show realistic values (no $532/hour)
- Check that idle hours show $0
- Check that model breakdown matches expectations
- Compare with `docker logs` or API console for sanity

---

## Files Changed

| File | Change |
|------|--------|
| `dashboards/claude-code-costs.json` | 5 panels: Prometheus → Loki queries, bump version |
| `dashboards/claude-code-tokens.json` | 8 panels: Prometheus → Loki queries, bump version |

No changes to `docker-compose.yml`, `otelcol-config.yaml`, or other dashboards.

---

## Advantages of Loki-Based Approach

| Aspect | Prometheus `increase()` | Loki `sum_over_time(unwrap)` |
|--------|------------------------|----------------------------|
| Phantom resets after container restart | Yes ($532/hour) | No |
| Warm-up delay after session start | ~60s | None (instant) |
| Stale series after session ends | Lost after 5 min | Retained permanently |
| Counter reset handling | Fragile (WAL interleaving) | N/A (no counters) |
| Data model | Cumulative counter (stateful) | Independent log events (stateless) |
| Precision | Subject to extrapolation | Exact (sum of actual values) |
| Historical queries | Limited by staleness/resets | Full history available |

## Risks / Considerations

1. **Loki query performance**: `sum_over_time` over long ranges (7d, 30d) scans all matching log lines. For a dev stack with moderate volume this is fine. For high-volume production, consider adding a Loki recording rule.

2. **Log retention**: Cost data is only available as long as Loki retains logs. Currently using persistent volume with no explicit retention policy. Default Loki retention is 30 days (configurable).

3. **Field type**: `unwrap` requires numeric fields. The `cost_usd` field in OTLP logs is a string ("0.08667"). Loki's `unwrap` handles string-to-float conversion automatically.

4. **Missing events**: If a log event is dropped (collector overloaded, Loki full), that cost is lost. With Prometheus counters, the cumulative value catches up on the next export. However, the phantom reset problem makes this theoretical advantage meaningless in practice.

5. **service_name variable**: The `$service_name` variable uses `allValue: ".+"` regex. LogQL's `{service_name=~".+"}` works the same as PromQL's regex matcher.

## Critique Findings (incorporated during implementation)

| Priority | Issue | Resolution |
|----------|-------|------------|
| **P0** | Stat panels need `"queryType": "instant"` when using Loki datasource | Added to all 7 stat panel targets |
| **P1** | Add `\| __error__=""` after `unwrap` for safety | Added to all 13 queries |
| **P1** | Verify `sum by (model)` works with structured metadata | Verified live — returns correct model breakdown (haiku + opus) |
| **P2** | Update CLAUDE.md after migration | Updated dashboard descriptions and known issues |
| **P2** | Increase auto-refresh from 10s to 30s | Changed on both dashboards |
| **P3** | Empty hours return null (no bar) not 0 | Acceptable — same as Prometheus behavior |
| **P3** | `$__range` works with Loki datasource | Confirmed by Grafana docs and live testing |

## Open Questions (resolved)

- [x] Does `sum_over_time(unwrap)` work correctly with Grafana's `$__range` variable in Loki datasource? **Yes** — verified live.
- [x] Do bar chart panels need any Grafana-specific changes when switching datasource from Prometheus to Loki? **No** — same `barchart` type with `interval: "1h"` works.
- [x] Should we remove the Prometheus counter env vars (`OTEL_METRICS_EXPORTER=otlp`) or keep them for other metrics? **Keep** — other metrics (sessions, active time, LOC, commits, PRs) still use Prometheus.
