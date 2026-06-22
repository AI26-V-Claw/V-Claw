package agent

import "sync"

type IterationBudget struct {
	maxTotal int
	used     int
	mu       sync.Mutex
}

func NewIterationBudget(maxTotal int) *IterationBudget {
	if maxTotal < 0 {
		maxTotal = 0
	}
	return &IterationBudget{maxTotal: maxTotal}
}

func (b *IterationBudget) Consume() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used >= b.maxTotal {
		return false
	}
	b.used++
	return true
}

func (b *IterationBudget) Refund() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used > 0 {
		b.used--
	}
}

func (b *IterationBudget) Used() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

func (b *IterationBudget) Remaining() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.maxTotal - b.used
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (b *IterationBudget) MaxTotal() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxTotal
}
