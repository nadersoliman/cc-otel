# CLAUDE.md

Go-based pre-commit hook implementing Tier 1 quality gates. See `docs/adr/0001-ci-quality-gates.md` for the full ADR.

## Build & Install

```bash
cd scripts/pre-commit
make install    # builds binary and copies to .git/hooks/pre-commit
make check      # builds and runs checks without installing
make clean      # removes build artifact
```

The binary must be reinstalled after a fresh clone (`.git/hooks/` is not tracked by git).

## What It Checks

| Check | What it catches |
|-------|----------------|
| JSON validation | Invalid JSON in `dashboards/*.json` (skips directories) |
| YAML validation | Invalid YAML in all config files (`docker-compose.yml`, `otelcol-config.yaml`, `grafana/datasources.yaml`, `prometheus/prometheus.yml`, `loki/loki-config.yaml`, `tempo/tempo-config.yaml`, `pyroscope/pyroscope-config.yaml`, `dashboards/claude-code-dashboards.yaml`) |
| Docker Compose validation | `docker compose config --quiet` syntax check |
| Version pinning — Docker | Rejects floating tags (`latest`, `next`, `stable`, `edge`, `nightly`, `mainline`, `lts`, `previous`) and untagged images |
| Version pinning — Go | Rejects pseudo-versions (`v0.0.0-*`) in direct deps of `hooks/go.mod` |

## Dependencies

- Go (builds the binary)
- `gopkg.in/yaml.v3` (YAML parsing)
- `jq` is NOT required — JSON validation uses Go's `encoding/json`
- `docker` CLI must be available for Docker Compose validation

## Files

| File | Purpose |
|------|---------|
| `main.go` | All check logic in a single file |
| `Makefile` | Build, install, check, and clean targets |
| `go.mod` / `go.sum` | Go module with `gopkg.in/yaml.v3` dependency |
