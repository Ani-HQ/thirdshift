package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type AppliedMigration struct {
	Version string
	Name    string
}

type migrationFile struct {
	Version  string
	Name     string
	Path     string
	Checksum string
	SQL      string
}

func ApplyURL(ctx context.Context, databaseURL, migrationsDir string) ([]AppliedMigration, error) {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	defer conn.Close(context.Background())

	return Apply(ctx, conn, migrationsDir)
}

func Apply(ctx context.Context, conn *pgx.Conn, migrationsDir string) ([]AppliedMigration, error) {
	files, err := loadMigrationFiles(migrationsDir)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version text PRIMARY KEY,
  name text NOT NULL,
  checksum text NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT now()
)`); err != nil {
		return nil, fmt.Errorf("ensure schema_migrations: %w", err)
	}

	applied := make([]AppliedMigration, 0)
	for _, file := range files {
		changed, err := applyOne(ctx, conn, file)
		if err != nil {
			return nil, err
		}
		if changed {
			applied = append(applied, AppliedMigration{Version: file.Version, Name: file.Name})
		}
	}
	return applied, nil
}

func loadMigrationFiles(dir string) ([]migrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory %q: %w", dir, err)
	}

	var files []migrationFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("migration %q must start with a numeric version and underscore", entry.Name())
		}
		path := filepath.Join(dir, entry.Name())
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", path, err)
		}
		sum := sha256.Sum256(contents)
		files = append(files, migrationFile{
			Version:  parts[0],
			Name:     strings.TrimSuffix(parts[1], ".up.sql"),
			Path:     path,
			Checksum: hex.EncodeToString(sum[:]),
			SQL:      string(contents),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Version < files[j].Version
	})
	return files, nil
}

func applyOne(ctx context.Context, conn *pgx.Conn, file migrationFile) (bool, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin migration %s: %w", file.Version, err)
	}
	defer tx.Rollback(context.Background())

	var existingChecksum string
	err = tx.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version = $1", file.Version).Scan(&existingChecksum)
	switch {
	case err == nil:
		if existingChecksum != file.Checksum {
			return false, fmt.Errorf("migration %s checksum mismatch; refusing to run changed migration", file.Version)
		}
		return false, tx.Commit(ctx)
	case err != pgx.ErrNoRows:
		return false, fmt.Errorf("check migration %s: %w", file.Version, err)
	}

	if _, err := tx.Exec(ctx, file.SQL); err != nil {
		return false, fmt.Errorf("apply migration %s (%s): %w", file.Version, file.Path, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES ($1, $2, $3, $4)", file.Version, file.Name, file.Checksum, time.Now().UTC()); err != nil {
		return false, fmt.Errorf("record migration %s: %w", file.Version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit migration %s: %w", file.Version, err)
	}
	return true, nil
}
