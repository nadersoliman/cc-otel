# CLAUDE.md

Empirical experiments with Grafana dashboard queries. See root `CLAUDE.md` for overall architecture.

## Files

- `dashboard-query-experiments.md` -- Chronological record of 10+ experiments with the Costs and Tokens dashboards. Covers: stale sessions, flat lines, `increase()` vs `rate()`, bar chart rendering, hour boundary alignment, temporality issues, and the phantom $532/hour counter reset investigation (Experiment 10). Each experiment has the symptom, hypothesis, what was tried, actual result, and fix.

## When to Reference

- Debugging unexpected dashboard values (phantom costs, missing data, flat lines).
- Understanding why Loki `sum_over_time(unwrap)` was chosen over Prometheus `increase()`.
- Adding new dashboard panels and choosing the right query approach.
