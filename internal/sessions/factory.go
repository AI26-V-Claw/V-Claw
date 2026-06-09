package sessions

import "os"

// NewStoreFromEnv creates the session store used by the agent runtime.
// Sessions are persisted as JSON files under DATA_DIR/sessions/ so they survive
// process restarts. No external server is required.
func NewStoreFromEnv() (Store, error) {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}
	return NewFileStore(dataDir)
}
