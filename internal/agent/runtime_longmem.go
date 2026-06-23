package agent

import (
	"context"

	"vclaw/internal/longmem"
	"vclaw/internal/providers"
)

func (r *Runtime) recordRepeatedLongTermHabits(ctx context.Context, sessionID, runID, requestID string, transcript []providers.Message) {
	if r == nil || r.ltMemFlusher == nil {
		return
	}
	if err := r.ltMemFlusher.RecordRepeatedHabits(context.WithoutCancel(ctx), longmem.HabitInput{
		Transcript: transcript,
		SessionID:  sessionID,
		RunID:      runID,
		RequestID:  requestID,
		ObservedAt: r.now().UTC(),
	}); err != nil {
		r.logger.Warn("long-term memory habit record failed", "error", err)
	}
}
