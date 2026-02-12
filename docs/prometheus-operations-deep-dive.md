# Prometheus Operations Deep Dive

> Context: Our observability stack originally used `grafana/otel-lgtm:0.17.0` – an all-in-one container bundling OTel Collector, Prometheus, Loki, Tempo, and Grafana. We hit phantom $532/hr cost readings after container restarts. The root cause was counter reset compensation compounded by `increase()` extrapolation — not "WAL interleaving" as we initially thought. This document explains the real mechanisms and the alternatives.

## How Prometheus Stores Data

### The Ingestion Pipeline

Prometheus stores time-series data in a custom Time Series Database (TSDB). The pipeline has four stages:

| Stage | What Happens |
|-------|-------------|
| **Ingestion** | Sample arrives, is appended to the in-memory Head block's "active chunk" and simultaneously recorded in the WAL |
| **Chunk Cutting** | When the active chunk is full, it's flushed to disk and memory-mapped. Only a reference stays in memory |
| **Head Compaction** | When the Head spans ~3 hours (1.5x the default 2h chunkRange), the oldest 2h is compacted into a persistent block on disk |
| **Block Compaction** | Small persistent blocks are merged into larger ones over time (leveled compaction, LSM-tree-inspired) |
| **Retention** | Blocks beyond the configured retention period are deleted |

Each persistent block is a self-contained directory with `chunks/` (samples), `index` (maps labels to series), and `meta.json`. Blocks are portable – you can copy, delete, or archive them independently.

### The Write-Ahead Log (WAL)

The WAL is a sequential, append-only log that records every sample *before* it modifies the in-memory Head block. It's the durability mechanism – if Prometheus crashes, the WAL allows recovery of data that hadn't been compacted into blocks yet.

**File structure:**

- Written in 32 KiB pages, organized into numbered segments (e.g., `wal/000000002`, `wal/000000003`)
- Records can span page boundaries but never span segment boundaries
- Each record has a checksum for corruption detection during replay
- Minimum 3 segments retained; high-traffic instances keep ~2 hours of raw data
- Compression (Snappy) enabled by default since Prometheus 2.20, halving WAL size

**Record types:**

| Record | Purpose | Write Ordering |
|--------|---------|----------------|
| **Series** | New time series metadata (reference ID + label set) | Written *after* series creation in Head |
| **Samples** | Timestamp-value pairs, referencing a series by ID | Written *before* adding to Head |
| **Tombstones** | Explicit deletions of recent series | Rare |
| **Exemplars** | OpenMetrics exemplar data | Alongside samples |

Series records always precede samples for new series, so during replay the reference in a sample record always resolves to a known series.

**Checkpointing:**

- Runs every 2 hours after head compaction
- Checkpoints the oldest 1/3 of WAL segments into a compressed format
- Total WAL retention is ~4 hours (geometric series: constant 2h, ratio 2/3)
- Replay loads the checkpoint first, then replays remaining segments on top

### What Happens During a Restart

On startup, Prometheus must reconstruct its in-memory Head block:

1. Load any existing checkpoint into memory
2. Replay WAL segments sequentially on top of the checkpoint
3. For Series records: recreate the series in the Head with the original reference
4. For Samples records: add samples to the corresponding series (samples with no matching series are skipped)
5. Rebuild the in-memory index
6. Only after replay completes does `/-/ready` return 200 and scraping/ingestion begin

**Key facts about replay:**

- **Scraping and ingestion are paused during replay.** This creates a data gap equal to the replay duration.
- **Replay is slow** – it re-processes every sample individually to reconstruct chunks in memory
- **Memory during replay is 2–3x steady-state** – if Prometheus is already at 70% memory, replay causes OOM kills
- **After a crash, there's no memory snapshot** – forcing a full WAL replay (vs the optimized path after clean shutdown)

**Memory snapshot on shutdown** (Prometheus 2.30+, `--enable-feature=memory-snapshot-on-shutdown`): On graceful shutdown, Prometheus writes a snapshot of all in-memory series and chunks. On restart, it loads the snapshot and only replays WAL data written *after* it. This achieves 50–80% faster restarts – but only works after clean shutdowns.

### Staleness Handling

When a time series stops receiving samples:

- **Active disappearance**: If a series present in the previous scrape is absent in the current one, Prometheus writes a "stale NaN" marker
- **Target disappearance**: If a target drops from service discovery, stale markers are written after ~1.1x the scrape interval
- **Crash recovery**: If Prometheus crashed before writing stale markers, the fallback is the pre-2.0 logic: a series is stale if there's no sample within 5 minutes of evaluation time

Range vector functions (`rate()`, `increase()`) ignore stale markers and only operate on non-stale samples.

## Counter Reset Detection

This is central to understanding the phantom cost readings we saw in Experiment 10.

### How `increase()` and `rate()` Work

Both functions handle counter resets automatically:

1. Iterate over samples in the range
2. When a new value is *less* than the previous value, assume a counter reset occurred
3. Compensate by adding the last pre-reset value to all subsequent samples

**Worked example:**

Raw samples: `t0=1000, t1=1200, t2=0 (reset), t3=100`

| Step | What happens |
|------|-------------|
| Compare t1 vs t2 | `0 < 1200` – counter reset detected. Pre-reset value = 1200 |
| Adjust t2 | `0 + 1200 = 1200` |
| Adjust t3 | `100 + 1200 = 1300` |
| Adjusted series | `1000, 1200, 1200, 1300` |
| `increase()` | `last - first = 1300 - 1000 = 300` |

The 300 breaks down as: 200 (before reset: 1200–1000) + 100 (after reset: 100–0).

**Critical rule**: `rate()` must be applied *before* any aggregation. Aggregating raw counter values first bypasses the reset detection logic.

**Important nuance**: Basic counter reset detection is actually correct. A single clean reset from `$8.26` → `$1.02` produces `increase() = $1.02` — the real post-reset accumulation. The phantom values we observed came from three subtler mechanisms described below.

### Mechanism 1: `increase()` Extrapolation

`increase()` does NOT simply return `last - first`. It **extrapolates the computed slope to the boundaries of the range window**.

The algorithm:

1. Find all samples within the range window `[t_start, t_end]`
2. Take the first sample at `t_first` and last sample at `t_last`
3. Compute raw increase: `delta = adjusted_last - adjusted_first` (after counter reset compensation)
4. **Extrapolate**: stretch the slope to cover the full window `[t_start, t_end]`

The extrapolation factor is approximately `(t_end - t_start) / (t_last - t_first)`. With a 15s push interval and a `[1m]` window, if samples span 45 seconds within the 60-second window, the factor is `60/45 = 1.33x`.

**Consequences:**

- A correct raw increase of `$1.20` across a reset gets reported as `$1.20 × 1.33 ≈ $1.60`
- This is why `increase()` returns fractional values for integer counters (e.g., `2.588` when the counter increased by exactly 2)
- The extrapolation factor varies depending on where samples fall relative to window boundaries, producing wobbly graphs
- At lower query resolutions (e.g., `increase(x[5m])` displayed at 1-hour step), the errors compound further

This is an inherent limitation of PromQL, not a bug. See [Prometheus issue #3746](https://github.com/prometheus/prometheus/issues/3746) and [PromLabs: How Exactly Does PromQL Calculate Rates?](https://promlabs.com/blog/2021/01/29/how-exactly-does-promql-calculate-rates/).

### Mechanism 2: Undetected Resets

If a counter resets and climbs **past** its previous value before Prometheus sees the next sample, no reset is detected. Prometheus thinks the counter just kept going up.

```
t0: counter = $8.26    (Prometheus sees this)
    process crashes     (not observed)
    counter resets to 0 (not observed)
    counter climbs fast (not observed)
t1: counter = $9.00    (Prometheus sees this — looks like normal +$0.74 growth)
```

Real increase = `$0` (pre-crash) + `$9.00` (post-reset) = `$9.00`. Prometheus reports `$0.74`. This is **undercounting** — but across many rapid restarts the picture becomes unpredictable. The [created timestamps proposal](https://github.com/prometheus/proposals/blob/main/proposals/2023-06-13_created-timestamp.md) documents this as a decade-old limitation.

### Mechanism 3: First-Sample Ambiguity and Staleness

When a counter reappears after a gap, Prometheus faces a fundamental question: is the first observed value an accumulation from zero, or a continuation of the existing counter?

- **Staleness gap (>5 minutes)**: The old series is marked stale. The new value starts a brand-new series. Without knowing when the counter was created, the first sample's worth of increase is lost — `increase()` needs at least two samples.
- **No staleness gap (<5 minutes)**: Prometheus sees the old series continuing. If the new value is lower, counter reset detection kicks in (correctly). If the new value is higher (see Mechanism 2), the reset goes undetected.

### What Actually Happened in Experiment 10

Our phantom `$532/hr` cost readings in the `grafana/otel-lgtm` all-in-one container came from these mechanisms compounding, not from "WAL interleaving."

In the all-in-one container, the OTel Collector and Prometheus share a lifecycle — restarting one restarts both. The sequence:

1. `docker compose down` kills the container (OTel Collector + Prometheus together)
2. `docker compose up` starts both simultaneously
3. Prometheus begins WAL replay — old counter values (e.g., `$8.26`) are loaded into the Head. During replay, **all incoming data is rejected** (ingestion is paused)
4. OTel Collector also restarts, losing its in-memory state. Data pushed to Prometheus during replay is dropped
5. After WAL replay completes, Prometheus begins accepting data. The series in the Head ends at `$8.26` (from WAL). The first live sample arrives — whether it triggers a detected reset, an undetected reset, or a staleness gap depends on timing

The real damage comes from the **combination** of all three mechanisms across repeated restarts:

| Mechanism | What happens per restart | Error direction |
|-----------|------------------------|-----------------|
| Extrapolation | Every `increase()` evaluation is inflated by ~1.1–1.3x | Over-count |
| Undetected reset | Counter climbs past pre-restart value before next sample | Under-count (masks real resets) |
| Staleness gap | First sample after long restart is lost | Under-count |

With frequent restarts (crash-loops or manual `down`/`up` cycles), counter reset compensation accumulates across each detected reset, and extrapolation amplifies each one. The phantom cost grows with each cycle — and once the bad data is in the TSDB, it doesn't self-correct.

### The Fix: Created Timestamps

OTLP's `start_time_unix_nano` field tells Prometheus exactly when a counter was created or last reset to zero. With the feature flag `--enable-feature=created-timestamp-zero-ingestion`, Prometheus injects a **synthetic zero-value sample** at the creation time. This fixes Mechanisms 2 and 3:

- **Undetected resets**: The synthetic zero makes the reset visible even if the counter climbed past its previous value
- **First-sample ambiguity**: The zero gives `increase()` a known baseline from the first sample

Extrapolation (Mechanism 1) remains an inherent PromQL limitation — which is exactly why our cost and token dashboards use Loki `sum_over_time(unwrap)` on `api_request` log events instead of Prometheus counters. Loki performs exact sums with no extrapolation.

### The `out_of_order_time_window` Setting

Introduced in Prometheus 2.39. By default, TSDB enforces strict sample ordering per series — samples with older timestamps than the newest sample are rejected.

```yaml
storage:
  tsdb:
    out_of_order_time_window: 30m  # accept samples up to 30 min late
```

This is relevant for OTel setups where batching from multiple sources can produce non-monotonic timestamps. Internally, out-of-order samples are stored in a separate Write-Behind-Log (WBL) and isolated in-memory chunks, then merged during compaction.

**Note**: This setting does NOT fix the counter reset problems described above — those are about counter *values*, not timestamp ordering.

## Why All-in-One Containers Are Problematic

Grafana explicitly states `grafana/otel-lgtm` is for "development, demo, and testing environments" only. The problems:

| Issue | Impact |
|-------|--------|
| **Coupled lifecycle** | Restarting any component kills all of them. A Prometheus WAL issue takes down Loki, Grafana, and the collector |
| **No independent scaling** | Can't scale collector ingestion separately from Prometheus storage |
| **Resource contention** | All components share CPU, memory, and I/O. A Loki ingestion spike can starve Prometheus |
| **Coupled restart resets** | Restarting the container resets both the collector and Prometheus together — counter resets compound with `increase()` extrapolation to produce phantom values |
| **No high availability** | Single container = single point of failure for all observability |
| **Signal handling** | Docker expects one main process per container. Multi-process signal routing (SIGTERM) is unreliable, causing unclean shutdowns (no memory snapshot) |

## Production Architecture: Separate Containers

Even for a single developer on a local machine, separating components into their own containers eliminates the coupled-restart problem:

```
[Claude Code CLI]
       |
       | OTLP HTTP
       v
[OTel Collector container]     <-- own lifecycle, buffering queue
       |
       +-- remote_write --------> [Prometheus container]  <-- own lifecycle, own WAL
       +-- OTLP/HTTP logs ------> [Loki container]
       +-- OTLP/HTTP traces ----> [Tempo container]
       |
[Grafana container]            <-- reads from all backends
```

**Why this fixes the phantom value problem:**

The key insight: when only Prometheus restarts (not the collector), **no counter reset occurs at all**.

- `docker compose restart prometheus` does NOT affect the OTel Collector
- While Prometheus replays its WAL, the collector buffers data using `retry_on_failure` and `sending_queue`
- The cumulative counter in the collector keeps incrementing (e.g., from `$8.26` to `$9.50` during the downtime)
- When Prometheus comes back, the WAL has the series at `$8.26` and the first live sample is `$9.50` — a monotonic increase, no reset detected, no extrapolation artifacts from reset compensation

Counter resets still happen when **the whole stack restarts** — `docker compose down && up`, machine reboot, Docker daemon restart, or if Claude Code itself restarts (the OTel SDK resets the counter to zero). Separate containers don't help here because every process restarts, and Prometheus replays WAL to the old high value while the source counter starts from zero. This is where `created-timestamp-zero-ingestion` is essential — it makes the reset visible to Prometheus regardless of timing, preventing both undetected resets and first-sample ambiguity (Mechanisms 2 and 3).

**Recommended settings for the separate Prometheus container:**

| Setting | Value | Purpose |
|---------|-------|---------|
| `--enable-feature=created-timestamp-zero-ingestion` | – | Inject synthetic zeros from OTLP `start_time_unix_nano` — protects against undetected resets and first-sample loss on full stack restarts |
| `--enable-feature=memory-snapshot-on-shutdown` | – | 50–80% faster restarts after clean shutdown |
| `--storage.tsdb.out-of-order-time-window=30m` | – | Tolerate OTel batch reordering |
| `--storage.tsdb.retention.time=15d` | – | Explicit retention |
| Named Docker volume for `/prometheus` | – | WAL survives `docker compose down` |

**On the OTel Collector side**, configure the Prometheus remote write exporter with persistent queue storage so data survives collector restarts too.

## Alternatives to Standalone Prometheus

### Grafana Mimir

Mimir is a horizontally scalable, multi-tenant, long-term storage system for Prometheus metrics. Built by Grafana Labs as a fork of Cortex.

**Architecture:**

| Component | Role |
|-----------|------|
| **Distributor** | Entry point for writes. Validates, enforces per-tenant limits, shards to ingesters via consistent hashing |
| **Ingester** | Holds recent data in memory + WAL. Compacts to TSDB blocks every 2h, uploads to object storage |
| **Compactor** | Merges blocks in object storage. Split-and-merge algorithm scales to billions of series |
| **Store-Gateway** | Queries blocks from object storage. Caches index headers locally |
| **Querier** | Executes PromQL by merging data from ingesters (recent) and store-gateways (historical) |
| **Query-Frontend** | Splits, caches, and shards queries before dispatching to queriers |

**How Mimir handles WAL differently:**

- Each series is written to 3 ingesters (replication factor = 3 by default)
- If one ingester crashes and replays its WAL, the other two still have the data
- The querier merges data from multiple ingesters, filling gaps seamlessly
- A single ingester restart does NOT cause data loss or counter discontinuities
- Mimir's fork of Prometheus (`grafana/mimir-prometheus`) has fixes for WAL replay race conditions

**Does Mimir suffer from WAL interleaving?**

- At the individual ingester level: same TSDB mechanics apply
- At the system level: **no** – replication means queries always have complete data from healthy ingesters
- In the newer ingest-storage architecture (Kafka-backed), ingesters aren't even on the write path

**Deployment modes:**

| Mode | Description | Our use case? |
|------|-------------|---------------|
| **Monolithic** | All components in one process, local filesystem storage | Viable for single-developer setup |
| **Microservices** | Each component scales independently, requires object storage (S3/GCS/MinIO) | Overkill for us |
| **Ingest-storage** | Kafka handles writes, ingesters are read-only | Production-grade, not for local dev |

**OTel integration**: Use `otlphttp` exporter in OTel Collector pointing to Mimir's OTLP endpoint, or `prometheusremotewrite` exporter.

### Thanos

Thanos extends existing Prometheus instances for long-term storage and high availability. Two architectures:

**Sidecar model:**

- Thanos Sidecar runs alongside each Prometheus instance
- Uploads TSDB blocks to object storage every 2 hours
- Thanos Querier fans out to multiple Sidecars + Store nodes
- Easy rollout – just add the sidecar to existing Prometheus

**Receive model:**

- Thanos Receive implements Prometheus Remote Write API
- Prometheus remote-writes to Receive
- Receive handles block creation and object storage upload
- Better for network-isolated environments

**Does Thanos solve WAL interleaving?**

- **No** – Thanos does not change Prometheus's internal TSDB/WAL behavior
- It mitigates via HA: with dual Prometheus instances, if one crashes, the other has continuous data. Thanos Querier deduplicates
- Thanos Receive has its *own* TSDB and WAL – same replay issues. Reported OOM crash loops during WAL replay (GitHub issue #6851)
- `out_of_order_time_window` is incompatible with Thanos Sidecar (can cause compaction failures, GitHub issue #13112)

**Unique advantage**: Downsampling (5-minute and 1-hour resolutions) dramatically improves long-range query performance.

**Thanos vs Mimir:**

| Aspect | Thanos | Mimir |
|--------|--------|-------|
| Philosophy | Extends Prometheus | Replaces Prometheus |
| WAL fix | HA redundancy (work-around) | Replication (architectural fix) |
| Multi-tenancy | Limited (external labels) | Native, first-class |
| Downsampling | Yes (5m, 1h) | No |
| Deployment | Incremental (add sidecar) | All-or-none (but monolithic is simple) |
| Query caching | Metadata only | Full result caching |

### VictoriaMetrics

VictoriaMetrics takes the most radical approach: **no WAL at all**.

- Uses its own storage engine design instead of a WAL for durability
- No crash-loop from WAL replay, no scraping pause during startup, no memory spikes during restart
- Native OTLP ingestion since v1.93 (cumulative temporality only – delta requires `deltatocumulative` in the collector)
- Lower memory footprint than Prometheus (~1.3 bytes per sample)
- `vmagent` (its scraper/forwarder) uses in-memory buffering and only writes to disk when the destination is slow/down

**Does it have the WAL interleaving issue?** No. No WAL means no replay, which means no interleaving. The entire class of problems disappears.

**Trade-off**: VictoriaMetrics is not PromQL-compatible out of the box (uses MetricsQL, a superset), and the ecosystem is smaller. But for our use case (simple counters, local dev), this is a non-issue.

## What Would We Do?

### Option A: Keep Current Setup, Accept the Risk

- Stay on `grafana/otel-lgtm` all-in-one
- Use Loki for cost/token data (already done)
- Use Prometheus for the 6 remaining metrics, accepting that `docker compose down && up` may corrupt them
- **Operational discipline**: avoid full restarts; use `docker compose restart` for individual-service-like restarts (though the all-in-one container doesn't support this)

### Option B: Split Into Separate Containers

- Replace the all-in-one with a `docker-compose.yml` running 5 separate containers
- Same stack (OTel Collector + Prometheus + Loki + Tempo + Grafana) but decoupled lifecycles
- Enable `memory-snapshot-on-shutdown` and `out_of_order_time_window` on Prometheus
- Add persistent queue on OTel Collector's remote write exporter
- **Eliminates WAL interleaving** by decoupling the collector from Prometheus restarts

### Option C: Replace Prometheus With VictoriaMetrics

- Swap Prometheus for VictoriaMetrics in the Docker Compose
- No WAL, no replay, no interleaving – the entire problem class disappears
- Native OTLP support, lower memory, PromQL-compatible (MetricsQL superset)
- Keep Loki for logs, Grafana for visualization

### Option D: Use Mimir in Monolithic Mode

- Replace Prometheus with Mimir's monolithic deployment (single binary, local filesystem)
- Gets replication, better compaction, multi-tenancy
- More complex than standalone Prometheus but architecturally superior
- OTel Collector pushes via OTLP or remote write

### Summary

| Option | WAL Risk | Complexity | Effort |
|--------|----------|------------|--------|
| A – Keep as-is | High | Lowest | None |
| B – Separate containers | Eliminated | Low–Medium | Rewrite docker-compose, rewire provisioning |
| C – VictoriaMetrics | Eliminated (no WAL) | Low | Swap Prometheus image, adjust queries |
| D – Mimir monolithic | Mitigated (replication) | Medium | New component, configuration |

For a single-developer local setup, **Option B or C** gives the best bang for the buck. Option B is the most conservative (same stack, just separated). Option C is the most elegant (eliminates the problem class entirely).

## References

- [Ganesh Vernekar: Prometheus TSDB Series](https://ganeshvernekar.com/blog/prometheus-tsdb-wal-and-checkpoint/)
- [Prometheus Storage Docs](https://prometheus.io/docs/prometheus/latest/storage/)
- [Grafana Mimir Architecture](https://grafana.com/docs/mimir/latest/get-started/about-grafana-mimir-architecture/)
- [Thanos HA with Sidecar or Receiver](https://www.infracloud.io/blogs/prometheus-ha-thanos-sidecar-receiver/)
- [VictoriaMetrics OTel Integration](https://docs.victoriametrics.com/guides/getting-started-with-opentelemetry/)
- [Grafana Blog: Reflecting on otel-lgtm](https://grafana.com/blog/observability-in-under-5-seconds-reflecting-on-a-year-of-grafana-otel-lgtm/)
- [PromLabs: Out-of-Order Samples](https://promlabs.com/blog/2022/12/15/understanding-duplicate-samples-and-out-of-order-timestamp-errors-in-prometheus/)
