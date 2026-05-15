package db

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// UserRepository abstracts user storage access for handlers.
type UserRepository interface {
	FindByCredentials(ctx context.Context, userName, password string) (*User, error)
	CreateWithPassword(ctx context.Context, userName, email, password string) (*User, error)
	UpdateSSHKey(ctx context.Context, userName, encryptedKey string) error
	UpdateAnthropicAPIKey(ctx context.Context, userName, encryptedKey string) error
}

type MySQLUserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *MySQLUserRepository {
	return &MySQLUserRepository{db: db}
}

func (r *MySQLUserRepository) FindByCredentials(ctx context.Context, userName, password string) (*User, error) {
	return FindByCredentials(r.db.WithContext(ctx), userName, password)
}

func (r *MySQLUserRepository) CreateWithPassword(ctx context.Context, userName, email, password string) (*User, error) {
	return CreateWithPassword(r.db.WithContext(ctx), userName, email, password)
}

func (r *MySQLUserRepository) UpdateSSHKey(ctx context.Context, userName, encryptedKey string) error {
	if err := r.db.WithContext(ctx).Model(&User{}).
		Where("user_name = ?", userName).
		Update("ssh_private_key_enc", encryptedKey).Error; err != nil {
		return fmt.Errorf("update ssh key for %q: %w", userName, err)
	}
	return nil
}

func (r *MySQLUserRepository) UpdateAnthropicAPIKey(ctx context.Context, userName, encryptedKey string) error {
	if err := r.db.WithContext(ctx).Model(&User{}).
		Where("user_name = ?", userName).
		Update("anthropic_api_key_enc", encryptedKey).Error; err != nil {
		return fmt.Errorf("update anthropic key for %q: %w", userName, err)
	}
	return nil
}
