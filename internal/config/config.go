// Package config holds the server configuration, collected from
// command-line flags with environment-variable fallbacks (POLKA_*).
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	// Addr is the HTTP listen address, e.g. ":12791" or "127.0.0.1:8080".
	Addr string
	// DataDir holds the SQLite database and caches.
	DataDir string
	// LibraryDir is the root directory with book files and archives.
	LibraryDir string
	// Auth: "required" — catalog only after login, "public" — open.
	Auth string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load parses configuration from args (without the program name).
// extra, if provided, registers additional subcommand flags.
// Returns the config and the positional arguments.
func Load(args []string, extra func(*flag.FlagSet)) (*Config, []string, error) {
	defaultData := ".polka"
	if home, err := os.UserHomeDir(); err == nil {
		defaultData = filepath.Join(home, ".polka")
	}

	cfg := &Config{}
	fs := flag.NewFlagSet("polka", flag.ContinueOnError)
	fs.StringVar(&cfg.Addr, "addr", env("POLKA_ADDR", ":12791"), "HTTP listen address")
	fs.StringVar(&cfg.DataDir, "data-dir", env("POLKA_DATA_DIR", defaultData), "directory for database and caches")
	fs.StringVar(&cfg.LibraryDir, "library-dir", env("POLKA_LIBRARY_DIR", ""), "root directory of the book library")
	fs.StringVar(&cfg.Auth, "auth", env("POLKA_AUTH", "required"), `access mode: "required" or "public"`)
	if extra != nil {
		extra(fs)
	}
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create data dir: %w", err)
	}
	return cfg, fs.Args(), nil
}

// DBPath returns the path to the collection database file.
func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "polka.db")
}
