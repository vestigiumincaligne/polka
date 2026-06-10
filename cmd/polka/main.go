// Polka is a standalone home e-book library server:
// web interface, OPDS catalog and collection management.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/vestigiumincaligne/polka/internal/auth"
	"github.com/vestigiumincaligne/polka/internal/config"
	"github.com/vestigiumincaligne/polka/internal/importer"
	"github.com/vestigiumincaligne/polka/internal/library"
	"github.com/vestigiumincaligne/polka/internal/server"
	"github.com/vestigiumincaligne/polka/internal/store"
	"github.com/vestigiumincaligne/polka/web"
)

var version = "dev" // overridden at build time via -ldflags

const usage = `polka — сервер домашней библиотеки

Использование:
  polka [serve] [флаги]          запустить сервер (по умолчанию)
  polka import <файл.inpx>       импортировать inpx-каталог
      --replace                  заменить существующую коллекцию
  polka passwd <логин> <пароль>  сменить пароль пользователя (восстановление доступа)
  polka version                  показать версию

Флаги: --addr, --data-dir, --library-dir (или POLKA_ADDR, POLKA_DATA_DIR, POLKA_LIBRARY_DIR)
`

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	var err error
	switch cmd {
	case "serve":
		err = runServe(log, args)
	case "import":
		err = runImport(log, args)
	case "passwd":
		err = runPasswd(args)
	case "version":
		fmt.Println(version)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Error(cmd, "error", err)
		os.Exit(1)
	}
}

func runServe(log *slog.Logger, args []string) error {
	cfg, _, err := config.Load(args, nil)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	webFS, err := web.Dist()
	if err != nil {
		return fmt.Errorf("embedded frontend: %w", err)
	}

	lib := library.New(cfg.LibraryDir)
	if lib == nil {
		log.Warn("library-dir is not set: covers and downloads are disabled")
	}

	users, err := auth.Open(filepath.Join(cfg.DataDir, "users.db"))
	if err != nil {
		return fmt.Errorf("open users database: %w", err)
	}
	defer users.Close()

	// First run: create an administrator with a random password.
	ctx := context.Background()
	if n, err := users.UsersCount(ctx); err != nil {
		return err
	} else if n == 0 {
		password := auth.RandomPassword()
		if _, err := users.CreateUser(ctx, "admin", password, "Администратор", auth.RoleAdmin); err != nil {
			return err
		}
		log.Warn("created initial administrator — save this password, it is shown only once",
			"login", "admin", "password", password)
	}

	srv := server.New(cfg, log, st, lib, users, nil, webFS)

	errCh := make(chan error, 1)
	go func() {
		log.Info("polka starting", "version", version, "addr", cfg.Addr, "db", cfg.DBPath())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-stop:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("polka stopped")
	return nil
}

// runPasswd is an emergency password change from the server console (when the
// administrator password is lost). Creates the user if missing.
func runPasswd(args []string) error {
	cfg, rest, err := config.Load(args, nil)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return errors.New("использование: polka passwd <логин> <новый-пароль>")
	}
	login, password := rest[0], rest[1]

	users, err := auth.Open(filepath.Join(cfg.DataDir, "users.db"))
	if err != nil {
		return err
	}
	defer users.Close()

	ctx := context.Background()
	list, err := users.ListUsers(ctx)
	if err != nil {
		return err
	}
	for _, u := range list {
		if strings.EqualFold(u.Login, login) {
			if err := users.UpdateUser(ctx, u.ID, &password, nil, nil, nil); err != nil {
				return err
			}
			fmt.Printf("пароль пользователя %s обновлён\n", u.Login)
			return nil
		}
	}
	if _, err := users.CreateUser(ctx, login, password, "", auth.RoleAdmin); err != nil {
		return err
	}
	fmt.Printf("создан администратор %s\n", login)
	return nil
}

func runImport(log *slog.Logger, args []string) error {
	var replace bool
	cfg, rest, err := config.Load(args, func(fs *flag.FlagSet) {
		fs.BoolVar(&replace, "replace", false, "replace existing collection")
	})
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("укажите путь к inpx-файлу: polka import [флаги] <файл.inpx>")
	}

	if replace {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			os.Remove(cfg.DBPath() + suffix)
		}
	}

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	stats, err := importer.ImportInpx(context.Background(), log, st, rest[0], nil)
	if err != nil {
		return err
	}
	log.Info("import done",
		"books", stats.Books, "authors", stats.Authors, "series", stats.Series,
		"genres", stats.Genres, "duration", stats.Duration.Round(time.Millisecond))
	return nil
}
