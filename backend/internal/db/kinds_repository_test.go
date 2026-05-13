package db

import (
	"context"
	"encoding/json"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestKindsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&User{}, &Kind{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func createUser(t *testing.T, db *gorm.DB, name string) uint {
	t.Helper()
	u := User{
		UserName:     name,
		Email:        name + "@test.com",
		PasswordHash: "x",
		IsActive:     true,
		AuthSource:   AuthSourcePassword,
	}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	return u.ID
}

func TestKindsRepo_CreateAndGet(t *testing.T) {
	db := newTestKindsDB(t)
	uid := createUser(t, db, "alice")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	meta := json.RawMessage(`{"description":"search skill"}`)
	rec, err := repo.Create(ctx, uid, "skill", "my-search", "alice/resources/skills/my-search/", meta)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rec.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if rec.Kind != "skill" {
		t.Errorf("Kind = %q, want skill", rec.Kind)
	}
	if rec.Name != "my-search" {
		t.Errorf("Name = %q, want my-search", rec.Name)
	}
	if rec.OFSPath != "alice/resources/skills/my-search/" {
		t.Errorf("OFSPath = %q", rec.OFSPath)
	}
	if string(rec.Meta) != string(meta) {
		t.Errorf("Meta = %s, want %s", rec.Meta, meta)
	}
	if !rec.IsActive {
		t.Error("expected IsActive=true")
	}
	if rec.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	got, err := repo.Get(ctx, rec.ID, uid)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != rec.Name {
		t.Errorf("Get: Name = %q, want %q", got.Name, rec.Name)
	}
}

func TestKindsRepo_Create_DefaultMeta(t *testing.T) {
	db := newTestKindsDB(t)
	uid := createUser(t, db, "bob")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	rec, err := repo.Create(ctx, uid, "skill", "bare", "bob/resources/skills/bare/", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if string(rec.Meta) != "{}" {
		t.Errorf("expected default meta {}, got %s", rec.Meta)
	}
}

func TestKindsRepo_Create_DuplicateName(t *testing.T) {
	db := newTestKindsDB(t)
	uid := createUser(t, db, "carol")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, uid, "skill", "foo", "carol/resources/skills/foo/", nil); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := repo.Create(ctx, uid, "skill", "foo", "carol/resources/skills/foo/", nil); err == nil {
		t.Error("expected error on duplicate (user_id, kind, name), got nil")
	}
}

func TestKindsRepo_Create_SameNameDifferentUser(t *testing.T) {
	db := newTestKindsDB(t)
	uid1 := createUser(t, db, "dave")
	uid2 := createUser(t, db, "eve")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, uid1, "skill", "shared", "dave/resources/skills/shared/", nil); err != nil {
		t.Fatalf("Create user1: %v", err)
	}
	if _, err := repo.Create(ctx, uid2, "skill", "shared", "eve/resources/skills/shared/", nil); err != nil {
		t.Errorf("Create user2 with same name should succeed, got: %v", err)
	}
}

func TestKindsRepo_List_ReturnsOwnedOnly(t *testing.T) {
	db := newTestKindsDB(t)
	uid1 := createUser(t, db, "frank")
	uid2 := createUser(t, db, "grace")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	repo.Create(ctx, uid1, "skill", "s1", "frank/resources/skills/s1/", nil)
	repo.Create(ctx, uid1, "skill", "s2", "frank/resources/skills/s2/", nil)
	repo.Create(ctx, uid2, "skill", "s3", "grace/resources/skills/s3/", nil)

	records, err := repo.List(ctx, uid1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records for uid1, got %d", len(records))
	}
	for _, r := range records {
		if r.UserID != uid1 {
			t.Errorf("got record owned by %d, want %d", r.UserID, uid1)
		}
	}
}

func TestKindsRepo_ListActive_ExcludesInactive(t *testing.T) {
	db := newTestKindsDB(t)
	uid := createUser(t, db, "henry")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	rec, _ := repo.Create(ctx, uid, "skill", "active-skill", "henry/resources/skills/active-skill/", nil)
	inactive, _ := repo.Create(ctx, uid, "skill", "inactive-skill", "henry/resources/skills/inactive-skill/", nil)

	f := false
	repo.Update(ctx, inactive.ID, uid, KindUpdate{IsActive: &f})

	records, err := repo.ListActive(ctx, uid)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 active record, got %d", len(records))
	}
	if records[0].ID != rec.ID {
		t.Errorf("expected active record ID=%d, got %d", rec.ID, records[0].ID)
	}
}

func TestKindsRepo_Update_Meta(t *testing.T) {
	db := newTestKindsDB(t)
	uid := createUser(t, db, "ivan")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	rec, _ := repo.Create(ctx, uid, "mcp", "github", "ivan/resources/mcp/github.json", json.RawMessage(`{"type":"stdio"}`))
	newMeta := json.RawMessage(`{"type":"http","url":"https://example.com"}`)

	updated, err := repo.Update(ctx, rec.ID, uid, KindUpdate{Meta: newMeta})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if string(updated.Meta) != string(newMeta) {
		t.Errorf("returned Meta = %s, want %s", updated.Meta, newMeta)
	}

	fetched, _ := repo.Get(ctx, rec.ID, uid)
	if string(fetched.Meta) != string(newMeta) {
		t.Errorf("persisted Meta = %s, want %s", fetched.Meta, newMeta)
	}
}

func TestKindsRepo_Update_IsActive_False(t *testing.T) {
	db := newTestKindsDB(t)
	uid := createUser(t, db, "jane")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	rec, _ := repo.Create(ctx, uid, "skill", "deactivate-me", "jane/resources/skills/deactivate-me/", nil)
	f := false
	updated, err := repo.Update(ctx, rec.ID, uid, KindUpdate{IsActive: &f})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.IsActive {
		t.Error("expected IsActive=false after update")
	}

	active, _ := repo.ListActive(ctx, uid)
	for _, r := range active {
		if r.ID == rec.ID {
			t.Error("deactivated record should not appear in ListActive")
		}
	}
}

func TestKindsRepo_Update_BothFields(t *testing.T) {
	db := newTestKindsDB(t)
	uid := createUser(t, db, "kim")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	rec, _ := repo.Create(ctx, uid, "skill", "combo", "kim/resources/skills/combo/", nil)
	newMeta := json.RawMessage(`{"key":"val"}`)
	f := false

	updated, err := repo.Update(ctx, rec.ID, uid, KindUpdate{Meta: newMeta, IsActive: &f})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if string(updated.Meta) != string(newMeta) {
		t.Errorf("Meta = %s, want %s", updated.Meta, newMeta)
	}
	if updated.IsActive {
		t.Error("expected IsActive=false")
	}
}

func TestKindsRepo_Update_NotFound(t *testing.T) {
	db := newTestKindsDB(t)
	uid := createUser(t, db, "leo")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	t_ := true
	if _, err := repo.Update(ctx, 9999, uid, KindUpdate{IsActive: &t_}); err == nil {
		t.Error("expected error for non-existent ID, got nil")
	}
}

func TestKindsRepo_Update_WrongUser(t *testing.T) {
	db := newTestKindsDB(t)
	uid1 := createUser(t, db, "mia")
	uid2 := createUser(t, db, "noah")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	rec, _ := repo.Create(ctx, uid1, "skill", "private", "mia/resources/skills/private/", nil)
	t_ := true
	if _, err := repo.Update(ctx, rec.ID, uid2, KindUpdate{IsActive: &t_}); err == nil {
		t.Error("expected error when wrong user updates, got nil")
	}
}

func TestKindsRepo_Delete_Success(t *testing.T) {
	db := newTestKindsDB(t)
	uid := createUser(t, db, "olivia")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	rec, _ := repo.Create(ctx, uid, "skill", "delete-me", "olivia/resources/skills/delete-me/", nil)
	if err := repo.Delete(ctx, rec.ID, uid); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, rec.ID, uid); err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestKindsRepo_Delete_NotFound(t *testing.T) {
	db := newTestKindsDB(t)
	uid := createUser(t, db, "paul")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	if err := repo.Delete(ctx, 9999, uid); err == nil {
		t.Error("expected error for non-existent ID, got nil")
	}
}

func TestKindsRepo_Delete_WrongUser(t *testing.T) {
	db := newTestKindsDB(t)
	uid1 := createUser(t, db, "quinn")
	uid2 := createUser(t, db, "rose")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	rec, _ := repo.Create(ctx, uid1, "skill", "mine", "quinn/resources/skills/mine/", nil)
	if err := repo.Delete(ctx, rec.ID, uid2); err == nil {
		t.Error("expected error when wrong user deletes, got nil")
	}
	// Row must still exist after failed delete.
	if _, err := repo.Get(ctx, rec.ID, uid1); err != nil {
		t.Errorf("row should still exist after wrong-user delete, got: %v", err)
	}
}

func TestKindsRepo_Get_NotFound(t *testing.T) {
	db := newTestKindsDB(t)
	uid := createUser(t, db, "sam")
	repo := NewKindsRepository(db)
	ctx := context.Background()

	if _, err := repo.Get(ctx, 9999, uid); err == nil {
		t.Error("expected error for non-existent ID, got nil")
	}
}
