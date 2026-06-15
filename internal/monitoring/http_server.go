package monitoring

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type ServerConfig struct {
	Logger                *slog.Logger
	Metrics               *Metrics
	DatabaseURL           string
	ProviderName          string
	GoogleOAuthConfigured bool
	TavilyConfigured      bool
	ChannelName           string
	ToolCount             int
	StartedAt             time.Time
}

type Server struct {
	logger                *slog.Logger
	metrics               *Metrics
	db                    *sql.DB
	providerName          string
	googleOAuthConfigured bool
	tavilyConfigured      bool
	channelName           string
	toolCount             int
	startedAt             time.Time
}

type HealthResponse struct {
	Status     string                     `json:"status"`
	Uptime     string                     `json:"uptime"`
	CheckedAt  string                     `json:"checkedAt"`
	Components map[string]ComponentStatus `json:"components"`
}

type ComponentStatus struct {
	Status    string `json:"status"`
	LatencyMS int64  `json:"latencyMs,omitempty"`
	ToolCount int    `json:"toolCount,omitempty"`
}

type healthResponse = HealthResponse
type componentStatus = ComponentStatus

type historyResponse struct {
	Events []map[string]any `json:"events"`
}

func StartServer(ctx context.Context, cfg ServerConfig) error {
	server, err := NewServer(cfg)
	if err != nil {
		return err
	}
	return server.Start(ctx)
}

func NewServer(cfg ServerConfig) (*Server, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	startedAt := cfg.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}

	var db *sql.DB
	databaseURL := strings.TrimSpace(cfg.DatabaseURL)
	if databaseURL != "" {
		var err error
		db, err = sql.Open("pgx", databaseURL)
		if err != nil {
			return nil, fmt.Errorf("open postgres monitoring connection: %w", err)
		}
	}

	return &Server{
		logger:                logger,
		metrics:               cfg.Metrics,
		db:                    db,
		providerName:          strings.TrimSpace(cfg.ProviderName),
		googleOAuthConfigured: cfg.GoogleOAuthConfigured,
		tavilyConfigured:      cfg.TavilyConfigured,
		channelName:           strings.TrimSpace(cfg.ChannelName),
		toolCount:             cfg.ToolCount,
		startedAt:             startedAt,
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	port := strings.TrimSpace(os.Getenv("METRICS_PORT"))
	if port == "" {
		port = "8080"
	}
	mux := http.NewServeMux()
	// No auth is needed here because this server is only intended for single-owner local deployment.
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/metrics/history", s.handleMetricsHistory)

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		if s.db != nil {
			_ = s.db.Close()
		}
	}()

	go func() {
		s.logger.Info("starting metrics server", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("metrics server stopped", "error", err)
		}
	}()
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health, err := s.probeHealth(r.Context(), time.Now())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, health)
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.metrics.Snapshot(time.Now()))
}

func (s *Server) handleMetricsHistory(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid since"})
		return
	}
	events, err := s.loadHistory(r.Context(), limit, since)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, historyResponse{Events: events})
}

func (s *Server) loadHistory(ctx context.Context, limit int, since time.Time) ([]map[string]any, error) {
	if s.db == nil {
		return nil, fmt.Errorf("DATABASE_URL is not configured")
	}
	tables, err := s.availableAuditTables(ctx)
	if err != nil {
		return nil, err
	}

	parts := make([]string, 0, 5)
	if tables["agent_runs"] {
		parts = append(parts, `SELECT started_at AS ts, 'run' AS event_type,
request_id, session_id, status, NULL::text AS tool_name,
NULL::text AS approval_id, NULL::text AS decision, completed_at, NULL::text AS error_text,
jsonb_build_object('goal', original_goal, 'iterationCount', iteration_count) AS payload
FROM agent_runs
WHERE ($1::timestamptz IS NULL OR started_at >= $1)`)
	}
	if tables["tool_executions"] {
		parts = append(parts, `SELECT requested_at AS ts, 'tool_call' AS event_type,
request_id, session_id, execution_status AS status, tool_name,
tool_call_id AS approval_id, NULL::text AS decision, completed_at, error::text AS error_text,
jsonb_build_object('toolCallId', tool_call_id, 'success', result_success, 'result', result_data, 'startedAt', started_at) AS payload
FROM tool_executions
WHERE ($1::timestamptz IS NULL OR requested_at >= $1)`)
	}
	if tables["approval_requests"] {
		parts = append(parts, `SELECT created_at AS ts, 'approval_request' AS event_type,
request_id, session_id, status, NULL::text AS tool_name,
approval_id, NULL::text AS decision, resolved_at AS completed_at, NULL::text AS error_text,
jsonb_build_object('toolCallId', tool_call_id, 'riskLevel', risk_level, 'summary', summary) AS payload
FROM approval_requests
WHERE ($1::timestamptz IS NULL OR created_at >= $1)`)
	}
	if tables["approval_decisions"] {
		parts = append(parts, `SELECT decided_at AS ts, 'approval_decision' AS event_type,
request_id, NULL::text AS session_id, decision AS status, NULL::text AS tool_name,
approval_id, decision, decided_at AS completed_at, NULL::text AS error_text,
jsonb_build_object('decidedBy', decided_by, 'comment', comment) AS payload
FROM approval_decisions
WHERE ($1::timestamptz IS NULL OR decided_at >= $1)`)
	}
	if tables["audit_entries"] {
		parts = append(parts, `SELECT timestamp AS ts, 'error' AS event_type,
request_id, session_id, policy_decision AS status, tool_name,
approval_id, NULL::text AS decision, NULL::timestamptz AS completed_at, error_message AS error_text,
jsonb_build_object('channel', channel, 'eventType', event_type, 'outputSummary', output_summary, 'riskLevel', risk_level) AS payload
FROM audit_entries
WHERE error_message IS NOT NULL AND error_message <> '' AND ($1::timestamptz IS NULL OR timestamp >= $1)`)
	}
	if len(parts) == 0 {
		return []map[string]any{}, nil
	}

	query := strings.Join(parts, "\nUNION ALL\n") + "\nORDER BY ts DESC LIMIT $2"
	rows, err := s.db.QueryContext(ctx, query, nullableTime(since), limit)
	if err != nil {
		return nil, fmt.Errorf("query metrics history: %w", err)
	}
	defer rows.Close()

	events := make([]map[string]any, 0, limit)
	for rows.Next() {
		var (
			ts          time.Time
			eventType   string
			requestID   sql.NullString
			sessionID   sql.NullString
			status      sql.NullString
			toolName    sql.NullString
			approvalID  sql.NullString
			decision    sql.NullString
			completedAt sql.NullTime
			errorText   sql.NullString
			payload     []byte
		)
		if err := rows.Scan(&ts, &eventType, &requestID, &sessionID, &status, &toolName, &approvalID, &decision, &completedAt, &errorText, &payload); err != nil {
			return nil, fmt.Errorf("scan metrics history: %w", err)
		}
		item := map[string]any{
			"type":      eventType,
			"timestamp": ts.Format(time.RFC3339),
		}
		if requestID.Valid {
			item["requestId"] = requestID.String
		}
		if sessionID.Valid {
			item["sessionId"] = sessionID.String
		}
		if status.Valid {
			item["status"] = status.String
		}
		if toolName.Valid {
			item["toolName"] = toolName.String
		}
		if approvalID.Valid {
			item["approvalId"] = approvalID.String
		}
		if decision.Valid {
			item["decision"] = decision.String
		}
		if completedAt.Valid {
			item["completedAt"] = completedAt.Time.Format(time.RFC3339)
		}
		if errorText.Valid && errorText.String != "" {
			item["error"] = errorText.String
		}
		if len(payload) > 0 {
			var extra map[string]any
			if err := json.Unmarshal(payload, &extra); err == nil {
				item["data"] = extra
			}
		}
		events = append(events, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate metrics history: %w", err)
	}
	return events, nil
}

func (s *Server) availableAuditTables(ctx context.Context) (map[string]bool, error) {
	return availableAuditTables(ctx, s.db)
}

func overallHealthStatus(components map[string]componentStatus) string {
	if components["postgres"].Status != "ok" || components["llm_provider"].Status != "ok" {
		return "unhealthy"
	}
	for name, component := range components {
		if name == "postgres" || name == "llm_provider" || component.Status == "ok" || component.Status == "skipped" {
			continue
		}
		return "degraded"
	}
	return "ok"
}

func ProbeHealth(ctx context.Context, cfg ServerConfig) (HealthResponse, error) {
	server, err := NewServer(cfg)
	if err != nil {
		return HealthResponse{}, err
	}
	if server.db != nil {
		defer server.db.Close()
	}
	return server.probeHealth(ctx, time.Now())
}

func FetchHealth(ctx context.Context, baseURL string) (HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/health", nil)
	if err != nil {
		return HealthResponse{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return HealthResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return HealthResponse{}, fmt.Errorf("health endpoint returned %s", resp.Status)
	}
	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return HealthResponse{}, err
	}
	return health, nil
}

func (s *Server) probeHealth(ctx context.Context, now time.Time) (HealthResponse, error) {
	components := map[string]componentStatus{
		"llm_provider": {
			Status: statusFromBool(s.providerName != ""),
		},
		"google_oauth": {
			Status: statusFromBool(s.googleOAuthConfigured),
		},
		"channel": {
			Status: statusFromBool(s.channelName != ""),
		},
		"tool_registry": {
			Status:    statusFromBool(s.toolCount > 0),
			ToolCount: s.toolCount,
		},
	}
	if s.tavilyConfigured {
		components["tavily"] = componentStatus{Status: "ok"}
	} else {
		components["tavily"] = componentStatus{Status: "skipped"}
	}

	postgres := componentStatus{Status: "unhealthy"}
	if s.db != nil {
		started := time.Now()
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := s.db.PingContext(pingCtx)
		cancel()
		if err == nil {
			postgres.Status = "ok"
			postgres.LatencyMS = time.Since(started).Milliseconds()
		}
	}
	components["postgres"] = postgres

	return HealthResponse{
		Status:     overallHealthStatus(components),
		Uptime:     now.Sub(s.startedAt).Round(time.Second).String(),
		CheckedAt:  now.Format(time.RFC3339),
		Components: components,
	}, nil
}

func statusFromBool(ok bool) string {
	if ok {
		return "ok"
	}
	return "unhealthy"
}

func parseLimit(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 20
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 20
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func parseSince(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
