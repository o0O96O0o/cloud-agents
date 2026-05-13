package db

import "time"

type Kind struct {
	ID        int    `gorm:"primaryKey;autoIncrement"`
	UserID    uint   `gorm:"not null;uniqueIndex:uq_kinds_user_kind_name;index:ix_kinds_user_active"`
	Kind      string `gorm:"size:50;not null;uniqueIndex:uq_kinds_user_kind_name"`
	Name      string `gorm:"size:100;not null;uniqueIndex:uq_kinds_user_kind_name"`
	OFSPath   string `gorm:"size:512;not null"`
	Meta      string `gorm:"type:json;not null"`
	IsActive  bool   `gorm:"default:true;index:ix_kinds_user_active"`
	CreatedAt time.Time
	UpdatedAt time.Time

	User User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
}
