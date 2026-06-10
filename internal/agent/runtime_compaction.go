package agent

import (
	"context"
	"time"

	"vclaw/internal/sessions"
)

func (r *Runtime) maybeCompactAsync(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	transcript, err := r.sessionStore.LoadTranscript(ctx, sessionID)
	if err != nil {
		r.logger.Warn("compaction: load transcript failed", "session_id", sessionID, "error", err)
		return
	}

	memory, errShape := r.loadSessionMemory(ctx, sessionID)
	if errShape != nil {
		r.logger.Warn("compaction: load memory failed", "session_id", sessionID, "error", errShape.Message)
		return
	}

	guard := sessions.CompactorGuard{
		HasPendingApproval: func(sid string) bool {
			return r.HasPendingApproval(ctx, sid)
		},
		HasPendingClarification: func(string) bool {
			return memory.PendingClarification != nil
		},
	}

	result, err := r.compactor.MaybeCompact(ctx, sessionID, transcript, memory, guard)
	if err != nil {
		r.logger.Warn("compaction: summarization failed", "session_id", sessionID, "error", err)
		return
	}
	if !result.Compacted {
		if result.SkipReason != "below_threshold" {
			r.logger.Debug("compaction skipped", "session_id", sessionID, "reason", result.SkipReason)
		}
		return
	}

	if err := r.sessionStore.SetTranscript(ctx, sessionID, result.KeptMessages); err != nil {
		r.logger.Warn("compaction: set transcript failed", "session_id", sessionID, "error", err)
		return
	}

	latest, errShape := r.loadSessionMemory(ctx, sessionID)
	if errShape != nil {
		r.logger.Warn("compaction: reload memory failed", "session_id", sessionID, "error", errShape.Message)
		return
	}
	latest.Summary = result.Summary
	if errShape := r.saveSessionMemory(ctx, sessionID, latest); errShape != nil {
		r.logger.Warn("compaction: save summary failed", "session_id", sessionID, "error", errShape.Message)
		return
	}

	r.logger.Info("session compacted",
		"session_id", sessionID,
		"messages_kept", len(result.KeptMessages),
	)
}
