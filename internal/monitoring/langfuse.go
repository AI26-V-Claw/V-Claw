package monitoring

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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
	return &Langfuse{
		logger:    logger,
		tracer:    provider.Tracer("vclaw/langfuse"),
		host:      host,
		projectID: strings.TrimSpace(cfg.ProjectID),
	}, nil
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
			"success":        result.Success,
			"contentForLLM":  result.ContentForLLM,
			"contentForUser": result.ContentForUser,
			"error":          result.Error,
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
			"messages":   request.Messages,
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

func (p *langfuseProvider) Close() error {
	return p.next.Close()
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
