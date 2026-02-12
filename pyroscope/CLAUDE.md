# CLAUDE.md

Pyroscope continuous profiling configuration for the observability stack. See root `CLAUDE.md` for overall architecture.

**Note**: Pyroscope is kept in the stack for exploration and future use. No profiling data is currently emitted by Claude Code.

## Files

- `pyroscope-config.yaml` -- Minimal Pyroscope configuration, mounted at `/etc/pyroscope/config.yaml` in the container.

## Key Config Sections

- **Server**: gRPC on port 9097.
- **Storage**: PyroscopeDB at `/data/pyroscope` (persisted in `pyroscope-data` named volume).
- **Ring**: In-memory kvstore, `replication_factor: 1` (single-node setup).

## Access

- Host port **4040** maps to container port 4040 (Pyroscope API and UI).
- The OTel Collector exports profiles here via `otlphttp/profiles` at `http://pyroscope:4040`.
- Grafana has a pre-configured Pyroscope datasource at `http://pyroscope:4040`.
