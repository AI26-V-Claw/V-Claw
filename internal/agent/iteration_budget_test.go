package agent

import "testing"

func TestIterationBudgetConsumeRefundAndRemaining(t *testing.T) {
	budget := NewIterationBudget(2)

	if budget.Used() != 0 || budget.Remaining() != 2 || budget.MaxTotal() != 2 {
		t.Fatalf("unexpected initial budget: used=%d remaining=%d max=%d", budget.Used(), budget.Remaining(), budget.MaxTotal())
	}
	if !budget.Consume() || !budget.Consume() {
		t.Fatalf("expected first two consumes to succeed")
	}
	if budget.Consume() {
		t.Fatalf("expected exhausted budget to reject consume")
	}
	budget.Refund()
	if budget.Used() != 1 || budget.Remaining() != 1 {
		t.Fatalf("unexpected budget after refund: used=%d remaining=%d", budget.Used(), budget.Remaining())
	}
}

func TestIterationBudgetRefundDoesNotGoNegative(t *testing.T) {
	budget := NewIterationBudget(1)
	budget.Refund()

	if budget.Used() != 0 || budget.Remaining() != 1 {
		t.Fatalf("refund on empty budget should be a no-op: used=%d remaining=%d", budget.Used(), budget.Remaining())
	}
}
