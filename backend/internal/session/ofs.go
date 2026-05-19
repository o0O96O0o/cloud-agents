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

func (s *OFSSessionStore) GetHistory(ctx context.Context, username, taskID string) ([]json.RawMessage, error) {
	entries, err := s.client.GetAllHistory(ctx, username, taskID)
	if err != nil {
		return nil, fmt.Errorf("getting history: %w", err)
	}
	return entries, nil
}
