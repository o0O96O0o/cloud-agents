package db

import "time"

type Task struct {
	ID          string `gorm:"primaryKey;size:36"`
	Username    string `gorm:"size:100;not null;index"`
	State       int    `gorm:"not null;default:0"`
	Title       string `gorm:"size:255"`
	SessionID   string `gorm:"size:36"`
	ExtraEnv    string `gorm:"type:text"`
	Provisioned bool   `gorm:"default:false"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
