package memory

import "testing"

func TestStoreKeepsSlidingWindow(t *testing.T) {
	store := NewStore()

	for index := 0; index < 25; index++ {
		store.Append("session-1", RoleUser, "message")
	}

	history := store.GetHistory("session-1")
	if len(history) != 20 {
		t.Fatalf("unexpected history length: %d", len(history))
	}
}

func TestStoreClear(t *testing.T) {
	store := NewStore()
	store.Append("session-1", RoleUser, "hello")
	store.Clear("session-1")

	if len(store.GetHistory("session-1")) != 0 {
		t.Fatal("expected cleared store to be empty")
	}
}

func TestStoreSeparatesSessions(t *testing.T) {
	store := NewStore()
	store.Append("session-1", RoleUser, "hello")
	store.Append("session-2", RoleUser, "world")

	if len(store.GetHistory("session-1")) != 1 {
		t.Fatalf("expected session-1 history to be isolated")
	}
	if len(store.GetHistory("session-2")) != 1 {
		t.Fatalf("expected session-2 history to be isolated")
	}
}
