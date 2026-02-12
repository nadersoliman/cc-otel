# Plan: Ship OTel Collector Internal Logs to Loki

**Status:** Implemented
**Date:** 2026-02-07

## Problem

The OTel Collector dashboard shows **how many** exports failed or receivers refused (via Prometheus counters), but not **why**. The actual error messages (e.g., "invalid temporality", "Exporting failed. Dropping data.") live in container stdout (`docker logs claude-code-otel`) and are not queryable in Grafana.

## Goal

Make collector internal logs queryable in Grafana log panels so failures shown in bar charts have companion detail panels for troubleshooting.

---

## Version Research & Findings

We investigated whether our stack supports the Internal OTLP Log Export feature (`service.telemetry.logs.processors`).

### Our versions

| Component | Version |
|-----------|---------|
| Docker image | `grafana/otel-lgtm:latest` → **0.17.0** |
| otelcol-contrib | **0.144.0** |

### Feature timeline

| Milestone | Version | Date | Reference |
|-----------|---------|------|-----------|
| Feature gate introduced (alpha) | v0.78.0 | 2023-05-22 | [PR #7678](https://github.com/open-telemetry/opentelemetry-collector/pull/7678) |
| Feature gate → beta (enabled by default) | v0.109.0 | 2024-09-10 | [PR #11091](https://github.com/open-telemetry/opentelemetry-collector/pull/11091) |
| Feature gate → stable | v0.110.0 | 2024-09-23 | [PR #11202](https://github.com/open-telemetry/opentelemetry-collector/pull/11202) |
| **Log export via OTLP** (otelzap bridge) | **v0.113.0** | 2024-11-05 | [PR #10544](https://github.com/open-telemetry/opentelemetry-collector/pull/10544) |
| Feature gate removed (permanent default) | v0.128.0 | 2025-06-09 | [PR #13153](https://github.com/open-telemetry/opentelemetry-collector/pull/13153) |
| **Our version** | **v0.144.0** | — | — |

### Conclusion

**v0.144.0 >> v0.113.0** — we are well past the required version. No feature gate needed. The internal OTLP log export is the permanent default behavior. Selecting **Approach A**.

### Known caveats at our version

- **Log level filtering doesn't apply to OTLP output** — all severities get exported regardless of `level:` setting ([Issue #11813](https://github.com/open-telemetry/opentelemetry-collector/issues/11813)). Acceptable — we want all levels for troubleshooting.
- **Logs are tee'd** — always written to both stderr AND the OTLP destination; can't disable console output ([Issue #13019](https://github.com/open-telemetry/opentelemetry-collector/issues/13019)). Acceptable — `docker logs` still works as before.

---

## Selected Approach: Internal OTLP Log Export (Approach A)

The collector exports its own internal logs directly to Loki via the OTel SDK's built-in OTLP exporter. This bypasses the collector's own pipeline entirely — no filelog receiver, no log file, no JSON parsing, no rotation concern.

### Config change (otelcol-config.yaml)

Add `telemetry` block under `service`:

```yaml
service:
  telemetry:
    logs:
      level: info
      processors:
        - batch:
            exporter:
              otlp:
                protocol: http/protobuf
                endpoint: http://127.0.0.1:3100/otlp/v1/logs
    resource:
      service.name: otel-collector
  extensions: [health_check]
  pipelines:
    # ... unchanged ...
```

### Gotcha: endpoint must include full path

The internal telemetry OTLP exporter does NOT auto-append `/v1/logs` to the endpoint (unlike the collector's pipeline `otlphttp` exporter). You must specify the full path:

- **Wrong**: `http://127.0.0.1:3100/otlp` → 404 Not Found
- **Correct**: `http://127.0.0.1:3100/otlp/v1/logs`

This caused empty panels on first deploy — the collector silently failed to send its own logs to Loki.

### Why this works

- The collector's SDK emits log records directly to Loki's OTLP endpoint
- This does NOT go through the collector's own logs pipeline (no feedback loop)
- `service.name: otel-collector` resource attribute makes these logs queryable separately from Claude Code logs in Loki
- No new receivers, processors, or pipelines needed

### What changes

| Aspect | Before | After |
|--------|--------|-------|
| Collector internal logs | stderr only (`docker logs`) | stderr + Loki (queryable in Grafana) |
| Log file on disk | None | None |
| Collector config | No `telemetry` block | ~8 lines added |
| Feedback loop risk | N/A | None (bypasses pipeline) |
| Log rotation concern | N/A | None (no file) |

---

## Approach B: filelog Receiver Self-Pipeline (Fallback)

The collector writes JSON logs to a file, then reads that file with the `filelog` receiver and ships to Loki through a dedicated pipeline.

### Config changes (otelcol-config.yaml)

```yaml
receivers:
  otlp:
    # ... existing ...
  prometheus/collector:
    # ... existing ...
  filelog/collector:
    include:
      - /tmp/otel-collector.log
    start_at: end
    include_file_name: false
    include_file_path: false
    operators:
      - type: json_parser
      - type: time_parser
        parse_from: attributes.ts
        layout_type: epoch
        layout: s.us
      - type: severity_parser
        parse_from: attributes.level
      - type: move
        from: attributes.msg
        to: body

processors:
  batch:
  resource/collector:
    attributes:
      - key: service.name
        value: otel-collector
        action: upsert

service:
  telemetry:
    logs:
      level: info
      encoding: json
      output_paths: ["stderr", "/tmp/otel-collector.log"]
  extensions: [health_check]
  pipelines:
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp/logs]
    logs/collector:
      receivers: [filelog/collector]
      processors: [resource/collector, batch]
      exporters: [otlphttp/logs]
    # ... other pipelines unchanged ...
```

### Key design decisions

| Decision | Rationale |
|----------|-----------|
| `layout_type: epoch`, `layout: s.us` | OTel Collector uses zap logger which emits `"ts":1721163003.793889` (epoch float), NOT ISO 8601 |
| `resource/collector` processor (not `resource:` on receiver) | The `resource:` key is not valid on the filelog receiver — must use a processor |
| Separate `logs/collector` pipeline | Isolates resource attribute assignment from the OTLP logs pipeline |
| `move` operator: `attributes.msg` → `body` | Without this, Grafana log panels show raw JSON instead of the readable message |
| `/tmp/` not `/data/` | Wiped on container restart — eliminates log rotation concern. No persistent growth. |
| `start_at: end` | Don't replay old logs on restart. Only live logs matter for troubleshooting. |
| `include_file_name: false` | Prevents unnecessary Loki label cardinality |
| `stderr` for console output | Matches zap's default; keeps `stdout` clean |

### Feedback loop analysis

Safe at `info` level:
- filelog reads a line → emits a log record → batch processor → Loki HTTP export
- The export does NOT generate a new collector log line (unless it fails)
- Loop terminates after 1 hop

**Constraint**: Never set collector log level to `debug` while this pipeline is active — debug-level events from the filelog receiver would create an unbounded feedback loop.

### Failure amplification edge case

If Loki is down → export fails → collector logs the error → filelog reads it → tries to send to Loki → fails again. Bounded because the collector logs the initial failure then backs off (does not log every retry).

---

## Dashboard Changes (both approaches)

### New panels for otel-collector.json

**Panel: "Export Failure Logs"** — placed below "Sent vs Failed per Hour" bar charts

```
{service_name="otel-collector"} | json | level = `error` | line_format "{{.msg}}{{if .error}} error={{.error}}{{end}}"
```

**Panel: "Receiver Logs"** — placed below "Receiver: Accepted vs Refused per Hour"

```
{service_name="otel-collector"} | json | level = `error` or level = `warn` | line_format "{{.msg}}{{if .error}} error={{.error}}{{end}}"
```

Note: Use conditional `{{if .error}}` to avoid `<no value>` on lines without an error field.

Optional: Add a `$log_level` template variable (info/warn/error) for filtering.

---

## Files Changed (Approach A)

| File | Change |
|------|--------|
| `otelcol-config.yaml` | Add `service.telemetry.logs.processors` and `service.telemetry.resource` (~8 lines) |
| `dashboards/otel-collector.json` | Add 2 log panels (export failures, receiver errors), bump version |
| `docker-compose.yml` | No change needed |

---

## Rejected Alternatives

### Docker Loki logging driver
- Sends ALL container stdout (Grafana + Prometheus + Loki + Collector mixed) to Loki
- Requires Docker plugin installation
- Chicken-and-egg: Loki is inside the container, driver needs Loki's API during startup
- Overkill for dev stack

### Sidecar / second collector
- Production-grade approach but extreme overkill for local dev

---

## Preconditions

- `filelog` receiver is included in `otelcol-contrib` (confirmed — `grafana/otel-lgtm` bundles `otelcol-contrib` v0.144.0)
- `/tmp/` exists in the container (standard Linux, always true)

## Open Questions

- [x] Does `otelcol-contrib` v0.144.0 support `service.telemetry.logs.processors`? **Yes** — feature landed in v0.113.0, gate removed in v0.128.0. We're on v0.144.0.
- [x] What is the exact Loki OTLP endpoint path? **`http://127.0.0.1:3100/otlp`** — same as the existing `otlphttp/logs` exporter uses.
- [x] Should we add a `filter` processor? **No** — Approach A bypasses the pipeline entirely, no feedback loop possible.
- [ ] Verify Loki creates a `service_name="otel-collector"` stream from the resource attribute after deployment.
