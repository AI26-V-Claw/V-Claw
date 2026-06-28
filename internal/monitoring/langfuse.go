package monitoring

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"vclaw/internal/agent"
	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
	"vclaw/internal/traceutil"
)

type LangfuseConfig struct {
	PublicKey   string
	SecretKey   string
	Host        string
	ProjectID   string
	ServiceName string
	Environment string
	Logger      *slog.Logger
}

type Langfuse struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	host      string
	projectID string

	httpClient *http.Client
	authHeader string
	priceTTL   time.Duration

	priceMu      sync.Mutex
	priceModels  []modelPrice
	priceExpires time.Time
}

// modelPrice holds a compiled Langfuse model definition used for cost lookup.
type modelPrice struct {
	pattern *regexp.Regexp
	input   float64
	output  float64
}

func NewLangfuse(ctx context.Context, cfg LangfuseConfig) (*Langfuse, error) {
	publicKey := strings.TrimSpace(cfg.PublicKey)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	host := strings.TrimRight(strings.TrimSpace(cfg.Host), "/")
	if publicKey == "" && secretKey == "" && host == "" {
		return nil, nil
	}
	if publicKey == "" || secretKey == "" {
		return nil, fmt.Errorf("LANGFUSE_PUBLIC_KEY and LANGFUSE_SECRET_KEY must both be set")
	}
	if host == "" {
		host = "https://cloud.langfuse.com"
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		serviceName = "vclaw"
	}

	headers := map[string]string{
		"Authorization":                "Basic " + basicAuth(publicKey, secretKey),
		"x-langfuse-ingestion-version": "4",
	}
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(host+"/api/public/otel/v1/traces"),
		otlptracehttp.WithHeaders(headers),
	)
	if err != nil {
		return nil, fmt.Errorf("configure langfuse exporter: %w", err)
	}

	resourceAttrs := []attribute.KeyValue{
		semconv.ServiceName(serviceName),
	}
	if env := strings.TrimSpace(cfg.Environment); env != "" {
		resourceAttrs = append(resourceAttrs, attribute.String("deployment.environment.name", env))
	}
	res, err := resource.New(ctx, resource.WithAttributes(resourceAttrs...))
	if err != nil {
		return nil, fmt.Errorf("configure langfuse resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	lf := &Langfuse{
		logger:     logger,
		tracer:     provider.Tracer("vclaw/langfuse"),
		host:       host,
		projectID:  strings.TrimSpace(cfg.ProjectID),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		authHeader: "Basic " + basicAuth(publicKey, secretKey),
		priceTTL:   priceCacheTTLFromEnv(),
	}
	// Pre-warm the price cache best-effort so the first cost calculation does not
	// race a cold fetch. Failure is non-fatal: PriceFor will retry on demand.
	if err := lf.refreshModelPrices(ctx); err != nil {
		logger.Warn("langfuse model price prewarm failed", "error", err)
	}
	return lf, nil
}

// priceCacheTTLFromEnv reads LANGFUSE_PRICE_CACHE_TTL (a Go duration like "1h").
// Defaults to 1h on empty or invalid input.
func priceCacheTTLFromEnv() time.Duration {
	const def = time.Hour
	raw := strings.TrimSpace(os.Getenv("LANGFUSE_PRICE_CACHE_TTL"))
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// langfuseModelsResponse mirrors the GET /api/public/models payload.
type langfuseModelsResponse struct {
	Data []langfuseModelDef `json:"data"`
	Meta struct {
		Page       int `json:"page"`
		TotalPages int `json:"totalPages"`
	} `json:"meta"`
}

type langfuseModelDef struct {
	ModelName    string                  `json:"modelName"`
	MatchPattern string                  `json:"matchPattern"`
	Unit         string                  `json:"unit"`
	InputPrice   *float64                `json:"inputPrice"`
	OutputPrice  *float64                `json:"outputPrice"`
	Prices       map[string]langfusePrice `json:"prices"`
}

// langfusePrice is a per-usage-type price entry in the `prices` map. The Langfuse
// API returns each entry as an object like {"price": 0.0000015}, but older/legacy
// payloads (and our own fixtures) sometimes encode it as a bare number. Accept
// both so a single unexpected shape does not fail decoding of the whole page —
// which would empty the price cache and silently record every run at zero cost.
type langfusePrice struct {
	Price float64
}

func (p *langfusePrice) UnmarshalJSON(data []byte) error {
	var bare float64
	if err := json.Unmarshal(data, &bare); err == nil {
		p.Price = bare
		return nil
	}
	var obj struct {
		Price float64 `json:"price"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	p.Price = obj.Price
	return nil
}

// PriceFor returns the per-token price for model, refreshing the cache if it has
// expired. Non-blocking beyond a possible cache refresh; the hot path is a regex
// scan over the cached definitions. Returns Found=false when no definition matches
// or no price data is available.
func (l *Langfuse) PriceFor(ctx context.Context, model string) agent.ModelPrice {
	if l == nil {
		return agent.ModelPrice{}
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return agent.ModelPrice{}
	}

	l.priceMu.Lock()
	expired := time.Now().After(l.priceExpires)
	l.priceMu.Unlock()
	if expired {
		if err := l.refreshModelPrices(ctx); err != nil {
			l.logger.Warn("langfuse model price refresh failed; using cached prices", "error", err)
		}
	}

	l.priceMu.Lock()
	models := l.priceModels
	l.priceMu.Unlock()
	for _, m := range models {
		if m.pattern != nil && m.pattern.MatchString(model) {
			return agent.ModelPrice{
				InputPricePerToken:  m.input,
				OutputPricePerToken: m.output,
				Found:               true,
			}
		}
	}
	return agent.ModelPrice{}
}

// refreshModelPrices fetches all model definitions and replaces the cache. On any
// error it leaves the existing cache untouched and returns the error, except it
// still advances the expiry slightly so a hard-down API is not hammered per call.
func (l *Langfuse) refreshModelPrices(ctx context.Context) error {
	models, err := l.fetchModels(ctx)
	if err != nil {
		l.priceMu.Lock()
		// Back off for a fraction of the TTL before retrying on persistent failure.
		l.priceExpires = time.Now().Add(l.priceTTL / 10)
		l.priceMu.Unlock()
		return err
	}
	l.priceMu.Lock()
	l.priceModels = models
	l.priceExpires = time.Now().Add(l.priceTTL)
	l.priceMu.Unlock()
	return nil
}

// fetchModels pages through GET /api/public/models and compiles token-priced model
// definitions. Definitions without a usable token price or match pattern are skipped.
func (l *Langfuse) fetchModels(ctx context.Context) ([]modelPrice, error) {
	if l.httpClient == nil {
		return nil, fmt.Errorf("langfuse http client not configured")
	}
	var out []modelPrice
	page := 1
	for {
		url := fmt.Sprintf("%s/api/public/models?page=%d&limit=100", l.host, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", l.authHeader)
		resp, err := l.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("langfuse models API status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		if readErr != nil {
			return nil, readErr
		}
		var parsed langfuseModelsResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("decode langfuse models: %w", err)
		}
		for _, def := range parsed.Data {
			mp, ok := compileModelPrice(def)
			if ok {
				out = append(out, mp)
			}
		}
		if parsed.Meta.TotalPages <= page || len(parsed.Data) == 0 {
			break
		}
		page++
	}
	return out, nil
}

// compileModelPrice converts a Langfuse model definition into a cached entry.
// Only TOKENS-unit definitions with a compilable pattern and a resolvable price
// are kept. The newer `prices` map takes precedence over legacy scalar fields.
func compileModelPrice(def langfuseModelDef) (modelPrice, bool) {
	if def.Unit != "" && !strings.EqualFold(def.Unit, "TOKENS") {
		return modelPrice{}, false
	}
	pattern := strings.TrimSpace(def.MatchPattern)
	if pattern == "" {
		return modelPrice{}, false
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return modelPrice{}, false
	}
	input, output := def.InputPrice, def.OutputPrice
	if v, ok := def.Prices["input"]; ok {
		price := v.Price
		input = &price
	}
	if v, ok := def.Prices["output"]; ok {
		price := v.Price
		output = &price
	}
	if input == nil && output == nil {
		return modelPrice{}, false
	}
	mp := modelPrice{pattern: re}
	if input != nil {
		mp.input = *input
	}
	if output != nil {
		mp.output = *output
	}
	return mp, true
}

func (l *Langfuse) StartRequest(ctx context.Context, message contracts.UserMessage) (context.Context, func(contracts.AgentResponse, error)) {
	if l == nil {
		return ctx, func(contracts.AgentResponse, error) {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	attrs := []attribute.KeyValue{
		attribute.String("langfuse.trace.name", "vclaw."+strings.TrimSpace(message.Channel)),
		attribute.String("langfuse.session.id", strings.TrimSpace(message.SessionID)),
		attribute.String("langfuse.trace.input", strings.TrimSpace(message.Text)),
		attribute.String("langfuse.trace.metadata.request_id", strings.TrimSpace(message.RequestID)),
		attribute.String("langfuse.trace.metadata.channel", strings.TrimSpace(message.Channel)),
	}
	if locale := strings.TrimSpace(message.Locale); locale != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.metadata.locale", locale))
	}
	ctx, span := l.tracer.Start(ctx, "vclaw.request", trace.WithAttributes(attrs...))
	traceID := span.SpanContext().TraceID().String()
	if strings.TrimSpace(traceID) != "" {
		ctx = traceutil.WithTraceID(ctx, traceID)
	}
	return ctx, func(response contracts.AgentResponse, err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		if response.Message != "" {
			span.SetAttributes(attribute.String("langfuse.trace.output", response.Message))
		}
		if response.Status != "" {
			span.SetAttributes(attribute.String("langfuse.trace.metadata.status", string(response.Status)))
		}
		if response.Error != nil {
			span.SetAttributes(attribute.String("langfuse.trace.metadata.error_code", response.Error.Code))
			span.SetStatus(codes.Error, response.Error.Message)
		}
		span.End()
	}
}

func (l *Langfuse) WrapProvider(provider providers.Provider) providers.Provider {
	if l == nil || provider == nil {
		return provider
	}
	if _, ok := provider.(*langfuseProvider); ok {
		return provider
	}
	return &langfuseProvider{next: provider, tracer: l.tracer}
}

func (l *Langfuse) RecordToolCall(ctx context.Context, toolCall providers.ToolCall, result tools.ToolResult, latency time.Duration) {
	if l == nil {
		return
	}
	startedAt := time.Now().Add(-latency)
	ctx, span := l.tracer.Start(ctx, toolCall.Name, trace.WithTimestamp(startedAt))
	span.SetAttributes(
		attribute.String("langfuse.observation.type", "span"),
		attribute.String("langfuse.observation.input", compactJSON(toolCall.Arguments)),
		attribute.String("langfuse.observation.output", compactJSON(map[string]any{
			"success":       result.Success,
			"contentForLLM": result.ContentForLLM,
			"error":         result.Error,
		})),
		attribute.String("langfuse.observation.metadata.tool_call_id", strings.TrimSpace(toolCall.ID)),
	)
	if !result.Success {
		span.SetAttributes(attribute.String("langfuse.observation.level", "ERROR"))
		if result.Error != nil {
			span.SetStatus(codes.Error, result.Error.Message)
		}
	}
	span.End(trace.WithTimestamp(time.Now()))
}

func (l *Langfuse) RecordApproval(ctx context.Context, event agent.ApprovalTelemetryEvent) {
	if l == nil {
		return
	}
	name := "approval." + strings.ToLower(string(event.Status))
	_, span := l.tracer.Start(ctx, name)
	span.SetAttributes(
		attribute.String("langfuse.observation.type", "event"),
		attribute.String("langfuse.observation.metadata.approval_id", strings.TrimSpace(event.ApprovalID)),
		attribute.String("langfuse.observation.metadata.request_id", strings.TrimSpace(event.RequestID)),
		attribute.String("langfuse.observation.metadata.session_id", strings.TrimSpace(event.SessionID)),
		attribute.String("langfuse.observation.metadata.tool_call_id", strings.TrimSpace(event.ToolCallID)),
		attribute.String("langfuse.observation.metadata.tool_name", strings.TrimSpace(event.ToolName)),
		attribute.String("langfuse.observation.metadata.risk_level", string(event.RiskLevel)),
		attribute.String("langfuse.observation.output", compactJSON(map[string]any{
			"status":    string(event.Status),
			"comment":   strings.TrimSpace(event.Comment),
			"expiresAt": timestampString(event.ExpiresAt),
		})),
	)
	if event.Status == agent.ActionStatusRejected || event.Status == agent.ActionStatusExpired {
		span.SetAttributes(attribute.String("langfuse.observation.level", "WARNING"))
	}
	span.End()
}

type langfuseProvider struct {
	next   providers.Provider
	tracer trace.Tracer
}

func (p *langfuseProvider) Chat(ctx context.Context, request providers.ChatRequest) (providers.ChatResponse, error) {
	model := strings.TrimSpace(request.Model)
	attrs := []attribute.KeyValue{
		attribute.String("langfuse.observation.type", "generation"),
		attribute.String("langfuse.observation.input", compactJSON(map[string]any{
			"messages":   sanitizeProviderMessagesForTelemetry(request.Messages),
			"tools":      request.Tools,
			"toolChoice": strings.TrimSpace(request.ToolChoice),
		})),
	}
	if model != "" {
		attrs = append(attrs, attribute.String("langfuse.observation.model.name", model))
	}
	ctx, span := p.tracer.Start(ctx, p.next.Name()+".chat", trace.WithAttributes(attrs...))
	resp, err := p.next.Chat(ctx, request)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return providers.ChatResponse{}, err
	}
	span.SetAttributes(attribute.String("langfuse.observation.output", compactJSON(resp.Message)))
	span.End()
	return resp, nil
}

func (p *langfuseProvider) Generate(ctx context.Context, req *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	attrs := []attribute.KeyValue{
		attribute.String("langfuse.observation.type", "generation"),
		attribute.String("langfuse.observation.input", compactJSON(map[string]any{
			"systemPrompt": req.SystemPrompt,
			"userPrompt":   req.UserPrompt,
		})),
	}
	if model := strings.TrimSpace(req.Model); model != "" {
		attrs = append(attrs, attribute.String("langfuse.observation.model.name", model))
	}
	ctx, span := p.tracer.Start(ctx, p.next.Name()+".generate", trace.WithAttributes(attrs...))
	resp, err := p.next.Generate(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return nil, err
	}
	span.SetAttributes(attribute.String("langfuse.observation.output", strings.TrimSpace(resp.Text)))
	if resp.Usage != nil {
		span.SetAttributes(attribute.String("langfuse.observation.usage_details", compactJSON(map[string]int{
			"input":  resp.Usage.PromptTokens,
			"output": resp.Usage.CompletionTokens,
			"total":  resp.Usage.TotalTokens,
		})))
	}
	span.End()
	return resp, nil
}

func (p *langfuseProvider) Name() string {
	return p.next.Name()
}

func (p *langfuseProvider) Capabilities() providers.Capabilities {
	return providers.ProviderCapabilities(p.next)
}

func (p *langfuseProvider) Close() error {
	return p.next.Close()
}

func sanitizeProviderMessagesForTelemetry(messages []providers.Message) []providers.Message {
	if len(messages) == 0 {
		return nil
	}
	sanitized := make([]providers.Message, len(messages))
	for i, message := range messages {
		sanitized[i] = message
		if len(message.Parts) > 0 {
			sanitized[i].Parts = make([]providers.ContentPart, len(message.Parts))
			for j, part := range message.Parts {
				sanitized[i].Parts[j] = part
				if part.Image != nil {
					image := *part.Image
					image.Data = nil
					sanitized[i].Parts[j].Image = &image
				}
			}
		}
	}
	return sanitized
}

func compactJSON(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func timestampString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func basicAuth(publicKey string, secretKey string) string {
	return base64.StdEncoding.EncodeToString([]byte(publicKey + ":" + secretKey))
}
