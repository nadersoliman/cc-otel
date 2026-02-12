# CLAUDE.md

Grafana datasource provisioning for the observability stack. See root `CLAUDE.md` for overall architecture.

## Files

- `datasources.yaml` -- Provisioned into Grafana at `/etc/grafana/provisioning/datasources/datasources.yaml` via docker-compose volume mount.

## Datasources (4 total)

| Name | Type | UID | URL | Default |
|------|------|-----|-----|---------|
| Prometheus | `prometheus` | `prometheus` | `http://prometheus:9090` | No |
| Loki | `loki` | `loki` | `http://loki:3100` | **Yes** |
| Tempo | `tempo` | `tempo` | `http://tempo:3200` | No |
| Pyroscope | `grafana-pyroscope-datasource` | `pyroscope` | `http://pyroscope:4040` | No |

URLs use Docker Compose service names (not `localhost`). Loki is the default datasource since most dashboards query it.

## Cross-Datasource Links

- **Prometheus** links trace IDs to Tempo via `exemplarTraceIdDestinations`.
- **Loki** extracts `trace_id` labels and links to Tempo.
- **Tempo** links traces to Loki logs and uses Prometheus for the service map.

These links are pre-configured. While Claude Code's built-in OTel does not emit traces, the OTel trace hook (`hooks/`) exports spans to Tempo, making the Tempo cross-datasource links active.

## Gotchas

- UID values (`prometheus`, `loki`, `tempo`, `pyroscope`) are referenced by dashboard JSON panels. Changing a UID here requires updating all dashboards that reference it.
- The old `grafana/otel-lgtm` image used a non-standard provisioning path. The current setup uses the standard `/etc/grafana/provisioning/` path.
