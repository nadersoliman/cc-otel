# CLAUDE.md

Grafana dashboard JSON files and provisioning config for the Claude Code observability stack. See root `CLAUDE.md` for overall architecture.

## Files

- `claude-code-dashboards.yaml` -- Grafana provisioning config. Lists each dashboard JSON file with its container path under `/etc/grafana/provisioning/dashboards/`. Grafana auto-reloads every 10s (`updateIntervalSeconds`).
- `claude-code-costs.json` -- Cost tracking (Loki `sum_over_time(unwrap cost_usd)`)
- `claude-code-tokens.json` -- Token usage (Loki `sum_over_time(unwrap)` on `input_tokens`, `output_tokens`, etc.)
- `claude-code-api-requests.json` -- API request rate and log panel (Loki `count_over_time`)
- `claude-code-api-errors.json` -- API errors by status code and model (Loki)
- `claude-code-tool-usage.json` -- Tool success/failure/rejection rates (Loki)
- `claude-code-log-explorer.json` -- Raw log browser (Loki)
- `claude-code-activity.json` -- Activity metrics (Prometheus)
- `otel-collector.json` -- Collector health: memory, queue, export stats (Prometheus + Loki)
- `dashboard.yml` -- Legacy provisioning file (unused by current docker-compose volume mounts)

## Query Source Convention

- **Loki** for all Claude Code dashboards (Costs, Tokens, API Requests, Tool Usage, API Errors, Log Explorer). Use `{service_name=~"$service_name"}`.
- **Prometheus** for Activity and OTel Collector dashboards. Use `{job=~"$service_name"}`.

The label name differs (`service_name` vs `job`) but both use the same `$service_name` template variable.

## Template Variables

- `$service_name` -- Multi-select, default `.+` (All). Filters by Claude Code service instance.
- `$tool_status` -- Tool Usage dashboard only. Values: All / Success / Failure.

## Adding or Modifying Dashboards

1. Edit the JSON file (or export from Grafana UI and save here).
2. Add a new provider entry in `claude-code-dashboards.yaml` with the container path.
3. Add a volume mount in `docker-compose.yml` under the `grafana` service.
4. Run `docker compose restart grafana` to reload.

## Gotchas

- Files are named `claude-code-*` because "Claude Code" is the product being monitored. The `cc-otel-*` directories are empty placeholders (not used).
- Dashboard JSON `uid` fields must be unique across all dashboards.
- Provisioned dashboards cannot be saved back from the Grafana UI unless `editable: true` is set in the provider -- but changes will be lost on restart since the file is bind-mounted.
