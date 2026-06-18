package agent

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/sdk/trace"
)

type collectingSpanExporter struct {
	spans []oteltrace.ReadOnlySpan
}

func (e *collectingSpanExporter) ExportSpans(_ context.Context, spans []oteltrace.ReadOnlySpan) error {
	e.spans = append(e.spans, spans...)
	return nil
}

func (e *collectingSpanExporter) Shutdown(context.Context) error {
	return nil
}

func TestFinishRunStateAttachesErrorRefToActiveTrace(t *testing.T) {
	store := NewInMemoryRuntimeStateStore()
	r := &Runtime{stateStore: store, now: time.Now}
	run := RunState{RunID: "run_trace_error_ref"}
	if err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	exporter := &collectingSpanExporter{}
	tp := oteltrace.NewTracerProvider(oteltrace.WithSyncer(exporter))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	ctx, span := tp.Tracer("test").Start(context.Background(), "finishRunState")
	if err := r.finishRunState(ctx, run, RuntimeRunStatusFailed); err != nil {
		t.Fatalf("finish run state: %v", err)
	}
	span.End()

	stored, err := store.GetRun(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if stored.ErrorRef == "" {
		t.Fatal("error ref was not generated")
	}
	if len(exporter.spans) != 1 {
		t.Fatalf("exported spans = %d, want 1", len(exporter.spans))
	}
	got := spanAttrValue(exporter.spans[0].Attributes(), "langfuse.trace.metadata.error_ref")
	if got != stored.ErrorRef {
		t.Fatalf("trace metadata error_ref = %q, want %q", got, stored.ErrorRef)
	}
}

func spanAttrValue(attrs []attribute.KeyValue, key string) string {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	return ""
}

var _ oteltrace.SpanExporter = (*collectingSpanExporter)(nil)
