// Package sqlitedrv selects the SQLite driver per platform:
// pure Go (modernc) everywhere, the CGO mattn driver on Android:
// modernc/libc uses raw system calls (lstat and others)
// that Android forbids via seccomp.
package sqlitedrv

import "database/sql"

// Options are common connection settings independent of the driver.
type Options struct {
	WAL         bool
	SyncNormal  bool
	BusyTimeout int // milliseconds
	ForeignKeys bool
}

// Open opens the database with the settings, translating them into the driver's syntax.
func Open(path string, o Options) (*sql.DB, error) {
	return sql.Open(driverName, dsn(path, o))
}
