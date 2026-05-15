package sandbox

import (
	"context"
	"log/slog"

	"github.com/l-lab/cloud-agents/internal/crypto"
	"github.com/l-lab/cloud-agents/internal/db"
)

// lookupUserAnthropicKey returns the decrypted Anthropic API key for the given
// user, or an empty string if the user has none configured or on any error.
func (m *Manager) lookupUserAnthropicKey(ctx context.Context, username string) string {
	var user db.User
	if err := m.gormDB.Where("user_name = ?", username).First(&user).Error; err != nil {
		slog.WarnContext(ctx, "lookup user for anthropic key failed", "username", username, "error", err)
		return ""
	}
	if user.AnthropicAPIKeyEnc == "" {
		return ""
	}
	key, err := crypto.Decrypt(user.AnthropicAPIKeyEnc, m.sshKeySecret)
	if err != nil {
		slog.WarnContext(ctx, "decrypt anthropic key failed", "username", username, "error", err)
		return ""
	}
	return key
}
