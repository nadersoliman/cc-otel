# CLAUDE.md

Loki log storage configuration for the observability stack. See root `CLAUDE.md` for overall architecture.

## Files

- `loki-config.yaml` -- Full Loki configuration, mounted at `/etc/loki/local-config.yaml` in the container.

## How Logs Arrive

Two sources push logs to Loki's OTLP endpoint (`http://loki:3100/otlp`):

1. **Claude Code events** -- via the OTel Collector pipeline (otlphttp/logs exporter).
2. **Collector internal logs** -- via the Collector's SDK telemetry exporter (bypasses the pipeline).

## Key Config Sections

- **Storage**: Filesystem-backed with TSDB store. Chunks at `/loki/chunks`, rules at `/loki/rules` (persisted in `loki-data` named volume).
- **Schema**: v13, TSDB index with 24h period, in effect since 2020-10-24.
- **Ring**: In-memory kvstore, `replication_factor: 1` (single-node setup).
- **Auth**: Disabled (`auth_enabled: false`).

## Gotchas

- The Collector's SDK exporter must specify the full path `http://loki:3100/otlp/v1/logs` (it does not auto-append `/v1/logs`). The pipeline exporter uses `http://loki:3100/otlp` (it does auto-append). See root CLAUDE.md for details.
- OTel `service.name` maps to Loki stream label `service_name`. Use `{service_name=~"$service_name"}` in LogQL.
- Loki is the default Grafana datasource since most dashboards are Loki-based.
