# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Local observability stack for monitoring Claude Code CLI activity. Claude Code emits **metrics** and **logs** via OpenTelemetry (its built-in OTel does not include traces). The **OTel trace hook** (`hooks/`) bridges Claude Code's lifecycle events into proper OTel spans exported to Tempo. We collect, store, and visualize all three signal types using the Grafana LGTM stack running as a 6-container Docker Compose deployment.

## Quick Start

```bash
docker compose up -d          # Start all 6 services
docker compose ps             # Verify all containers are healthy
open http://localhost:3001    # Grafana UI (admin/admin)
docker compose down           # Stop (volumes preserved)
docker compose restart grafana  # Reload provisioned dashboards
```

## Architecture

| Container | Image | Role |
|-----------|-------|------|
| otel-collector | `otel/opentelemetry-collector-contrib:0.144.0` | Receives OTLP data, exports to backends |
| prometheus | `prom/prometheus:v3.9.1` | Metrics storage |
| loki | `grafana/loki:3.6.4` | Log storage |
| tempo | `grafana/tempo:2.10.0` | Trace storage (receives spans from the OTel trace hook) |
| pyroscope | `grafana/pyroscope:1.18.0` | Continuous profiling (kept for exploration) |
| grafana | `grafana/grafana:12.3.1` | Dashboards and visualization |

Image versions are pinned. The OTel Collector requires otelcol-contrib >= v0.113.0 for internal OTLP log export.

- **Push-based**: Claude Code pushes OTLP data to the collector. No scraping.
- **Per-service volumes**: Each backend has its own named volume -- no shared `/data` mount.
- **Internal networking**: Services communicate via Docker Compose service names (e.g., `http://loki:3100`).
- **Independent restarts**: Each service can be restarted without affecting others.

## Port Mapping

| Host Port | Container Port | Container | Service |
|-----------|----------------|-----------|---------|
| 3001 | 3000 | grafana | Grafana UI |
| 4320 | 4317 | otel-collector | OTel Collector gRPC |
| 4321 | 4318 | otel-collector | OTel Collector HTTP |
| 9092 | 9090 | prometheus | Prometheus API (remapped -- 9090 already in use on host) |
| 13133 | 13133 | otel-collector | OTel Collector health check |
| 4040 | 4040 | pyroscope | Pyroscope API and UI |

## Environment Variables

Set these in your shell profile (e.g., `~/.zshrc`, `~/.bashrc`, `~/.bash_profile`) for permanent use. `claude-tracer.sh` can be sourced for quick debugging.

| Variable | Value | Purpose |
|----------|-------|---------|
| `CLAUDE_CODE_ENABLE_TELEMETRY` | `1` | Enable OTel emission from Claude Code |
| `OTEL_METRICS_EXPORTER` | `otlp` | Export metrics via OTLP |
| `OTEL_LOGS_EXPORTER` | `otlp` | Export logs via OTLP |
| `OTEL_SERVICE_NAME` | `claude-code` | Service name label on all telemetry |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4321` | OTel Collector HTTP endpoint |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | OTLP transport protocol |
| `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE` | `cumulative` | Counters as running totals (Prometheus-compatible) |
| `OTEL_LOG_USER_PROMPTS` | `1` | Include prompt content in logs (off = redacted) |
| `OTEL_LOG_TOOL_DETAILS` | `1` | Include tool details in logs |

To run separate sessions with distinct labels, override `OTEL_SERVICE_NAME`:

```bash
export OTEL_SERVICE_NAME=claude-code-project-x
claude
```

## What Claude Code Emits

> **Official reference**: [code.claude.com/docs/en/monitoring-usage](https://code.claude.com/docs/en/monitoring-usage)

**No native traces/spans** -- Claude Code's built-in OTel emission does not include traces (`traceId` and `spanId` are undefined in its native output). Traces are provided separately by the OTel trace hook (`hooks/`), which reads Claude Code's transcript and hook events, then creates and exports proper OTel spans to the collector and on to Tempo.

### Metrics (8 total -- Prometheus counters, cumulative temporality)

| OTel Name | Prometheus Name | Unit |
|-----------|-----------------|------|
| `claude_code.cost.usage` | `claude_code_cost_usage_USD_total` | USD |
| `claude_code.token.usage` | `claude_code_token_usage_tokens_total` | tokens |
| `claude_code.session.count` | `claude_code_session_count_total` | count |
| `claude_code.lines_of_code.count` | `claude_code_lines_of_code_count_total` | count |
| `claude_code.commit.count` | `claude_code_commit_count_total` | count |
| `claude_code.pull_request.count` | `claude_code_pull_request_count_total` | count |
| `claude_code.active_time.total` | `claude_code_active_time_seconds_total` | seconds |
| `claude_code.code_edit_tool.decision` | `claude_code_code_edit_tool_decision_total` | count |

Prometheus name normalization: dots become underscores, unit is appended as suffix, counters get `_total`.

### Events/Logs (5 total -- Loki)

| Event | Key Fields |
|-------|------------|
| `claude_code.user_prompt` | `prompt_length`, `prompt` (redacted unless `OTEL_LOG_USER_PROMPTS=1`) |
| `claude_code.tool_result` | `tool_name`, `success`, `duration_ms`, `decision`, `source` |
| `claude_code.api_request` | `model`, `cost_usd`, `duration_ms`, `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_creation_tokens` |
| `claude_code.api_error` | `model`, `error`, `status_code`, `duration_ms`, `attempt` |
| `claude_code.tool_decision` | `tool_name`, `decision`, `source` |

## Directory Guide

Each subdirectory has its own `CLAUDE.md` with deeper context relevant to that component.

| Directory | What its CLAUDE.md covers |
|-----------|--------------------------|
| `dashboards/` | Dashboard JSON files, provisioning config, query source conventions, template variables, how to add/modify dashboards |
| `grafana/` | Datasource provisioning (`datasources.yaml`), datasource UIDs, cross-datasource links |
| `prometheus/` | Config file, OTLP push ingestion path, command flags, healthcheck/WAL replay gating |
| `loki/` | Config file, how logs arrive (two sources), storage/schema details |
| `tempo/` | Config file, trace storage setup (receives spans from the OTel trace hook) |
| `pyroscope/` | Config file, profiling storage (inactive -- kept for exploration) |
| `docs/` | Investigation notes, deep dives, and links to `experiments/` and `plans/` subdirectories |

## Key Conventions

### Prometheus `job` label vs Loki `service_name`

The OTel resource attribute `service.name` maps to different labels in each backend:

- **Loki panels**: `{service_name=~"$service_name"}`
- **Prometheus panels**: `{job=~"$service_name"}`

Both use the same `$service_name` template variable. When creating new panels, use the correct label for the datasource.

### Cumulative temporality requirement

Claude Code defaults to **delta** temporality. We override to **cumulative** via `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=cumulative`. Without this, Prometheus rejects the metrics.

### Loki-based dashboards for cost/token data

Cost and token dashboards use Loki `sum_over_time(unwrap)` on `api_request` log events, not Prometheus counters. Loki provides instant results (no warm-up delay), survives container restarts, and gives exact sums without `increase()` extrapolation artifacts. See `docs/` for the full migration rationale.

## Critical Gotchas

- **OTel env vars must be in the shell profile, not `settings.json`** -- The OTel SDK initializes before `settings.json` is parsed. All `OTEL_*` and `CLAUDE_CODE_ENABLE_TELEMETRY` vars must be in the shell environment (e.g., `~/.zshrc`, `~/.bashrc`).
- **Collector SDK endpoint path** -- The `service.telemetry.logs.processors` OTLP exporter does NOT auto-append `/v1/logs`. You must specify `http://loki:3100/otlp/v1/logs`. Using just `/otlp` results in a silent 404.
- **Tool failure telemetry is partial** -- Only tools that execute and fail emit `tool_result` with `success=false`. Pre-execution validation failures don't produce OTel events.

## Debugging

```bash
# Check all container statuses
docker compose ps

# Tail collector logs
docker compose logs -f otel-collector

# Test OTLP endpoint
curl -s http://localhost:13133/ready

# List Claude Code metrics in Prometheus
curl -s 'http://localhost:9092/api/v1/label/__name__/values' | jq '.data[] | select(startswith("claude"))'

# Check Loki streams
curl -s 'http://localhost:3100/loki/api/v1/query?query={service_name=~".%2B"}' | jq '.data.result[].stream'

# Raw console export (no container needed)
CLAUDE_CODE_ENABLE_TELEMETRY=1 OTEL_METRICS_EXPORTER=console OTEL_LOGS_EXPORTER=console claude -p "hello"
```

See `prometheus/CLAUDE.md` and `loki/CLAUDE.md` for curl query examples against each backend.

## Git Conventions

All commits must use the author flag: `--author="Claude Opus 4.6 <noreply@anthropic.com>"`

## Deep Dives & Experiments

Detailed investigations and experiment logs live in `docs/`. See `docs/CLAUDE.md` for the full index.
