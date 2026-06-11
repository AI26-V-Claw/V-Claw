package monitoring

import (
	"sync/atomic"
	"time"

	"vclaw/internal/agent"
	"vclaw/internal/contracts"
)

type Metrics struct {
	startedAt              time.Time
	requestsTotal          atomic.Uint64
	requestsFailed         atomic.Uint64
	toolCallsTotal         atomic.Uint64
	approvalsPending       atomic.Int64
	approvalsApprovedTotal atomic.Uint64
	approvalsRejectedTotal atomic.Uint64
	approvalsExpiredTotal  atomic.Uint64
}

type Snapshot struct {
	RequestsTotal          uint64 `json:"requests_total"`
	RequestsFailed         uint64 `json:"requests_failed"`
	ToolCallsTotal         uint64 `json:"tool_calls_total"`
	ApprovalsPending       int64  `json:"approvals_pending"`
	ApprovalsApprovedTotal uint64 `json:"approvals_approved_total"`
	ApprovalsRejectedTotal uint64 `json:"approvals_rejected_total"`
	ApprovalsExpiredTotal  uint64 `json:"approvals_expired_total"`
	UptimeSeconds          int64  `json:"uptime_seconds"`
}

func NewMetrics(now time.Time) *Metrics {
	if now.IsZero() {
		now = time.Now()
	}
	return &Metrics{startedAt: now}
}

func (m *Metrics) RecordRequest(response contracts.AgentResponse, err error) {
	if m == nil {
		return
	}
	m.requestsTotal.Add(1)
	if err != nil || response.Status == contracts.AgentStatusFailed {
		m.requestsFailed.Add(1)
	}
}

func (m *Metrics) RecordToolCall(_ string, _ bool) {
	if m == nil {
		return
	}
	m.toolCallsTotal.Add(1)
}

func (m *Metrics) RecordApprovalStateChange(status agent.ActionStatus, pending int) {
	if m == nil {
		return
	}
	m.approvalsPending.Store(int64(pending))
	switch status {
	case agent.ActionStatusApproved:
		m.approvalsApprovedTotal.Add(1)
	case agent.ActionStatusRejected:
		m.approvalsRejectedTotal.Add(1)
	case agent.ActionStatusExpired:
		m.approvalsExpiredTotal.Add(1)
	}
}

func (m *Metrics) Snapshot(now time.Time) Snapshot {
	if m == nil {
		return Snapshot{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	uptime := int64(now.Sub(m.startedAt).Seconds())
	if uptime < 0 {
		uptime = 0
	}
	return Snapshot{
		RequestsTotal:          m.requestsTotal.Load(),
		RequestsFailed:         m.requestsFailed.Load(),
		ToolCallsTotal:         m.toolCallsTotal.Load(),
		ApprovalsPending:       m.approvalsPending.Load(),
		ApprovalsApprovedTotal: m.approvalsApprovedTotal.Load(),
		ApprovalsRejectedTotal: m.approvalsRejectedTotal.Load(),
		ApprovalsExpiredTotal:  m.approvalsExpiredTotal.Load(),
		UptimeSeconds:          uptime,
	}
}

func (m *Metrics) StartedAt() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.startedAt
}
