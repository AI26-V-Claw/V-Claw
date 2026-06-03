package sessions

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	StoreModeMemory = "memory"
	StoreModeRedis  = "redis"
)

func NewStoreFromEnv() (Store, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("VCLAW_SESSION_STORE")))
	redisURL := strings.TrimSpace(os.Getenv("VCLAW_REDIS_URL"))
	if mode == "" {
		if redisURL != "" {
			mode = StoreModeRedis
		} else {
			mode = StoreModeMemory
		}
	}
	switch mode {
	case StoreModeMemory, "inmemory", "in-memory":
		return NewInMemoryStore(), nil
	case StoreModeRedis:
		if redisURL == "" {
			return nil, fmt.Errorf("VCLAW_REDIS_URL is required when VCLAW_SESSION_STORE=redis")
		}
		return NewRedisStore(RedisStoreConfig{
			URL:         redisURL,
			KeyPrefix:   strings.TrimSpace(os.Getenv("VCLAW_REDIS_KEY_PREFIX")),
			MaxMessages: envInt("VCLAW_SESSION_MAX_MESSAGES", defaultRedisMaxMessages),
			TTL:         time.Duration(envInt("VCLAW_SESSION_TTL_SECONDS", 24*60*60)) * time.Second,
		})
	default:
		return nil, fmt.Errorf("VCLAW_SESSION_STORE must be one of: memory, redis")
	}
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
