# CLAUDE.md

Prometheus configuration for the observability stack. See root `CLAUDE.md` for overall architecture.

## Files

- `prometheus.yml` -- Minimal config: sets `scrape_interval` and `evaluation_interval` to 15s. No scrape targets are defined because metrics arrive via OTLP push, not pull.

## How Metrics Arrive

Prometheus does **not** scrape Claude Code. Instead:

1. Claude Code pushes OTLP metrics to the OTel Collector (port 4321).
2. The Collector exports to Prometheus's native OTLP endpoint at `http://prometheus:9090/api/v1/otlp`.
3. This is enabled by `--web.enable-otlp-receiver` in docker-compose.yml.

## Key Command Flags (from docker-compose.yml)

| Flag | Purpose |
|------|---------|
| `--config.file=/etc/prometheus/prometheus.yml` | Config file path |
| `--storage.tsdb.path=/prometheus` | Data stored in `prometheus-data` named volume |
| `--web.enable-remote-write-receiver` | Accepts remote write at `/api/v1/write` (used by Tempo's metrics generator) |
| `--web.enable-otlp-receiver` | Accepts OTLP push at `/api/v1/otlp` (primary ingestion path) |
| `--enable-feature=memory-snapshot-on-shutdown` | Writes memory snapshot on shutdown, reducing WAL replay time 50-80% on next start |

## Healthcheck

The docker-compose healthcheck uses `wget --spider -q http://localhost:9090/-/ready`. The OTel Collector `depends_on: condition: service_healthy` ensures it cannot push data until Prometheus has finished WAL replay. This prevents WAL interleaving (see root CLAUDE.md Known Issues).

## Gotchas

- Host port is **9092** (not 9090) because 9090 is already in use on the host.
- OTel `service.name` maps to Prometheus label `job` (not `service_name`). Use `{job=~"$service_name"}` in PromQL.
- OTel metric names are normalized: dots become underscores, unit is appended, counters get `_total` suffix. See root CLAUDE.md for the full mapping table.
