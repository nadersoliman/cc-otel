# CLAUDE.md

OTel trace hook for Claude Code. Bridges hook lifecycle events to OpenTelemetry traces exported to the collector (gRPC on `localhost:4320`).

## Build & Install

```bash
cd hooks
make install    # builds and copies binary to ~/.claude/hooks/otel_trace_hook
```

## Architecture

Short-lived CLI invoked by Claude Code on **PostToolUse** and **Stop** hook events via stdin JSON.

- **PostToolUse** (< 10ms, no network): Records tool data to `~/.claude/state/otel_trace_state.json`
- **Stop** (< 2s): Parses JSONL transcript, creates OTel spans, exports via OTLP gRPC, updates state

## Span Hierarchy

```
Session Root
  └── Turn N
       ├── LLM Response (model, tokens)
       ├── Tool: Read
       └── Tool: Edit
```

- **Trace ID**: Deterministic `SHA-256(session_id)[:16]` -- consistent across invocations
- **Timing**: From transcript timestamps (real wall-clock, not hook execution time)
- **Export**: `SimpleSpanProcessor` (synchronous) -- required for short-lived CLI

## Files

| File | Purpose |
|------|---------|
| `main.go` | Entry point, stdin parsing, event dispatch |
| `types.go` | Data structures (HookInput, Turn, ToolCall, SessionState) |
| `state.go` | State file load/save with file locking |
| `transcript.go` | JSONL transcript parsing into turns |
| `tracer.go` | OTel SDK init, span creation, deterministic trace IDs |

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `CC_OTEL_TRACE_ENDPOINT` | `localhost:4320` | Collector gRPC endpoint |
| `CC_OTEL_TRACE_SERVICE_NAME` | `claude-code-hooks` | `service.name` attribute |
| `CC_OTEL_TRACE_DEBUG` | `false` | Debug logging to `~/.claude/state/otel_trace_hook.log` |
