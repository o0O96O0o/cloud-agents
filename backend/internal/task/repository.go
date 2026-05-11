package task

import "context"

// Repository is the storage interface for Tasks. Implementations include
// MemoryRepository (for local dev / tests) and RedisRepository.
type Repository interface {
	Create(ctx context.Context, username string, extraEnv map[string]string) (*Task, error)
	// Get returns nil, nil when the task does not exist.
	Get(ctx context.Context, id string) (*Task, error)
	Delete(ctx context.Context, id string) error
}

// taskOps is the optional persistence hook for a Task.
// nil means all state lives in the Task struct's own fields and mutexes.
// RedisRepository sets this to a redisTaskOps instance so mutation methods
// persist to Redis in addition to updating the local snapshot.
type taskOps interface {
	persistRunning(sandboxID, proxyBaseURL string, proxyHeaders map[string]string)
	persistProvisioning()
	persistError()
	// persistSessionID writes sessionID only if the field is currently empty.
	// Returns true if the value was actually written (was empty before).
	persistSessionID(sessionID string) bool
	ensureProvisioned(fn func() error) error
	// resetIfExpired returns (wasReset, err). wasReset=true means the task
	// fields were cleared in Redis and the caller should update its local snapshot.
	resetIfExpired(isAlive func(string) (bool, error)) (bool, error)
	resetForReprovisioning()
}
