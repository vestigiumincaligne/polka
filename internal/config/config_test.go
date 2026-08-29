package config

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	// Defaults (with the data dir redirected so the test never touches $HOME).
	dir := t.TempDir()
	t.Setenv("POLKA_ADDR", "")
	t.Setenv("POLKA_LIBRARY_DIR", "")
	t.Setenv("POLKA_AUTH", "")
	t.Setenv("POLKA_DATA_DIR", filepath.Join(dir, "envdata"))
	cfg, rest, err := Load(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":12791" || cfg.Auth != "required" || cfg.LibraryDir != "" || len(rest) != 0 {
		t.Errorf("defaults: %+v rest=%v", cfg, rest)
	}
	if cfg.DataDir != filepath.Join(dir, "envdata") {
		t.Errorf("env data dir: %q", cfg.DataDir)
	}
	if st, err := os.Stat(cfg.DataDir); err != nil || !st.IsDir() {
		t.Error("data dir must be created")
	}
	if cfg.DBPath() != filepath.Join(cfg.DataDir, "polka.db") {
		t.Errorf("DBPath: %q", cfg.DBPath())
	}

	// Environment supplies values…
	t.Setenv("POLKA_ADDR", "127.0.0.1:1")
	t.Setenv("POLKA_LIBRARY_DIR", "/books")
	t.Setenv("POLKA_AUTH", "public")
	cfg, _, _ = Load(nil, nil)
	if cfg.Addr != "127.0.0.1:1" || cfg.LibraryDir != "/books" || cfg.Auth != "public" {
		t.Errorf("env: %+v", cfg)
	}
	// …and flags beat the environment; positional args come back.
	flagData := filepath.Join(dir, "flagdata")
	cfg, rest, err = Load([]string{"--addr", ":9", "--data-dir", flagData, "--library-dir=/lib", "--auth", "demo", "catalog.inpx"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9" || cfg.DataDir != flagData || cfg.LibraryDir != "/lib" || cfg.Auth != "demo" {
		t.Errorf("flags: %+v", cfg)
	}
	if len(rest) != 1 || rest[0] != "catalog.inpx" {
		t.Errorf("positional: %v", rest)
	}
}

func TestLoadExtraFlagsAndErrors(t *testing.T) {
	t.Setenv("POLKA_DATA_DIR", t.TempDir())
	var replace bool
	var inpx string
	_, rest, err := Load([]string{"--replace", "--inpx", "x.inpx", "pos"}, func(fs *flag.FlagSet) {
		fs.BoolVar(&replace, "replace", false, "")
		fs.StringVar(&inpx, "inpx", "", "")
	})
	if err != nil || !replace || inpx != "x.inpx" || len(rest) != 1 {
		t.Errorf("extra flags: %v %v %q %v", err, replace, inpx, rest)
	}
	if _, _, err := Load([]string{"--no-such-flag"}, nil); err == nil {
		t.Error("unknown flag must be an error")
	}
	// A data dir that cannot be created is reported with a hint.
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, []byte("x"), 0o644)
	_, _, err = Load([]string{"--data-dir", filepath.Join(blocker, "sub")}, nil)
	if err == nil || !strings.Contains(err.Error(), "POLKA_DATA_DIR") {
		t.Errorf("unwritable data dir: %v", err)
	}
}
