package session

import (
	"context"
	"encoding/json"
)

// SessionStore retrieves conversation history for a task.
// Implementations hide storage addressing details from callers.
type SessionStore interface {
	// GetHistory returns one page of history entries and the cursor for the next
	// older page. cursor="" requests the most recent session. nextCursor="" means
	// no more history is available.
	GetHistory(ctx context.Context, username, taskID, cursor string) (entries []json.RawMessage, nextCursor string, err error)
}
