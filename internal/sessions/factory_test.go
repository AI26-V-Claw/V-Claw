package sessions

import "testing"

func TestNewStoreFromEnvReturnsFileStore(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	store, err := NewStoreFromEnv()
	if err != nil {
		t.Fatalf("NewStoreFromEnv: %v", err)
	}
	if _, ok := store.(*FileStore); !ok {
		t.Fatalf("expected *FileStore, got %T", store)
	}
}
