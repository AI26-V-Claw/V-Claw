package agent

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/sdk/trace"

	"vclaw/internal/providers"
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
	if _, err := r.finishRunState(ctx, run, RuntimeRunStatusFailed, "test_failure"); err != nil {
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

type stubPriceSource struct {
	price ModelPrice
	calls int
}

func (s *stubPriceSource) PriceFor(context.Context, string) ModelPrice {
	s.calls++
	return s.price
}

func TestRecordLLMUsageCostPersistsWithCanceledContext(t *testing.T) {
	store := NewInMemoryRuntimeStateStore()
	run := RunState{RunID: "run_cost", CostUSD: 1.25}
	if err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	price := &stubPriceSource{price: ModelPrice{InputPricePerToken: 0.000003, OutputPricePerToken: 0.000015, Found: true}}
	r := &Runtime{stateStore: store, now: time.Now, priceSource: price}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r.recordLLMUsageCost(ctx, &run, &providers.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30})

	stored, err := store.GetRun(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	want := 1.25 + float64(10)*0.000003 + float64(20)*0.000015
	if stored.CostUSD != want {
		t.Fatalf("cost_usd = %v, want %v", stored.CostUSD, want)
	}
}

func TestRecordLLMUsageCostSkipsWhenPriceNotFound(t *testing.T) {
	store := NewInMemoryRuntimeStateStore()
	run := RunState{RunID: "run_cost_missing", CostUSD: 2.0}
	if err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	price := &stubPriceSource{price: ModelPrice{Found: false}}
	r := &Runtime{stateStore: store, now: time.Now, priceSource: price}

	r.recordLLMUsageCost(context.Background(), &run, &providers.Usage{PromptTokens: 100, CompletionTokens: 200})

	stored, err := store.GetRun(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if stored.CostUSD != 2.0 {
		t.Fatalf("cost_usd = %v, want unchanged 2.0", stored.CostUSD)
	}
	if price.calls != 1 {
		t.Fatalf("PriceFor calls = %d, want 1", price.calls)
	}
}

func TestRecordLLMUsageCostSkipsWhenNoPriceSource(t *testing.T) {
	store := NewInMemoryRuntimeStateStore()
	run := RunState{RunID: "run_cost_nil_source", CostUSD: 3.0}
	if err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	r := &Runtime{stateStore: store, now: time.Now}

	r.recordLLMUsageCost(context.Background(), &run, &providers.Usage{PromptTokens: 100, CompletionTokens: 200})

	stored, err := store.GetRun(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if stored.CostUSD != 3.0 {
		t.Fatalf("cost_usd = %v, want unchanged 3.0", stored.CostUSD)
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
