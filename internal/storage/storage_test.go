package storage

import (
	"path/filepath"
	"testing"

	"github.com/NLstn/chunkd/internal/domain"
)

func TestMigrationsPersistAndAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chunkd.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Server{Name: "Test", State: "Stopped"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var versions int64
	if err := db.Table("schema_migrations").Count(&versions).Error; err != nil || versions != 2 {
		t.Fatalf("versions=%d err=%v", versions, err)
	}
	conn, _ := db.DB()
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	conn, _ = db.DB()
	defer conn.Close()
	var servers []domain.Server
	if err := db.Find(&servers).Error; err != nil || len(servers) != 1 || servers[0].Name != "Test" {
		t.Fatalf("servers=%v err=%v", servers, err)
	}
	if err := db.Create(&domain.Admin{ID: 2, Username: "other", PasswordHash: "not-a-real-hash"}).Error; err == nil {
		t.Fatal("database allowed a second admin")
	}
}
