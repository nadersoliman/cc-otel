package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// traceIDFromSession generates a deterministic trace ID from a session ID.
func traceIDFromSession(sessionID string) trace.TraceID {
	h := sha256.Sum256([]byte(sessionID))
	var tid trace.TraceID
	copy(tid[:], h[:16])
	return tid
}

// initTracer sets up the OTel TracerProvider with OTLP gRPC exporter.
func initTracer(endpoint, serviceName string) (func(), error) {
	ctx := context.Background()

	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial: %w", err)
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithGRPCConn(conn),
		otlptracegrpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			attribute.String("hook.version", "0.1.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}
	return shutdown, nil
}

// exportSessionTrace creates all spans for a session and exports them.
func exportSessionTrace(sessionID string, turns []Turn, toolSpanData []ToolSpanData) {
	tracer := otel.Tracer("cc-otel-hooks")
	tid := traceIDFromSession(sessionID)

	if len(turns) == 0 {
		debugLog("No turns to export")
		return
	}

	sessionStart := turns[0].StartTime
	sessionEnd := turns[len(turns)-1].EndTime

	// Create session root span.
	sessionCtx := contextWithTraceID(tid)
	sessionCtx, sessionSpan := tracer.Start(sessionCtx, fmt.Sprintf("Session %s", truncate(sessionID, 12)),
		trace.WithTimestamp(sessionStart),
		trace.WithAttributes(
			attribute.String("session.id", sessionID),
		),
	)

	// Create turn spans as children of session.
	for _, turn := range turns {
		_, turnSpan := tracer.Start(sessionCtx, fmt.Sprintf("Turn %d", turn.Number),
			trace.WithTimestamp(turn.StartTime),
			trace.WithAttributes(
				attribute.Int("turn.number", turn.Number),
				attribute.String("user.prompt", truncate(turn.UserText, 500)),
				attribute.Int("user.prompt_length", len(turn.UserText)),
			),
		)

		turnCtx := trace.ContextWithSpan(sessionCtx, turnSpan)

		// LLM Response span.
		if turn.Model != "" {
			_, llmSpan := tracer.Start(turnCtx, "LLM Response",
				trace.WithTimestamp(turn.StartTime),
				trace.WithAttributes(
					attribute.String("gen_ai.system", "anthropic"),
					attribute.String("gen_ai.request.model", turn.Model),
					attribute.Int("gen_ai.usage.input_tokens", turn.InputTokens),
					attribute.Int("gen_ai.usage.output_tokens", turn.OutputTokens),
					attribute.Int("gen_ai.usage.cache_read_tokens", turn.CacheReadTokens),
					attribute.Int("gen_ai.usage.cache_creation_tokens", turn.CacheCreationTokens),
				),
			)
			llmSpan.End(trace.WithTimestamp(turn.EndTime))
		}

		// Tool spans.
		for _, tc := range turn.ToolCalls {
			attrs := []attribute.KeyValue{
				attribute.String("tool.name", tc.Name),
				attribute.String("tool.use_id", tc.ID),
				attribute.Bool("tool.success", tc.Success),
			}
			if tc.Input != nil {
				if inputJSON, err := json.Marshal(tc.Input); err == nil {
					attrs = append(attrs, attribute.String("tool.input", truncate(string(inputJSON), 4096)))
				}
			}
			// Enrich from PostToolUse data if available.
			for _, tsd := range toolSpanData {
				if tsd.ToolUseID == tc.ID && tsd.ToolResponse != nil {
					if respJSON, err := json.Marshal(tsd.ToolResponse); err == nil {
						attrs = append(attrs, attribute.String("tool.response", truncate(string(respJSON), 4096)))
					}
					break
				}
			}

			_, toolSpan := tracer.Start(turnCtx, fmt.Sprintf("Tool: %s", tc.Name),
				trace.WithTimestamp(tc.StartTime),
				trace.WithAttributes(attrs...),
			)
			toolSpan.End(trace.WithTimestamp(tc.EndTime))
		}

		turnSpan.End(trace.WithTimestamp(turn.EndTime))
	}

	sessionSpan.End(trace.WithTimestamp(sessionEnd))
	debugLog(fmt.Sprintf("Exported %d turns for session %s", len(turns), truncate(sessionID, 12)))
}

// contextWithTraceID creates a context with a pre-set trace ID using a custom span context.
func contextWithTraceID(tid trace.TraceID) context.Context {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithRemoteSpanContext(context.Background(), sc)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
