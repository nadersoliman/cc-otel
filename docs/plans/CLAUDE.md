# CLAUDE.md

Implementation plans for completed features. See root `CLAUDE.md` for overall architecture.

## Files

| File | Status | Description |
|------|--------|-------------|
| `loki-based-cost-tokens.md` | Implemented | Migration of all 13 cost/token dashboard panels from Prometheus `increase()` to Loki `sum_over_time(unwrap)`. Documents the problem (phantom costs from WAL interleaving), the solution, and the panel-by-panel migration. |
| `collector-logs-to-loki.md` | Implemented | Shipping OTel Collector internal logs to Loki via the SDK's built-in OTLP exporter (`service.telemetry.logs.processors`). Documents the approach selection and the endpoint path gotcha (`/otlp/v1/logs` vs `/otlp`). |

## When to Reference

- Understanding why a design decision was made (these plans capture the reasoning).
- Implementing similar migrations or features.
- The plans are historical -- both features are already live in the current stack.
