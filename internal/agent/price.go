package agent

import "context"

// ModelPrice carries the per-token prices used to compute LLM cost for a model.
// Found is false when no price is available (model not defined upstream, source
// unconfigured, or lookup failed); callers must not fabricate a cost in that case.
type ModelPrice struct {
	InputPricePerToken  float64
	OutputPricePerToken float64
	Found               bool
}

// PriceSource resolves per-token prices for a model name. Implementations must be
// non-blocking and safe for concurrent use: PriceFor is called synchronously inside
// the agent loop after every provider response.
type PriceSource interface {
	PriceFor(ctx context.Context, model string) ModelPrice
}
