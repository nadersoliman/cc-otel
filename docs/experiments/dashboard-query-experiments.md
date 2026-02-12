# Dashboard Query Experiments: Costs & Tokens

**Date:** 2026-02-07
**Context:** Building Grafana dashboards for Claude Code's Prometheus counters (cost, tokens) and Loki logs (API requests, tool usage).

This document captures every experiment, failure, and fix we encountered while getting the Costs and Tokens dashboards to display correct data. Each section is a named experiment with the symptom, hypothesis, what we tried, what actually happened, and the fix.

---

## Experiment 1: Total Cost Shows $1.19 Instead of $4

**Symptom**: The "Total Cost" stat panel showed $1.19, but we had spent ~$4 across multiple sessions.

**Query used**: `sum(claude_code_cost_usage_USD_total)`

**Root cause**: `sum(counter)` is an **instant query** — it only sums currently active time series. Claude Code sessions are ephemeral: when a session ends, its metrics go stale in Prometheus after ~5 minutes. The $1.19 was only the cost from the one session still running. The other sessions had already disappeared.

**Why this is different from traditional Prometheus**: In a typical server setup, the process stays alive and its counters persist. With Claude Code, the CLI process starts and stops frequently, so counters come and go.

**Fix**: Use `increase()` instead of raw `sum()`:
```
# Before (broken — only shows active sessions)
sum(claude_code_cost_usage_USD_total)

# After (correct — recovers totals from all sessions in the range)
sum(increase(claude_code_cost_usage_USD_total[$__range]))
```

`increase()` reaches back through Prometheus's stored data points and computes the total increment across all sessions within the time window, even if those series are now stale.

---

## Experiment 2: Sessions Stat Always Shows 1

**Symptom**: The "Active Sessions" stat panel always showed `1`, regardless of how many sessions we'd run.

**Query used**: `sum(claude_code_session_count_total)`

**Root cause**: Each Claude Code session emits `session_count_total = 1` as a cumulative counter. `sum()` across active series naturally gives 1 (since only one session is active at a time — the current one). Past sessions are stale and excluded.

**First attempt**: `count(count by (session_id) (claude_code_session_count_total))` — count distinct session IDs. This also only counts active sessions.

**Fix**: Use `increase()` to count sessions across the range:
```
sum(increase(claude_code_session_count_total[$__range]))
```

---

## Experiment 3: Flat Line at $1.19 in Timeseries

**Symptom**: After switching to cumulative temporality, the timeseries panel showed a flat horizontal line at $1.19 during idle time. No activity was happening, but the line stayed fixed at that value.

**Why it happens**: Cumulative counters **hold their value** indefinitely. A counter that reached $1.19 stays at $1.19 even when idle — that's the nature of cumulative metrics. A timeseries panel plotting the raw counter shows this constant value as a flat line.

**Why this is confusing**: It looks like ongoing activity, but it's actually the counter's resting state. There's no way to distinguish "idle at $1.19" from "slowly accumulating to $1.19" by looking at the raw value.

**Fix**: Use `increase()` to show the **rate of change**, not the absolute value:
```
# Before (broken — shows flat line during idle)
sum(claude_code_cost_usage_USD_total)

# After (correct — shows 0 during idle, spikes during activity)
sum(increase(claude_code_cost_usage_USD_total[$__interval]))
```

For bar charts, use a fixed interval:
```
sum by (model) (increase(claude_code_cost_usage_USD_total[1h]))
```

---

## Experiment 4: rate() vs increase() — Which One?

**Symptom**: Needed to choose between `rate()` and `increase()` for timeseries panels.

**What rate() does**: Returns per-second average rate of increase.
```
rate(claude_code_cost_usage_USD_total[1h])  →  $0.0003/s
```

**What increase() does**: Returns total increase over the interval.
```
increase(claude_code_cost_usage_USD_total[1h])  →  $0.15
```

**Why increase() wins for our use case**: Cost and token values are human-meaningful as totals ("$0.15 this hour", "500 tokens this hour"), not as rates ("$0.0003 per second", "0.14 tokens per second"). `rate()` is useful for request rates (req/s), but not for cost or token dashboards.

**Decision**: `increase()` for all cost and token panels. Specifically:
- **Stat panels**: `sum(increase(counter[$__range]))` — total across the dashboard time range
- **Pie/donut charts**: `sum by (label) (increase(counter[$__range]))` — breakdown across the range
- **Bar charts**: `sum by (label) (increase(counter[1h]))` with `interval: "1h"` — per-hour totals
- **Tables**: `sum by (l1, l2) (increase(counter[$__range]))` with `instant: true`

---

## Experiment 5: Bar Charts Rendering as Smooth Curves

**Symptom**: API Request and Tool Usage panels were configured with `[1h]` aggregation but rendered as smooth curves instead of discrete hourly bars.

**Query**: `sum by (model) (count_over_time({...} | event_name = 'api_request' [1h]))` with `interval: "1h"`

**Panel type**: `timeseries` with `drawStyle: "bars"`

**Root cause**: The `timeseries` panel type evaluates the query at Grafana's **step interval** (determined by the panel width and time range), regardless of the `[1h]` in the query. Even with `drawStyle: "bars"`, the timeseries panel samples at many points per hour, producing a smooth-looking bar chart that doesn't align to hour boundaries.

**Fix**: Change panel type from `timeseries` to `barchart`:
```json
// Before (broken — smooth curves)
{
  "type": "timeseries",
  "fieldConfig": {
    "defaults": { "custom": { "drawStyle": "bars" } }
  }
}

// After (correct — discrete hourly bars)
{
  "type": "barchart",
  "interval": "1h"
}
```

The `barchart` panel type with `"interval": "1h"` evaluates the query exactly once per hour, producing clean discrete bars.

---

## Experiment 6: Hour Boundary — Current Hour Missing

**Symptom**: The most recent hour had no bar in the chart, even though activity was happening.

**Explanation**: Bar chart panels with `[1h]` interval only render a bar once the hour **completes**. The current in-progress hour will not appear until the boundary passes (e.g., activity at 14:30 won't show until 15:00).

**This is not a bug**: It's expected Grafana behavior. The `increase(...[1h])` function needs a full hour of data points to compute the increase. Mid-hour, the data is incomplete.

**Workaround**: None needed — just know that the rightmost bar will always be the *last completed* hour.

---

## Experiment 7: Delta vs Cumulative Temporality

**Symptom**: Prometheus rejected all Claude Code metrics with:
```
Error appending remote write: invalid temporality and type combination
for metric "claude_code.cost.usage"
```

**Root cause**: Claude Code's OTel SDK defaults to **Delta temporality** (each data point is an increment: "+$0.05"). Prometheus only accepts **Cumulative temporality** (running totals: "$0.05", "$0.10", "$0.15").

**First fix — deltatocumulative processor**: Added the OTel Collector's `deltatocumulative` processor to convert Delta → Cumulative in-flight. This worked initially but caused:
- **OOM crash**: With `max_stale: 30m`, the collector silently died. High-cardinality attributes created thousands of streams, each retained for 30 minutes.
- **Stale metrics**: Default `max_stale: 5m` meant counters reset to zero after 5 minutes of inactivity.
- **Sparse counter gaps**: `rate()` and `increase()` returned nothing between infrequent data points.
- **State lost on restart**: All accumulated totals vanished when the collector restarted.

**Final fix — cumulative from the SDK**:
```bash
export OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=cumulative
```

This tells Claude Code's OTel SDK to maintain running totals in-process. No `deltatocumulative` processor needed. Pipeline simplifies to: Claude Code → batch → Prometheus.

**Critical gotcha**: This env var is read at **SDK initialization only**. Changing it mid-session has no effect. Sessions started before the change continue sending Delta. This caused a false alarm when we removed the `deltatocumulative` processor — an old session (still Delta) was rejected, while new sessions worked fine.

**Evidence**: After all old sessions exited and a new session started with the env var set, $81.50 appeared correctly in the Costs dashboard at the hour boundary.

---

## Experiment 8: Metrics vs Logs Discrepancy After Process Restart

**Symptom**: Cost/token bar charts showed $0 for recent hours, but API request counts (from Loki) showed activity in those same hours.

**Root cause**: Two different data sources behave differently after a restart:
- **Prometheus counters** (`increase()`): Need 2+ data points to compute a difference. After restart, the counter starts at 0. Until the second metric export (~60s later), `increase()` returns 0.
- **Loki logs** (`count_over_time()`): Each API request is an independent log line. Counted immediately. No warm-up needed.

**Workaround**: Wait ~60s after starting a new session for cost data to appear in Prometheus.

**Long-term fix idea**: Emit cost as a field on each `api_request` log event and use Loki `sum_over_time({} | unwrap cost_usd [1h])` instead of Prometheus counters. This would eliminate the warm-up delay entirely.

---

## Experiment 9: Dashboard Changes Not Picked Up

**Symptom**: Modified dashboard JSON files but Grafana still showed the old version.

**Root cause (two issues)**:
1. **Version field**: Grafana ignores provisioned dashboard JSON updates unless the `"version"` field is incremented. Grafana compares the version in the JSON with the version in its database and skips updates if they're equal.
2. **macOS bind mount sync**: Docker Desktop for Mac doesn't always sync bind-mounted files immediately. `docker compose restart` sometimes uses a stale cached copy.

**Fix**:
1. Always bump `"version"` in the dashboard JSON when making changes
2. Use `docker compose down && docker compose up -d` instead of `restart` for reliable file sync on macOS

---

## Experiment 10: Phantom $532/Hour — Counter Resets from WAL Interleaving

**Symptom**: The "Cost per Hour by Service" bar chart showed $532 every hour from 12:00-15:00, even though no sessions were active. Total showed $2,760 (actual was ~$42).

**Query**: `sum by (service_name) (increase(claude_code_cost_usage_USD_total{...}[1h]))`

**Investigation**:
1. Raw counter values were flat at $5.38 during idle hours — `increase()` should return $0
2. `resets(counter[1h])` at 10:00 UTC returned **60** — sixty counter resets in a single hour
3. At 10-second resolution, the counter was **oscillating** between two values every minute:

```
08:51:20  $8.2589   ← stale value from OLD Prometheus WAL
08:51:40  $1.0247   ← real value from CURRENT session
08:52:20  $8.2589   ← stale WAL value again
08:52:40  $1.0247   ← real value again
... (repeating every minute)
```

**Root cause**: When we ran `docker compose down && docker compose up -d`:
1. Prometheus restarted and loaded the old WAL (Write-Ahead Log) containing the counter at $8.26
2. Simultaneously, the OTel Collector started receiving fresh OTLP data from Claude Code at $1.02 (and climbing)
3. These two data sources **interleaved** into the same time series — the WAL replay alternated with live ingestion
4. Prometheus interpreted each drop ($8.26 → $1.02) as a counter reset
5. Each "reset" added $8.26 to the `increase()` calculation
6. **60 phantom resets × $8.26 = ~$496 phantom increase per hour**
7. Once the interleaving stopped, the $8.26 stayed as a permanent offset in all subsequent `increase()` calculations, producing $532/hour indefinitely

**Why this persists during idle hours**: After the initial interleaving, Prometheus has internalized 60+ counter resets. The adjusted counter value is now $5.38 + (60 × $8.26) = ~$501. Each hour, even with no real increase, the extrapolation math produces ~$532 because the adjusted baseline keeps being compared against the raw values.

**Verification**: Queried the same data from Loki (log-based, no counters):
```
sum(sum_over_time({service_name="claude-code"} | event_name = `api_request` | unwrap cost_usd [1h]))
```
Result: **$0 for hours 12:00-15:00** (correct), **$42 total** (vs Prometheus's phantom $2,760).

**Conclusion**: `increase()` on Prometheus cumulative counters is **fundamentally broken for processes that survive container restarts**. The WAL replay creates phantom counter resets that permanently corrupt `increase()` calculations. This cannot be fixed by query tuning — it's a data-level corruption.

**Fix**: Migrate cost and token dashboards from Prometheus counters to **Loki log-based aggregation** using `sum_over_time({} | unwrap cost_usd)`. Each `api_request` log event is independent — no counters, no resets, no staleness, no warm-up delay. See `plans/loki-based-cost-tokens.md`.

---

## Summary: The Query Playbook

| Panel Type | Query Pattern | Why |
|------------|--------------|-----|
| **Stat** (total) | `sum(increase(counter[$__range]))` | Recovers totals from all sessions (including stale) within the dashboard time range |
| **Stat** (breakdown) | `sum by (model) (increase(counter[$__range]))` | Same, but grouped by label |
| **Pie/donut** | `sum by (label) (increase(counter[$__range]))` | Proportional breakdown across the range |
| **Bar chart** (hourly) | `sum by (label) (increase(counter[1h]))` + `interval: "1h"` | Per-hour totals, discrete bars |
| **Table** | `sum by (l1, l2) (increase(counter[$__range]))` + `instant: true` | Tabular breakdown |
| **Timeseries** (activity) | `sum(increase(counter[$__interval]))` | Shows rate of change, not flat lines |
| **Loki bar chart** | `sum by (label) (count_over_time({...} [1h]))` + `interval: "1h"` | Log-based counting, no warm-up delay |

### Key Rules

1. **Use Loki `sum_over_time(unwrap)` for cost and tokens** — immune to counter resets, no warm-up delay, no phantom increases (see Experiment 10)
2. **Prometheus `increase()` is unreliable after container restarts** — WAL interleaving creates phantom resets that permanently corrupt calculations
3. **Use `barchart` type** (not `timeseries` with `drawStyle: bars`) for discrete interval bars
4. **Bump `"version"` in JSON** when updating provisioned dashboards
5. **The rightmost bar is always the last completed hour** — current hour won't render until it finishes
6. **Prometheus counters are still useful** for metrics that don't need precise totals (e.g., session count, active time) or for rate-based alerting
