package monitoring

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestLangfuse(host string) *Langfuse {
	return &Langfuse{
		logger:     testLogger(),
		host:       host,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		authHeader: "Basic test",
		priceTTL:   time.Hour,
	}
}

func TestPriceForMatchesModelByPattern(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[
			{"modelName":"gpt-4o-mini","matchPattern":"(?i)^gpt-4o-mini","unit":"TOKENS","inputPrice":0.00000015,"outputPrice":0.0000006}
		],"meta":{"page":1,"totalPages":1}}`)
	}))
	defer srv.Close()

	lf := newTestLangfuse(srv.URL)
	price := lf.PriceFor(context.Background(), "gpt-4o-mini-2024-07-18")
	if !price.Found {
		t.Fatal("expected price found")
	}
	if price.InputPricePerToken != 0.00000015 || price.OutputPricePerToken != 0.0000006 {
		t.Fatalf("unexpected price %+v", price)
	}
}

func TestPriceForPrefersPricesMapOverLegacy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[
			{"modelName":"claude","matchPattern":"^claude","unit":"TOKENS","inputPrice":0.1,"outputPrice":0.2,"prices":{"input":0.000003,"output":0.000015}}
		],"meta":{"page":1,"totalPages":1}}`)
	}))
	defer srv.Close()

	lf := newTestLangfuse(srv.URL)
	price := lf.PriceFor(context.Background(), "claude-3-sonnet")
	if !price.Found {
		t.Fatal("expected price found")
	}
	if price.InputPricePerToken != 0.000003 || price.OutputPricePerToken != 0.000015 {
		t.Fatalf("prices map should win, got %+v", price)
	}
}

func TestPriceForSkipsNonTokenUnit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[
			{"modelName":"dalle","matchPattern":"^dalle","unit":"IMAGES","inputPrice":0.04}
		],"meta":{"page":1,"totalPages":1}}`)
	}))
	defer srv.Close()

	lf := newTestLangfuse(srv.URL)
	if price := lf.PriceFor(context.Background(), "dalle-3"); price.Found {
		t.Fatalf("non-token unit should not match, got %+v", price)
	}
}

func TestPriceForUnknownModelNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[
			{"modelName":"gpt-4o","matchPattern":"^gpt-4o$","unit":"TOKENS","inputPrice":0.01,"outputPrice":0.03}
		],"meta":{"page":1,"totalPages":1}}`)
	}))
	defer srv.Close()

	lf := newTestLangfuse(srv.URL)
	if price := lf.PriceFor(context.Background(), "some-other-model"); price.Found {
		t.Fatalf("unknown model should not match, got %+v", price)
	}
}

func TestPriceForParsesNestedPriceObjects(t *testing.T) {
	// Mirrors the real Langfuse API shape where each `prices` entry is an
	// object ({"price": ...}) and a sibling model on the same page carries
	// extra usage-type keys. A single unparsable entry must not blank the page.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[
			{"modelName":"gpt-4o-mini","matchPattern":"(?i)^(openai/)?(gpt-4o-mini)$","unit":"TOKENS","inputPrice":0.00000015,"outputPrice":0.0000006,"prices":{"input":{"price":0.00000015},"output":{"price":0.0000006},"input_cached_tokens":{"price":0.000000075}}}
		],"meta":{"page":1,"totalPages":1}}`)
	}))
	defer srv.Close()

	lf := newTestLangfuse(srv.URL)
	price := lf.PriceFor(context.Background(), "gpt-4o-mini")
	if !price.Found {
		t.Fatal("expected price found for nested price objects")
	}
	if price.InputPricePerToken != 0.00000015 || price.OutputPricePerToken != 0.0000006 {
		t.Fatalf("unexpected price %+v", price)
	}
}

func TestPriceForKeepsCacheWhenRefreshFails(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			fmt.Fprint(w, `{"data":[
				{"modelName":"gpt-4o-mini","matchPattern":"^gpt-4o-mini","unit":"TOKENS","inputPrice":0.000001,"outputPrice":0.000002}
			],"meta":{"page":1,"totalPages":1}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	lf := newTestLangfuse(srv.URL)
	// First call warms the cache.
	if price := lf.PriceFor(context.Background(), "gpt-4o-mini"); !price.Found {
		t.Fatal("expected initial price found")
	}
	// Force expiry so the next call refreshes and the server returns 500.
	lf.priceMu.Lock()
	lf.priceExpires = time.Now().Add(-time.Minute)
	lf.priceMu.Unlock()

	price := lf.PriceFor(context.Background(), "gpt-4o-mini")
	if !price.Found {
		t.Fatal("expected cached price to survive failed refresh")
	}
	if price.InputPricePerToken != 0.000001 {
		t.Fatalf("stale cache price wrong: %+v", price)
	}
}

func TestPriceForNilSafe(t *testing.T) {
	var lf *Langfuse
	if price := lf.PriceFor(context.Background(), "gpt-4o-mini"); price.Found {
		t.Fatal("nil Langfuse must return not-found")
	}
}
