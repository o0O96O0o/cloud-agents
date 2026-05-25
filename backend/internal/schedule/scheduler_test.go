package schedule

import (
	"context"
	"testing"
	"time"

	"github.com/l-lab/cloud-agents/internal/db"
)

func TestStart_CatchesUpMissedRecurringRun(t *testing.T) {
	g := newTestDB(t)
	uid := insertUser(t, g, "alice")

	past := time.Now().Add(-2 * time.Hour)
	rec := &db.ScheduledTask{
		ID: "sched-missed", UserID: uid,
		Title: "missed daily", Prompt: "p", CronExpr: "@daily",
		TimeoutSecs: 1800, Enabled: true,
		NextRunAt: &past,
	}
	if err := g.Create(rec).Error; err != nil {
		t.Fatalf("insert schedule: %v", err)
	}

	svc := newMockTaskSvc()
	s := NewScheduler(g, svc)
	s.Start(context.Background())
	defer s.Stop()

	svc.waitDone(t)

	svc.mu.Lock()
	count := len(svc.created)
	svc.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 catch-up task created, got %d", count)
	}

	var updated db.ScheduledTask
	g.Where("id = ?", rec.ID).First(&updated)
	if updated.LastRunAt == nil {
		t.Error("expected last_run_at to be set after catch-up")
	}
	if updated.NextRunAt == nil || !updated.NextRunAt.After(time.Now()) {
		t.Errorf("expected next_run_at to be advanced into the future, got %v", updated.NextRunAt)
	}
}

func TestStart_NoCatchUpWhenNextRunInFuture(t *testing.T) {
	g := newTestDB(t)
	uid := insertUser(t, g, "alice")

	future := time.Now().Add(2 * time.Hour)
	rec := &db.ScheduledTask{
		ID: "sched-future", UserID: uid,
		Prompt: "p", CronExpr: "@daily",
		TimeoutSecs: 1800, Enabled: true,
		NextRunAt: &future,
	}
	g.Create(rec)

	svc := newMockTaskSvc()
	s := NewScheduler(g, svc)
	s.Start(context.Background())
	defer s.Stop()

	// Give the scheduler a moment to launch any (incorrect) catch-up goroutine.
	time.Sleep(50 * time.Millisecond)

	svc.mu.Lock()
	count := len(svc.created)
	svc.mu.Unlock()
	if count != 0 {
		t.Errorf("expected no task created when next_run_at is future, got %d", count)
	}
}

func TestStart_NoCatchUpWhenNextRunAtIsNil(t *testing.T) {
	g := newTestDB(t)
	uid := insertUser(t, g, "alice")

	// Brand-new schedule that has never fired — NextRunAt is nil.
	rec := &db.ScheduledTask{
		ID: "sched-fresh", UserID: uid,
		Prompt: "p", CronExpr: "@daily",
		TimeoutSecs: 1800, Enabled: true,
	}
	g.Create(rec)

	svc := newMockTaskSvc()
	s := NewScheduler(g, svc)
	s.Start(context.Background())
	defer s.Stop()

	time.Sleep(50 * time.Millisecond)

	svc.mu.Lock()
	count := len(svc.created)
	svc.mu.Unlock()
	if count != 0 {
		t.Errorf("expected no task created when next_run_at is nil, got %d", count)
	}
}

func TestStart_CatchesUpMissedOnceRun(t *testing.T) {
	g := newTestDB(t)
	uid := insertUser(t, g, "alice")

	past := time.Now().Add(-time.Hour)
	rec := &db.ScheduledTask{
		ID: "sched-once-missed", UserID: uid,
		Prompt: "p", CronExpr: "@once",
		RunAt: &past, TimeoutSecs: 1800, Enabled: true,
	}
	g.Create(rec)

	svc := newMockTaskSvc()
	s := NewScheduler(g, svc)
	s.Start(context.Background())
	defer s.Stop()

	svc.waitDone(t)

	svc.mu.Lock()
	count := len(svc.created)
	svc.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 catch-up task for missed @once, got %d", count)
	}

	// RunFire disables @once after firing.
	var updated db.ScheduledTask
	g.Where("id = ?", rec.ID).First(&updated)
	if updated.Enabled {
		t.Error("expected @once schedule to be disabled after catch-up fire")
	}
}

func TestStart_CatchUpRespectsConcurrencySkip(t *testing.T) {
	g := newTestDB(t)
	uid := insertUser(t, g, "alice")

	past := time.Now().Add(-time.Hour)
	rec := &db.ScheduledTask{
		ID: "sched-skip-catchup", UserID: uid,
		Prompt: "p", CronExpr: "@daily",
		TimeoutSecs: 1800, Concurrency: 0, Enabled: true,
		NextRunAt: &past,
	}
	g.Create(rec)

	// A run from before the restart is still recorded as active. The catch-up
	// must skip rather than fire a duplicate.
	schedID := rec.ID
	g.Create(&db.Task{
		ID: "still-running", UserID: uid,
		State: 2, ScheduleID: &schedID,
	})

	svc := newMockTaskSvc()
	s := NewScheduler(g, svc)
	s.Start(context.Background())
	defer s.Stop()

	time.Sleep(50 * time.Millisecond)

	svc.mu.Lock()
	count := len(svc.created)
	svc.mu.Unlock()
	if count != 0 {
		t.Errorf("expected catch-up to skip when prior run is active, got %d new tasks", count)
	}
}
