package memory

import "testing"

func TestStoreKeepsSlidingWindow(t *testing.T) {
	store := NewStore()

	for index := 0; index < 25; index++ {
		store.Append(RoleUser, "message")
	}

	history := store.GetHistory()
	if len(history) != 20 {
		t.Fatalf("unexpected history length: %d", len(history))
	}
}

func TestStoreClear(t *testing.T) {
	store := NewStore()
	store.Append(RoleUser, "hello")
	store.Clear()

	if len(store.GetHistory()) != 0 {
		t.Fatal("expected cleared store to be empty")
	}
}
