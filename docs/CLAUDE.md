# CLAUDE.md

Documentation and investigation notes for the observability stack. See root `CLAUDE.md` for overall architecture.

## Documents

| File | Description |
|------|-------------|
| `tracing-claude-code-with-otel.md` | Full investigation from Langfuse/LangSmith to Grafana LGTM. Covers what Claude Code emits, the delta vs cumulative temporality deep dive, and the OTel environment variable setup. Start here for understanding the project's origins. |
| `prometheus-operations-deep-dive.md` | Prometheus internals: WAL mechanics, counter reset detection, the WAL interleaving failure mode that caused phantom costs, and production alternatives (Mimir, Thanos, VictoriaMetrics). Reference when debugging Prometheus counter issues. |
| `monitoring-claude-code-with-mitmproxy.md` | How to intercept Claude Code HTTPS traffic with mitmproxy. Useful for inspecting raw API calls and headers outside of OTel. |

## Subdirectories

- `adr/` -- Architecture Decision Records, numbered sequentially (`0001-*.md`). See `adr/CLAUDE.md`.
- `experiments/` -- Empirical dashboard query experiments. See `experiments/CLAUDE.md`.
- `plans/` -- Implementation plans for completed features. See `plans/CLAUDE.md`.
