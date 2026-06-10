//go:build !android

package sqlitedrv

import (
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

const driverName = "sqlite"

func dsn(path string, o Options) string {
	var p []string
	if o.WAL {
		p = append(p, "_pragma=journal_mode(WAL)")
	}
	if o.SyncNormal {
		p = append(p, "_pragma=synchronous(NORMAL)")
	}
	if o.BusyTimeout > 0 {
		p = append(p, fmt.Sprintf("_pragma=busy_timeout(%d)", o.BusyTimeout))
	}
	if o.ForeignKeys {
		p = append(p, "_pragma=foreign_keys(ON)")
	}
	return "file:" + path + "?" + strings.Join(p, "&")
}
