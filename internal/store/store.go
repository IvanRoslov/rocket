// Package store provides the SQLite-backed persistence layer for rocket.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Sentinel errors returned by store DAOs.
var (
	ErrNotFound         = errors.New("not found")
	ErrExists           = errors.New("already exists")
	ErrRepoInUse        = errors.New("repo is in use by a project")
	ErrQuestionResolved = errors.New("question already resolved")
	ErrQuestionOpen     = errors.New("question is not resolved")
)

// escapeDSNPath percent-encodes special characters in a file path for use in a
// SQLite URI. modernc.org/sqlite splits the DSN at the FIRST '?' to separate
// the path from query parameters, and SQLite URI filenames percent-decode %XX
// sequences — so an unescaped path containing '?', '#', or '%' would corrupt
// the pragma query string. We encode % first, then ? and #, to avoid double-encoding.
func escapeDSNPath(path string) string {
	path = strings.ReplaceAll(path, "%", "%25")
	path = strings.ReplaceAll(path, "?", "%3F")
	path = strings.ReplaceAll(path, "#", "%23")
	return path
}

// Store wraps a SQLite database handle with rocket's DAOs.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path, configures
// WAL mode, busy_timeout and foreign keys, then applies any pending
// migrations. Open is idempotent: calling it repeatedly against the same
// path is safe and re-applies no migration twice.
func Open(path string) (*Store, error) {
	dsn := "file:" + escapeDSNPath(path) + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate applies pending migrations on a single dedicated connection with
// foreign key enforcement disabled. Both details matter: PRAGMA foreign_keys
// is per-connection (so it must not be set through the pool) and is a no-op
// inside a transaction, and table rebuilds — the standard SQLite recipe for
// changing a CHECK constraint, see 0005_agents.sql — drop a table other tables
// reference, which would fail with enforcement on. The pragma is restored
// before the connection returns to the pool.
func (s *Store) migrate() error {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign keys: %w", err)
	}
	defer conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`)

	return s.migrateOn(ctx, conn)
}

// migrationNames returns the embedded migration file names in the order they
// are applied; a migration's version is its 1-based position in this list.
func migrationNames() ([]string, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func (s *Store) migrateOn(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := migrationNames()
	if err != nil {
		return err
	}

	for i, name := range names {
		version := i + 1

		var exists int
		err := conn.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE version = ?`, version).Scan(&exists)
		if err == nil {
			// already applied
			continue
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("check migration %d applied: %w", version, err)
		}

		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for migration %s: %w", name, err)
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}

	return nil
}
