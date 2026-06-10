// Package store is the storage layer: SQLite (WAL, FTS5) and operations on it.
package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vestigiumincaligne/polka/internal/sqlitedrv"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sqlitedrv.Open(path, sqlitedrv.Options{
		WAL: true, SyncNormal: true, BusyTimeout: 10000, ForeignKeys: true,
	})
	if err != nil {
		return nil, err
	}
	// SQLite is a single-writer database; one connection prevents SQLITE_BUSY
	// between inserts and reads within the process, while WAL allows concurrent reads.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version >= schemaVersion {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if version < 1 {
		if _, err := tx.Exec(schemaV1); err != nil {
			return err
		}
		for _, idx := range schemaIndexes {
			if _, err := tx.Exec(idx); err != nil {
				return err
			}
		}
	}
	if version == 1 {
		if _, err := tx.Exec(schemaV2); err != nil {
			return err
		}
		version = 2
	}
	if version == 2 {
		if _, err := tx.Exec(schemaV3); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) BookCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM books WHERE deleted = 0`).Scan(&n)
	return n, err
}

func (s *Store) GetMeta(ctx context.Context, key, def string) string {
	var v string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
