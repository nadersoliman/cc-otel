## Goal

Trace Claude Code agent activity using an observability backend (LangSmith, Langfuse, Jaeger, etc.).

## Key Finding: What Claude Code Emits

Claude Code supports OpenTelemetry via `CLAUDE_CODE_ENABLE_TELEMETRY=1`. It emits **metrics** and **logs/events** — but **not traces/spans** natively. The trace gap is now bridged by the **OTel trace hook** (`hooks/`), a separate Go binary that reads Claude Code's transcript and hook events, then creates and exports proper OTel spans to the collector and on to Tempo.

### Metrics

| Metric | Description |
|---|---|
| `claude_code.token.usage` | Tokens by type (input/output/cacheRead/cacheCreation) and model |
| `claude_code.cost.usage` | Cost in USD by model |
| `claude_code.session.count` | Sessions started |
| `claude_code.lines_of_code.count` | Lines added/removed |
| `claude_code.commit.count` | Git commits created |
| `claude_code.pull_request.count` | PRs created |
| `claude_code.active_time.total` | Active time in seconds |
| `claude_code.code_edit_tool.decision` | Tool permission decisions |

### Events/Logs

| Event | Key Attributes |
|---|---|
| `claude_code.user_prompt` | `prompt_length`, `prompt` (redacted by default) |
| `claude_code.tool_result` | `tool_name`, `success`, `duration_ms`, `decision` |
| `claude_code.api_request` | `model`, `cost_usd`, `input_tokens`, `output_tokens` |
| `claude_code.api_error` | `model`, `error`, `status_code` |
| `claude_code.tool_decision` | `tool_name`, `decision`, `source` |

### Supported Exporters

- `console` — stdout (for debugging)
- `otlp` — OpenTelemetry Protocol (gRPC or HTTP)
- `prometheus` — metrics only

## Investigation Steps

### Step 1: Can LangSmith ingest Claude Code telemetry?

- LangSmith accepts generic OTLP data at `https://api.smith.langchain.com/otel`
- However, it enriches traces best with GenAI semantic conventions (`gen_ai.system`, `gen_ai.prompt`, etc.)
- Claude Code uses its own attribute names, not GenAI conventions
- **Verdict**: Partially — data would arrive but lack rich LLM-specific context

### Step 2: Can Langfuse ingest Claude Code telemetry?

- Langfuse accepts OTel data at `http://localhost:3000/api/public/otel`
- Uses Basic Auth: `Authorization=Basic base64(publicKey:secretKey)`
- Protocol: `http/protobuf` only (no gRPC)
- **Problem**: Langfuse's OTel integration only accepts **traces/spans**, not logs or metrics
- Claude Code only emits **logs and metrics**, not spans
- **Verdict**: Does not work — signal type mismatch

### Step 3: Tested Langfuse locally

Script (`claude-tracer.sh`):
```bash
export CLAUDE_CODE_ENABLE_TELEMETRY=1
export OTEL_METRICS_EXPORTER=otlp
export OTEL_LOGS_EXPORTER=otlp
export OTEL_SERVICE_NAME=claude-code
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:3000/api/public/otel
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_EXPORTER_OTLP_HEADERS="Authorization=Basic $(echo -n 'pk:sk' | base64)"
```

- Langfuse endpoint responded (200 on POST to traces, OK on metrics)
- But zero traces appeared in Langfuse UI
- Root cause: Claude Code doesn't natively emit spans, Langfuse only shows spans

### Step 4: Console exporter — raw output proof

Command:
```bash
CLAUDE_CODE_ENABLE_TELEMETRY=1 \
OTEL_METRICS_EXPORTER=console \
OTEL_LOGS_EXPORTER=console \
OTEL_METRIC_EXPORT_INTERVAL=1000 \
OTEL_LOGS_EXPORT_INTERVAL=1000 \
claude -p "what is 2+2"
```

#### Log: `claude_code.user_prompt`
```json
{
  resource: {
    attributes: {
      'service.name': 'claude-code',
      'host.arch': 'arm64',
      'os.type': 'darwin',
      'os.version': '25.2.0',
      'service.version': '2.1.32'
    }
  },
  instrumentationScope: {
    name: 'com.anthropic.claude_code.events',
    version: '2.1.32'
  },
  timestamp: 1770335478974000,
  traceId: undefined,
  spanId: undefined,
  body: 'claude_code.user_prompt',
  attributes: {
    'session.id': 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee',
    'terminal.type': 'ghostty',
    'event.name': 'user_prompt',
    prompt_length: '11',
    prompt: '<REDACTED>'
  }
}
```

#### Log: `claude_code.api_request`
```json
{
  body: 'claude_code.api_request',
  attributes: {
    'event.name': 'api_request',
    model: 'claude-opus-4-6',
    input_tokens: '3',
    output_tokens: '5',
    cache_read_tokens: '18190',
    cache_creation_tokens: '2436',
    cost_usd: '0.02446',
    duration_ms: '1211'
  }
}
```

#### Metric: `claude_code.cost.usage`
```json
{
  descriptor: {
    name: 'claude_code.cost.usage',
    type: 'COUNTER',
    description: 'Cost of the Claude Code session',
    unit: 'USD'
  },
  dataPoints: [
    { attributes: { model: 'claude-opus-4-6' }, value: 0.02446 }
  ]
}
```

#### Metric: `claude_code.token.usage`
```json
{
  descriptor: {
    name: 'claude_code.token.usage',
    type: 'COUNTER',
    unit: 'tokens'
  },
  dataPoints: [
    { attributes: { type: 'input' }, value: 3 },
    { attributes: { type: 'output' }, value: 5 },
    { attributes: { type: 'cacheRead' }, value: 18190 },
    { attributes: { type: 'cacheCreation' }, value: 2436 }
  ]
}
```

#### Key observations

- `traceId: undefined`, `spanId: undefined` — **confirms Claude Code's built-in OTel emits zero spans/traces** (the OTel trace hook in `hooks/` now bridges this gap)
- Instrumentation scope: `com.anthropic.claude_code.events` — custom, not GenAI semantic conventions
- Prompts are redacted by default (set `OTEL_LOG_USER_PROMPTS=1` to include content)
- All data is OTel **logs** and **metrics** only

### Step 5: What about Jaeger?

- Jaeger is also a **tracing** backend (spans)
- Same problem — Claude Code doesn't natively emit spans (though the OTel trace hook now bridges this)
- Would need an OTel Collector to fan out to multiple backends anyway

## Signal Type Compatibility Matrix

| Backend | Accepts Traces/Spans | Accepts Metrics | Accepts Logs | Works with Claude Code? |
|---|---|---|---|---|
| LangSmith | Yes | ? | ? | Unclear |
| Langfuse | Yes | No | No | No |
| Jaeger | Yes | No | No | No |
| Prometheus | No | Yes | No | Metrics only |
| Grafana/Loki | Yes | Yes | Yes | Yes |
| Datadog | Yes | Yes | Yes | Yes |
| Honeycomb | Yes | Yes | Yes | Yes |

## Lessons Learned

1. **`source` vs `./`**: Running `./script.sh && claude` does NOT work because env vars are set in a child process that exits. Use `source script.sh && claude` to set vars in the current shell.

2. **`--resume` and env vars**: Env vars must be set before Claude Code starts. Using `source` then `--resume` works because the resumed process inherits the current shell's environment.

3. **OTel signal types matter**: OpenTelemetry has three signal types (traces, metrics, logs). Not all backends accept all types. Claude Code natively emits metrics + logs but not traces, which rules out trace-only backends like Langfuse and Jaeger for its built-in telemetry. The OTel trace hook (`hooks/`) now bridges this gap by generating spans from Claude Code's lifecycle events.

### Step 6: Grafana OTEL LGTM — working solution

Used [`grafana/otel-lgtm`](https://github.com/grafana/docker-otel-lgtm) — a single Docker image with OTel Collector + Loki + Grafana + Tempo + Prometheus, all pre-wired.

#### Problem: Delta temporality

Prometheus rejected all Claude Code metrics with:
```
Error appending remote write: invalid temporality and type combination
for metric "claude_code.cost.usage"
```

Claude Code sends **Delta temporality** counters, Prometheus only accepts **Cumulative**. Fixed by adding `deltatocumulative` processor to the OTel Collector config.

#### Problem: Prometheus metric name normalization

Prometheus normalizes OTel metric names (dots → underscores, adds `_total` suffix):

| OTel Metric Name | Prometheus Metric Name |
|---|---|
| `claude_code.cost.usage` | `claude_code_cost_usage_USD_total` |
| `claude_code.token.usage` | `claude_code_token_usage_tokens_total` |
| `claude_code.session.count` | `claude_code_session_count_total` |
| `claude_code.active_time.total` | `claude_code_active_time_seconds_total` |

#### Problem: Grafana provisioning path

The `grafana/otel-lgtm` image uses a non-standard provisioning path:
- Standard: `/etc/grafana/provisioning/dashboards/`
- This image: `/otel-lgtm/grafana/conf/provisioning/dashboards/`

Dashboard JSON files are referenced from `/otel-lgtm/` root.

#### Working setup

```
cc-otel/
├── docker-compose.yml           # grafana/otel-lgtm with persistent volume
├── claude-tracer.sh             # env vars for Claude Code
├── otelcol-config.yaml          # custom collector config (deltatocumulative)
├── dashboards/
│   ├── claude-code-dashboards.yaml  # Grafana provisioning
│   └── claude-code.json             # Dashboard definition
└── docs/tracing-claude-code-with-otel.md
```

Start:
```bash
docker compose up -d
source claude-tracer.sh && claude
```

Grafana at `http://localhost:3001` (admin/admin) → Dashboards → "Claude Code Observability"

#### Dashboard panels

- **Session Cost (USD)** — stat, from Prometheus
- **Token Usage by Type** — timeseries (input/output/cacheRead/cacheCreation)
- **Cost by Model** — pie chart (opus vs haiku)
- **Active Sessions** — stat
- **Active Time** — timeseries by session
- **Cost Over Time** — timeseries by model
- **API Requests** — Loki logs (all events)
- **Tool Usage** — Loki logs (tool_result events)

### Step 7: Errors dashboard

Created a second dashboard focused on error visibility:

- **API Errors** — log panel for `api_error` events (rate limits, 500s, auth failures)
- **API Error Rate** — bar chart by HTTP status code over time
- **API Errors by Model** — bar chart to spot model-specific failures
- **Failed Tool Executions** — log panel where `success=false`
- **Tool Failure Rate** — bar chart over time
- **Tool Rejections** — log panel where `decision=reject` (user denied a tool)
- **OTel Collector Export Failures** — pipeline health from Prometheus

#### What counts as a tool failure?

Only tools that **executed and returned an error** emit `tool_result` with `success=false` (e.g., a Bash command returning exit code 1). Pre-execution failures (e.g., `Read` on a nonexistent file) are handled at the application level and do NOT produce an OTel `tool_result` event.

## Lessons Learned

1. **`source` vs `./`**: Running `./script.sh && claude` does NOT work because env vars are set in a child process that exits. Use `source script.sh && claude` to set vars in the current shell.

2. **`--resume` and env vars**: Env vars must be set before Claude Code starts. Using `source` then `--resume` works because the resumed process inherits the current shell's environment.

3. **OTel signal types matter**: OpenTelemetry has three signal types (traces, metrics, logs). Not all backends accept all types. Claude Code natively emits metrics + logs but not traces, which rules out trace-only backends like Langfuse and Jaeger for its built-in telemetry. The OTel trace hook (`hooks/`) now bridges this gap by generating spans from Claude Code's lifecycle events.

4. **Delta vs Cumulative temporality**: Claude Code defaults to Delta temporality counters. Prometheus only accepts Cumulative. Set `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=cumulative` before starting the session. This eliminates the need for the `deltatocumulative` processor entirely.

5. **Prometheus normalizes metric names**: Dots become underscores, units and `_total` suffix are appended. Always check `{__name__=~"claude.*"}` in Prometheus to find actual names.

6. **Custom Docker images have custom paths**: Don't assume standard Grafana provisioning paths. Check container logs for the actual `Path Provisioning` setting.

7. **Tool failure telemetry is partial**: Only tools that actually execute and fail emit `tool_result` with `success=false`. Pre-execution validation failures (e.g., file not found before Read runs) don't produce OTel events.

8. **The OTel Collector can silently die**: Inside the `grafana/otel-lgtm` all-in-one container, the collector is one of several processes managed by a shell script. If it crashes (e.g., OOM from `deltatocumulative` accumulating too much state), the container stays "Up" but the OTLP ports (4317/4318) stop responding. Added a Docker healthcheck on the collector's `/ready` endpoint (port 13133) to detect this.

9. **OTel "Collector" is a pipeline, not a scraper**: Despite the name, it works in both push and pull modes. In our setup, Claude Code **pushes** OTLP data to the collector's HTTP receiver, and the collector forwards it to Loki/Prometheus/Tempo. But the same collector also **pulls** (scrapes) its own internal metrics via the `prometheus/collector` receiver. Think of it as a telemetry router, not a poller.

10. **OTel SDK temporality is set at initialization**: `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE` must be set **before** the Claude Code session starts. Changing it mid-session has no effect — the SDK locks in the temporality preference at startup. Sessions started before the env var change will continue sending Delta until they exit. This caused a false alarm: after removing `deltatocumulative` from the collector, an old session (still Delta) was rejected by Prometheus, while new sessions (Cumulative) worked fine.

11. **Grafana bar charts with fixed intervals only render completed intervals**: A bar chart panel with `[1h]` aggregation will not show the current in-progress hour. The bar only appears once the hour boundary passes. This is expected behavior, not a data gap.

## Step 8: The Delta Temporality Problem — Deep Dive

### The Problem

Claude Code emits **Delta temporality** counters (each data point is an increment like "+\$0.05"). Prometheus only accepts **Cumulative temporality** (running totals like "\$0.05", "\$0.10", "\$0.15"). We used the `deltatocumulative` processor in the OTel Collector to bridge this, but it introduced several issues:

1. **Stale metrics**: The processor evicts series from memory after `max_stale` (default 5m). Once evicted, the counter resets to zero — losing historical totals.
2. **OOM crash**: Setting `max_stale: 30m` caused the collector to crash silently (container showed "Up" but OTLP ports stopped responding).
3. **Sparse counter gaps**: The processor only emits a cumulative point when an upstream delta arrives. If a counter increments sporadically, `rate()` and `increase()` return no data because there aren't enough points within Prometheus's lookback window (5m).
4. **State lost on restart**: The processor is purely in-memory. Collector restart = all counters reset to zero.

### Why `deltatocumulative` OOM'd with `max_stale: 30m`

For our workload (3 counters × ~20 label combos = 60 streams), the memory needed is ~60KB — nowhere near OOM. Likely causes:

- **Unbounded `max_streams` default**: The default is max-int64. If any high-cardinality attribute leaks into metric labels (request ID, timestamp, trace ID), each unique combination creates a new stream retained for 30 minutes. Even moderate cardinality explosion × 30min retention = hundreds of thousands of streams.
- **Go runtime memory behavior**: Go's GC doesn't eagerly return freed memory to the OS. Without `GOMEMLIMIT`, the heap grows until OOM-killed.
- **Full stream identity retained**: Each stream stores all resource attributes, scope attributes, metric name, and data point attributes. High-cardinality resource attributes (e.g., `service.instance.id` that changes on restart) multiply the stream count.

### Research: What Others Are Doing

#### Option 1: Emit Cumulative from the SDK (Recommended)

Set `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=cumulative` instead of `delta`. This tells the OTel SDK inside Claude Code to maintain running totals in-process, eliminating the need for `deltatocumulative` entirely.

- Memory overhead in-app: ~300 bytes × 60 series = ~18KB (trivial)
- Counter values persist across the session lifetime
- Pipeline simplifies to: Claude Code → OTel Collector (batch only) → Prometheus
- No staleness, no OOM, no sparse-counter gaps
- Prometheus `sum(counter)` works exactly like traditional Prometheus

Grafana Labs [recommends this approach](https://grafana.com/blog/opentelemetry-metrics-a-guide-to-delta-vs-cumulative-temporality-trade-offs/): "Adopt cumulative temporality because it's more resilient to missing/dropped data points."

#### Option 2: Keep Delta + Harden the Processor

If delta is required (e.g., multiple exporters where some need delta):

- Set `max_streams: 1000` (hard cap, prevents cardinality explosion)
- Set `max_stale: 60m` or higher for sporadic sessions
- Add `memory_limiter` processor as the first in the pipeline
- Set `GOMEMLIMIT=<80% of container memory>`
- Accept that `rate()` may have gaps (sparse counter issue [#36485](https://github.com/open-telemetry/opentelemetry-collector-contrib/issues/36485), unresolved)

#### Option 3: Prometheus Native Delta Ingestion (Future)

Prometheus 3.x has an experimental feature flag `--enable-feature=otlp-native-delta-ingestion` that stores delta samples as-is. However:

- `rate()` and `increase()` produce **incorrect results** on delta metrics
- Must use `sum_over_time()` instead
- Not production-ready ([prometheus/proposals #48](https://github.com/prometheus/proposals/pull/48))

#### Option 4: Use a Backend That Handles Delta Natively

Datadog, New Relic, and Honeycomb natively support delta temporality — no conversion needed. VictoriaMetrics currently drops delta metrics entirely ([#8238](https://github.com/VictoriaMetrics/VictoriaMetrics/issues/8238)). Grafana Mimir has early-stage support ([#10439](https://github.com/grafana/mimir/issues/10439)).

### Processor Status: Alpha

The `deltatocumulative` processor is at **alpha stability**. Key open issues:

| Issue | Description | Status |
|---|---|---|
| [#36485](https://github.com/open-telemetry/opentelemetry-collector-contrib/issues/36485) | Sparse/slow counters — periodic re-emission needed | Closed (inactive) |
| [#35872](https://github.com/open-telemetry/opentelemetry-collector-contrib/issues/35872) | Memory reuse race condition | Fixed |
| [#31016](https://github.com/open-telemetry/opentelemetry-collector-contrib/issues/31016) | Staleness design | Implemented |

### Decision

Switched to **Option 1**: set `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=cumulative` in `~/.zshrc` and `claude-tracer.sh`. Removed `deltatocumulative` processor from the OTel Collector config.

### Verification: Does Claude Code Respect the Env Var?

Initially appeared broken — after removing `deltatocumulative`, Prometheus started rejecting metrics with `"invalid temporality and type combination"`. The collector logs showed rejections from 07:57–08:24 UTC, then they stopped.

**Root cause**: The env var only takes effect when a Claude Code session **starts**. Sessions already running when we changed the config continued sending Delta for their entire lifetime.

Evidence:
1. Old session (started before `~/.zshrc` update) → sent Delta → rejected by Prometheus (07:57–08:24 UTC)
2. Old session ended → rejections stopped
3. New session (started after `source ~/.zshrc`) → sent Cumulative → Prometheus accepted → $81.50 appeared in the Costs dashboard

**Conclusion**: `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=cumulative` **works**. Claude Code respects it. The key requirement is that the env var must be set **before** the session starts — it cannot be changed mid-session. This is standard OTel SDK behavior (temporality preference is set at SDK initialization).

### Dashboard Query Strategy

> **See also**: [experiments/dashboard-query-experiments.md](experiments/dashboard-query-experiments.md) for the full experiment log with every failure mode and fix.

With cumulative temporality, counters behave like traditional Prometheus counters. However, because Claude Code sessions are **ephemeral** (they start, run, and stop), stale series drop out of instant queries after ~5 minutes. This affects query choice:

- **Stat panels (totals across sessions)**: `sum(increase(counter{...}[$__range]))` — uses `increase()` to recover totals from stale sessions within the time window
- **Pie charts (breakdowns)**: `sum by (label) (increase(counter{...}[$__range]))` — same reasoning
- **Bar charts (per-interval)**: `sum by (label) (increase(counter{...}[1h]))` with `interval: "1h"` — shows spend/usage per hour, zero when idle
- **Tables (instant)**: `sum by (l1, l2) (increase(counter{...}[$__range]))` with `instant: true`

Note: `increase()` is preferred over plain `sum(counter)` because Claude Code sessions are short-lived. A plain `sum()` only includes currently active sessions — completed sessions disappear after Prometheus's staleness window (~5 min). `increase()` reaches back through stored data points to include all sessions within the range.

For timeseries over time, `rate()` gives per-second values (e.g., $0.0003/s — not human-readable), while `increase(...[1h])` gives per-interval totals (e.g., $0.15/hour). Use `increase()` for cost/token dashboards.

### Bar charts: hour boundary behavior

Bar chart panels with `[1h]` interval only render a bar once the hour **completes**. The current in-progress hour will not appear until it finishes. This is expected Grafana behavior, not a data gap.

## Next Steps / Ideas

- ~~Try Grafana + Loki + Prometheus stack~~ Done (Step 6)
- ~~Try `console` exporter with `claude -p "hello"` to see raw output~~ Done (Step 4)
- Try Honeycomb (native OTel, all signal types)
- ~~Explore if an OTel Collector could convert logs/metrics into spans for Langfuse~~ Solved differently -- the OTel trace hook (`hooks/`) generates spans from Claude Code's lifecycle events and transcript
- ~~Watch for future Claude Code updates that might add span emission~~ Bridged via the OTel trace hook (`hooks/`)
- Build alerting rules (e.g., cost threshold alerts)
