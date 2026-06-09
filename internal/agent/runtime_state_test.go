package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeStateStoreFindOrCreateActionIsAtomicByIdempotencyKey(t *testing.T) {
	store := NewInMemoryRuntimeStateStore()
	start := make(chan struct{})
	results := make(chan ActionRecord, 2)
	var createdCount atomic.Int32
	var wg sync.WaitGroup

	for _, actionID := range []string{"act_first", "act_second"} {
		actionID := actionID
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			record, created, err := store.FindOrCreateAction(context.Background(), ActionRecord{
				ActionID:       actionID,
				RunID:          "run_001",
				SessionID:      "sess_001",
				Status:         ActionStatusPendingApproval,
				IdempotencyKey: "same-write",
				CreatedAt:      time.Now(),
			})
			if err != nil {
				t.Errorf("find or create action: %v", err)
				return
			}
			if created {
				createdCount.Add(1)
			}
			results <- record
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	if createdCount.Load() != 1 {
		t.Fatalf("created actions = %d, want 1", createdCount.Load())
	}
	var canonicalID string
	for record := range results {
		if canonicalID == "" {
			canonicalID = record.ActionID
		}
		if record.ActionID != canonicalID {
			t.Fatalf("concurrent calls returned different actions: %q and %q", canonicalID, record.ActionID)
		}
	}
	if len(store.actions) != 1 {
		t.Fatalf("stored actions = %d, want 1", len(store.actions))
	}
}
