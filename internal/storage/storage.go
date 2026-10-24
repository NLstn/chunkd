package storage

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open uses a single SQLite connection, with WAL and bounded lock waits.
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL"} {
		if err := db.Exec(pragma).Error; err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
	}
	if err := Migrate(db); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// Migrate applies immutable, ordered SQL files transactionally. Do not edit applied
// migrations: add a new numbered file instead. Downgrades require a backup restore.
func Migrate(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)").Error; err != nil {
			return err
		}
		files, err := fs.Glob(migrations, "migrations/*.sql")
		if err != nil {
			return err
		}
		sort.Strings(files)
		for _, file := range files {
			var count int64
			if err := tx.Table("schema_migrations").Where("version = ?", file).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				continue
			}
			sql, err := migrations.ReadFile(file)
			if err != nil {
				return err
			}
			for _, statement := range strings.Split(string(sql), ";") {
				if strings.TrimSpace(statement) == "" {
					continue
				}
				if err := tx.Exec(statement).Error; err != nil {
					return fmt.Errorf("migration %s: %w", file, err)
				}
			}
			if err := tx.Exec("INSERT INTO schema_migrations(version) VALUES (?)", file).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
