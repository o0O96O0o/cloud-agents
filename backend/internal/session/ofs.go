package session

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/l-lab/cloud-agents/internal/storage"
)

// OFSSessionStore implements SessionStore backed by an OFS S3 client.
type OFSSessionStore struct {
	client storage.OFSClient
}

func NewOFSSessionStore(client storage.OFSClient) *OFSSessionStore {
	return &OFSSessionStore{client: client}
}

func (s *OFSSessionStore) GetHistory(ctx context.Context, username, taskID, cursor string) ([]json.RawMessage, string, error) {
	entries, nextCursor, err := s.client.GetHistoryPage(ctx, username, taskID, cursor)
	if err != nil {
		return nil, "", fmt.Errorf("getting history page: %w", err)
	}
	return entries, nextCursor, nil
}
