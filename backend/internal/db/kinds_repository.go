package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type KindRecord struct {
	ID        int             `json:"id"`
	UserID    uint            `json:"-"`
	Kind      string          `json:"kind"`
	Name      string          `json:"name"`
	OFSPath   string          `json:"ofs_path"`
	Meta      json.RawMessage `json:"meta"`
	IsActive  bool            `json:"is_active"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type KindUpdate struct {
	Meta     json.RawMessage // nil = no update
	IsActive *bool           // nil = no update
}

type KindsRepository interface {
	Create(ctx context.Context, userID uint, kind, name, ofsPath string, meta json.RawMessage) (*KindRecord, error)
	Get(ctx context.Context, id int, userID uint) (*KindRecord, error)
	List(ctx context.Context, userID uint) ([]*KindRecord, error)
	ListActive(ctx context.Context, userID uint) ([]*KindRecord, error)
	Update(ctx context.Context, id int, userID uint, update KindUpdate) (*KindRecord, error)
	Delete(ctx context.Context, id int, userID uint) error
}

type MySQLKindsRepository struct {
	db *gorm.DB
}

func NewKindsRepository(db *gorm.DB) *MySQLKindsRepository {
	return &MySQLKindsRepository{db: db}
}

func (r *MySQLKindsRepository) Create(ctx context.Context, userID uint, kind, name, ofsPath string, meta json.RawMessage) (*KindRecord, error) {
	metaStr := "{}"
	if len(meta) > 0 {
		metaStr = string(meta)
	}
	row := Kind{
		UserID:   userID,
		Kind:     kind,
		Name:     name,
		OFSPath:  ofsPath,
		Meta:     metaStr,
		IsActive: true,
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, fmt.Errorf("create kind: %w", err)
	}
	return rowToRecord(&row), nil
}

func (r *MySQLKindsRepository) Get(ctx context.Context, id int, userID uint) (*KindRecord, error) {
	return r.getByID(ctx, id, userID)
}

func (r *MySQLKindsRepository) List(ctx context.Context, userID uint) ([]*KindRecord, error) {
	var rows []Kind
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list kinds: %w", err)
	}
	records := make([]*KindRecord, len(rows))
	for i := range rows {
		records[i] = rowToRecord(&rows[i])
	}
	return records, nil
}

func (r *MySQLKindsRepository) ListActive(ctx context.Context, userID uint) ([]*KindRecord, error) {
	var rows []Kind
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND is_active = ?", userID, true).
		Order("id").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list active kinds: %w", err)
	}
	records := make([]*KindRecord, len(rows))
	for i := range rows {
		records[i] = rowToRecord(&rows[i])
	}
	return records, nil
}

func (r *MySQLKindsRepository) Update(ctx context.Context, id int, userID uint, update KindUpdate) (*KindRecord, error) {
	rec, err := r.getByID(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if update.Meta != nil {
		if err := r.db.WithContext(ctx).Model(&Kind{}).
			Where("id = ? AND user_id = ?", id, userID).
			Update("meta", string(update.Meta)).Error; err != nil {
			return nil, fmt.Errorf("update meta for kind %d: %w", id, err)
		}
		rec.Meta = update.Meta
	}
	if update.IsActive != nil {
		if err := r.db.WithContext(ctx).Model(&Kind{}).
			Where("id = ? AND user_id = ?", id, userID).
			Update("is_active", *update.IsActive).Error; err != nil {
			return nil, fmt.Errorf("update is_active for kind %d: %w", id, err)
		}
		rec.IsActive = *update.IsActive
	}
	return rec, nil
}

func (r *MySQLKindsRepository) Delete(ctx context.Context, id int, userID uint) error {
	result := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&Kind{})
	if result.Error != nil {
		return fmt.Errorf("delete kind %d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("kind %d not found", id)
	}
	return nil
}

func (r *MySQLKindsRepository) getByID(ctx context.Context, id int, userID uint) (*KindRecord, error) {
	var row Kind
	if err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("kind %d not found", id)
		}
		return nil, fmt.Errorf("get kind %d: %w", id, err)
	}
	return rowToRecord(&row), nil
}

func rowToRecord(row *Kind) *KindRecord {
	meta := json.RawMessage(row.Meta)
	if len(meta) == 0 {
		meta = json.RawMessage("{}")
	}
	return &KindRecord{
		ID:        row.ID,
		UserID:    row.UserID,
		Kind:      row.Kind,
		Name:      row.Name,
		OFSPath:   row.OFSPath,
		Meta:      meta,
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}
