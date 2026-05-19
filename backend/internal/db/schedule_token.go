package db

import "time"

// ScheduleToken stores a hashed fire token for a scheduled task.
// Raw tokens are never stored; only SHA-256(raw) is persisted.
type ScheduleToken struct {
	ID         string     `gorm:"primaryKey;size:36"`
	ScheduleID string     `gorm:"not null;size:36;index"`
	TokenHash  string     `gorm:"not null;size:64"` // hex(sha256(rawToken))
	CreatedAt  time.Time
	RevokedAt  *time.Time `gorm:"default:null"`
}
