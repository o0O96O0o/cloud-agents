package schedule

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/l-lab/cloud-agents/internal/db"
	"gorm.io/gorm"
)

// CreateRequest is the input for creating a new schedule.
type CreateRequest struct {
	Title       string            `json:"title"`
	Prompt      string            `json:"prompt"`
	CronExpr    string            `json:"cron_expr"`
	RunAt       *time.Time        `json:"run_at"`
	ExtraEnv    map[string]string `json:"extra_env"`
	GitURL      string            `json:"git_url"`
	TimeoutSecs int               `json:"timeout_secs"`
	Concurrency int               `json:"concurrency"` // 0=skip, 1=allow
}

// UpdateRequest is the input for updating an existing schedule.
type UpdateRequest struct {
	Title       *string           `json:"title"`
	Prompt      *string           `json:"prompt"`
	CronExpr    *string           `json:"cron_expr"`
	RunAt       *time.Time        `json:"run_at"`
	ExtraEnv    map[string]string `json:"extra_env"`
	GitURL      *string           `json:"git_url"`
	TimeoutSecs *int              `json:"timeout_secs"`
	Concurrency *int              `json:"concurrency"`
}

// Service handles CRUD for ScheduledTask records and notifies the Scheduler on changes.
type Service struct {
	db        *gorm.DB
	scheduler *Scheduler
}

func NewService(db *gorm.DB, scheduler *Scheduler) *Service {
	return &Service{db: db, scheduler: scheduler}
}

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ValidateCronExpr returns an error if expr is not a valid cron expression.
func ValidateCronExpr(expr string) error {
	if expr == "@once" {
		return nil
	}
	_, err := parser.Parse(expr)
	return err
}

func (s *Service) Create(ctx context.Context, userID uint, req CreateRequest) (*db.ScheduledTask, error) {
	if err := ValidateCronExpr(req.CronExpr); err != nil {
		return nil, fmt.Errorf("invalid cron_expr: %w", err)
	}
	if req.CronExpr == "@once" && req.RunAt == nil {
		return nil, errors.New("run_at is required for @once schedules")
	}
	if req.CronExpr == "@once" && req.RunAt != nil && req.RunAt.Before(time.Now()) {
		return nil, errors.New("run_at must be in the future")
	}
	if req.TimeoutSecs == 0 {
		req.TimeoutSecs = 1800
	}
	if req.TimeoutSecs < 60 || req.TimeoutSecs > 86400 {
		return nil, errors.New("timeout_secs must be between 60 and 86400")
	}
	if req.Concurrency != 0 && req.Concurrency != 1 {
		return nil, errors.New("concurrency must be 0 (skip) or 1 (allow)")
	}

	envJSON, err := json.Marshal(req.ExtraEnv)
	if err != nil {
		return nil, fmt.Errorf("marshal extra_env: %w", err)
	}

	var nextRun *time.Time
	if req.CronExpr != "@once" {
		sched, _ := parser.Parse(req.CronExpr)
		t := sched.Next(time.Now())
		nextRun = &t
	} else {
		nextRun = req.RunAt
	}

	rec := &db.ScheduledTask{
		ID:          uuid.New().String(),
		UserID:      userID,
		Title:       req.Title,
		Prompt:      req.Prompt,
		CronExpr:    req.CronExpr,
		RunAt:       req.RunAt,
		ExtraEnv:    string(envJSON),
		GitURL:      req.GitURL,
		TimeoutSecs: req.TimeoutSecs,
		Concurrency: req.Concurrency,
		Enabled:     true,
		NextRunAt:   nextRun,
	}
	if err := s.db.WithContext(ctx).Create(rec).Error; err != nil {
		return nil, fmt.Errorf("create scheduled_task: %w", err)
	}
	s.scheduler.Reload(rec.ID)
	return rec, nil
}

func (s *Service) Update(ctx context.Context, id string, userID uint, req UpdateRequest) (*db.ScheduledTask, error) {
	rec, err := s.getOwned(ctx, id, userID)
	if err != nil {
		return nil, err
	}

	if req.Title != nil {
		rec.Title = *req.Title
	}
	if req.Prompt != nil {
		rec.Prompt = *req.Prompt
	}
	if req.CronExpr != nil {
		if err := ValidateCronExpr(*req.CronExpr); err != nil {
			return nil, fmt.Errorf("invalid cron_expr: %w", err)
		}
		rec.CronExpr = *req.CronExpr
		if *req.CronExpr != "@once" {
			sched, _ := parser.Parse(*req.CronExpr)
			t := sched.Next(time.Now())
			rec.NextRunAt = &t
		}
	}
	if req.RunAt != nil {
		rec.RunAt = req.RunAt
		if rec.CronExpr == "@once" {
			rec.NextRunAt = req.RunAt
		}
	}
	if req.ExtraEnv != nil {
		envJSON, err := json.Marshal(req.ExtraEnv)
		if err != nil {
			return nil, fmt.Errorf("marshal extra_env: %w", err)
		}
		rec.ExtraEnv = string(envJSON)
	}
	if req.GitURL != nil {
		rec.GitURL = *req.GitURL
	}
	if req.TimeoutSecs != nil {
		if *req.TimeoutSecs < 60 || *req.TimeoutSecs > 86400 {
			return nil, errors.New("timeout_secs must be between 60 and 86400")
		}
		rec.TimeoutSecs = *req.TimeoutSecs
	}
	if req.Concurrency != nil {
		if *req.Concurrency != 0 && *req.Concurrency != 1 {
			return nil, errors.New("concurrency must be 0 or 1")
		}
		rec.Concurrency = *req.Concurrency
	}

	if err := s.db.WithContext(ctx).Save(rec).Error; err != nil {
		return nil, fmt.Errorf("update scheduled_task: %w", err)
	}
	s.scheduler.Reload(rec.ID)
	return rec, nil
}

func (s *Service) Delete(ctx context.Context, id string, userID uint) error {
	if _, err := s.getOwned(ctx, id, userID); err != nil {
		return err
	}
	s.scheduler.Remove(id)
	return s.db.WithContext(ctx).Delete(&db.ScheduledTask{}, "id = ?", id).Error
}

func (s *Service) Get(ctx context.Context, id string, userID uint) (*db.ScheduledTask, error) {
	return s.getOwned(ctx, id, userID)
}

func (s *Service) List(ctx context.Context, userID uint) ([]db.ScheduledTask, error) {
	var recs []db.ScheduledTask
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&recs).Error; err != nil {
		return nil, fmt.Errorf("list scheduled_tasks: %w", err)
	}
	return recs, nil
}

func (s *Service) Toggle(ctx context.Context, id string, userID uint, enabled bool) error {
	if _, err := s.getOwned(ctx, id, userID); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Model(&db.ScheduledTask{}).
		Where("id = ?", id).
		Update("enabled", enabled).Error; err != nil {
		return fmt.Errorf("toggle schedule: %w", err)
	}
	if enabled {
		s.scheduler.Reload(id)
	} else {
		s.scheduler.Remove(id)
	}
	return nil
}

func (s *Service) getOwned(ctx context.Context, id string, userID uint) (*db.ScheduledTask, error) {
	var rec db.ScheduledTask
	if err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get scheduled_task: %w", err)
	}
	return &rec, nil
}

var ErrNotFound = errors.New("schedule not found")

// GenerateToken creates a new fire token for the given schedule, revoking any existing one.
// The raw token is returned once and never stored; only its SHA-256 hash is persisted.
func (s *Service) GenerateToken(ctx context.Context, scheduleID string, userID uint) (string, *db.ScheduleToken, error) {
	if _, err := s.getOwned(ctx, scheduleID, userID); err != nil {
		return "", nil, err
	}

	// Revoke any existing active token for this schedule.
	now := time.Now()
	s.db.WithContext(ctx).Model(&db.ScheduleToken{}).
		Where("schedule_id = ? AND revoked_at IS NULL", scheduleID).
		Update("revoked_at", now)

	// Generate 32 random bytes → 64-char hex raw token.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate token bytes: %w", err)
	}
	rawHex := hex.EncodeToString(raw)

	// Store only the SHA-256 hash.
	h := sha256.Sum256([]byte(rawHex))
	tokenHash := hex.EncodeToString(h[:])

	rec := &db.ScheduleToken{
		ID:         uuid.New().String(),
		ScheduleID: scheduleID,
		TokenHash:  tokenHash,
	}
	if err := s.db.WithContext(ctx).Create(rec).Error; err != nil {
		return "", nil, fmt.Errorf("create schedule token: %w", err)
	}
	return rawHex, rec, nil
}

// RevokeToken revokes the active fire token for the given schedule (no-op if none exists).
func (s *Service) RevokeToken(ctx context.Context, scheduleID string, userID uint) error {
	if _, err := s.getOwned(ctx, scheduleID, userID); err != nil {
		return err
	}
	now := time.Now()
	return s.db.WithContext(ctx).Model(&db.ScheduleToken{}).
		Where("schedule_id = ? AND revoked_at IS NULL", scheduleID).
		Update("revoked_at", now).Error
}

// LookupScheduleByToken hashes the raw token and returns the matching schedule.
func (s *Service) LookupScheduleByToken(ctx context.Context, rawToken string) (*db.ScheduledTask, error) {
	h := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(h[:])

	var tok db.ScheduleToken
	if err := s.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL", tokenHash).
		First(&tok).Error; err != nil {
		return nil, fmt.Errorf("lookup token: %w", err)
	}

	var sched db.ScheduledTask
	if err := s.db.WithContext(ctx).Where("id = ?", tok.ScheduleID).First(&sched).Error; err != nil {
		return nil, fmt.Errorf("load schedule for token: %w", err)
	}
	return &sched, nil
}
