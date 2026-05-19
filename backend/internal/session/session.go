package session

import (
	"context"
	"encoding/json"
)

// SessionStore retrieves conversation history for a task.
// Implementations hide storage addressing details from callers.
type SessionStore interface {
	// GetHistory returns all history entries for the task across every session
	// directory (main agent + subagents). The frontend reconstructs the
	// conversation chain via parentUuid.
	GetHistory(ctx context.Context, username, taskID string) ([]json.RawMessage, error)
}
