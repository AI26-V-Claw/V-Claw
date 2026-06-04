package sessions

import "testing"

func TestNewStoreFromEnvDefaultsToMemory(t *testing.T) {
	t.Setenv("VCLAW_SESSION_STORE", "")
	t.Setenv("VCLAW_REDIS_URL", "")

	store, err := NewStoreFromEnv()
	if err != nil {
		t.Fatalf("new store from env: %v", err)
	}
	if _, ok := store.(*InMemoryStore); !ok {
		t.Fatalf("expected in-memory store, got %T", store)
	}
}

func TestNewStoreFromEnvUsesRedisWhenURLIsSet(t *testing.T) {
	t.Setenv("VCLAW_SESSION_STORE", "")
	t.Setenv("VCLAW_REDIS_URL", "redis://localhost:6379/0")

	store, err := NewStoreFromEnv()
	if err != nil {
		t.Fatalf("new store from env: %v", err)
	}
	if _, ok := store.(*RedisStore); !ok {
		t.Fatalf("expected redis store, got %T", store)
	}
}

func TestNewStoreFromEnvRequiresRedisURL(t *testing.T) {
	t.Setenv("VCLAW_SESSION_STORE", "redis")
	t.Setenv("VCLAW_REDIS_URL", "")

	if _, err := NewStoreFromEnv(); err == nil {
		t.Fatal("expected redis URL error")
	}
}
