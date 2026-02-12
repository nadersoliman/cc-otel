# CLAUDE.md

Tempo trace storage configuration for the observability stack. See root `CLAUDE.md` for overall architecture.

**Note**: Claude Code's built-in OTel emission does not include traces (`traceId` and `spanId` are undefined in its native output). However, the **OTel trace hook** (`hooks/`) bridges Claude Code's lifecycle events into proper OTel spans and exports them to Tempo via the OTel Collector. Tempo is actively receiving and storing these traces.

## Files

- `tempo-config.yaml` -- Full Tempo configuration, mounted at `/etc/tempo/config.yaml` in the container.

## Key Config Sections

- **Distributor**: OTLP receiver on gRPC (:4317) and HTTP (:4318) inside the container. The OTel Collector exports traces here via `otlphttp/traces`.
- **Storage**: Local filesystem backend. WAL at `/var/tempo/wal`, blocks at `/var/tempo/blocks` (persisted in `tempo-data` named volume).
- **Metrics Generator**: Generates span metrics and service graphs from incoming traces, writing to Prometheus at `http://prometheus:9090/api/v1/write`.
- **Query Frontend**: gRPC on port 9096, HTTP API on port 3200.

## Gotchas

- Tempo has no exposed host port in docker-compose.yml (only internal Docker network access). Grafana reaches it at `http://tempo:3200`.
- The metrics generator's remote write to Prometheus produces data from traces received via the OTel trace hook.
