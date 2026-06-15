// Polka Desktop is the home library desktop application.
// It starts an embedded server on localhost and opens a browser window
// in app mode. Works fully standalone: the library is
// managed and stored locally.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/vestigiumincaligne/polka/internal/auth"
	"github.com/vestigiumincaligne/polka/internal/config"
	"github.com/vestigiumincaligne/polka/internal/library"
	"github.com/vestigiumincaligne/polka/internal/server"
	"github.com/vestigiumincaligne/polka/internal/store"
	"github.com/vestigiumincaligne/polka/internal/syncer"
	"github.com/vestigiumincaligne/polka/web"
)

var version = "dev" // overridden at build time via -ldflags

func main() {
	// The native window (WebView2) must live on the OS main thread.
	runtime.LockOSThread()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "polka-desktop:", err)
		os.Exit(1)
	}
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "polka")
	}
	return ".polka"
}

func defaultLibraryDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Polka", "books")
	}
	return "books"
}

func run() error {
	var noBrowser bool
	dataDir := flag.String("data-dir", defaultDataDir(), "каталог базы и кэшей")
	libraryDir := flag.String("library-dir", defaultLibraryDir(), "каталог с книгами")
	addr := flag.String("addr", "127.0.0.1:0", "адрес встроенного сервера")
	serverURL := flag.String("server", "", "адрес сервера Полки (режим синхронизации)")
	login := flag.String("login", "", "логин на сервере")
	password := flag.String("password", "", "пароль на сервере")
	flag.BoolVar(&noBrowser, "no-browser", false, "не открывать окно (только сервер)")
	flag.Parse()

	for _, dir := range []string{*dataDir, *libraryDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	logFile, err := os.OpenFile(filepath.Join(*dataDir, "desktop.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	log := slog.New(slog.NewTextHandler(logFile, nil))

	cfg := &config.Config{
		Addr:       *addr,
		DataDir:    *dataDir,
		LibraryDir: *libraryDir,
		Auth:       "desktop",
		Version:    version,
	}

	st, err := store.Open(filepath.Join(cfg.DataDir, "polka.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	users, err := auth.Open(filepath.Join(cfg.DataDir, "users.db"))
	if err != nil {
		return fmt.Errorf("open users database: %w", err)
	}
	defer users.Close()

	webFS, err := web.Dist()
	if err != nil {
		return fmt.Errorf("embedded frontend: %w", err)
	}

	// Sync mode: flags update the config, otherwise use the saved one.
	if *serverURL != "" {
		if err := syncer.SaveConfig(*dataDir, &syncer.Config{
			Server: *serverURL, Login: *login, Password: *password,
		}); err != nil {
			return fmt.Errorf("save sync config: %w", err)
		}
	}
	var sync *syncer.Syncer
	if syncCfg, err := syncer.LoadConfig(*dataDir); err == nil {
		sync, err = syncer.New(*dataDir, *syncCfg, users, log)
		if err != nil {
			return fmt.Errorf("sync init: %w", err)
		}
		defer sync.Close()
		syncCtx, cancelSync := context.WithCancel(context.Background())
		defer cancelSync()
		go sync.Run(syncCtx)
		fmt.Println("Режим синхронизации с сервером:", syncCfg.Server)
	}

	srv := server.New(cfg, log, st, library.New(cfg.LibraryDir), users, sync, webFS)

	// Listen on a free port: multiple app instances do not conflict.
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	url := fmt.Sprintf("http://%s/", listener.Addr())
	log.Info("polka-desktop starting", "version", version, "url", url, "library", cfg.LibraryDir)
	fmt.Println("Полка работает:", url)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	shutdown := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}

	// Signals and server errors terminate the application from any state.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-stop:
		case err := <-errCh:
			fmt.Fprintln(os.Stderr, "polka-desktop:", err)
		}
		shutdown()
		os.Exit(0)
	}()

	if noBrowser {
		select {} // server only; live until a signal
	}

	// The native window (Windows/WebView2) runs on the main thread;
	// closing the window terminates the application.
	if runNativeWindow(url, log) {
		log.Info("window closed, shutting down")
		return shutdown()
	}

	// Fallback: a chromium-app window or the system browser.
	openWindow(url, *dataDir, log)
	log.Info("window closed, shutting down")
	return shutdown()
}

// openWindow opens the UI as an application window (a Chromium-family browser
// in --app mode: a separate window without an address bar), or as a regular
// browser tab otherwise. Blocks until the window closes if it is ours.
func openWindow(url, dataDir string, log *slog.Logger) {
	profile := filepath.Join(dataDir, "window-profile")
	if browser := findAppBrowser(); browser != "" {
		cmd := exec.Command(browser,
			"--app="+url,
			"--user-data-dir="+profile,
			"--window-size=1280,860",
			"--no-first-run",
			"--no-default-browser-check",
		)
		if err := cmd.Start(); err == nil {
			log.Info("app window", "browser", browser)
			cmd.Wait() // window closed — exit
			return
		}
	}

	// Fallback: the system browser; the app's lifetime is governed by Ctrl+C/signal.
	log.Info("no chromium-family browser, opening default browser")
	openDefault(url)
	select {} // keep the goroutine: a tab being closed cannot be detected
}

// findAppBrowser looks for a browser for the app window: on Windows and macOS
// browsers are not in PATH — check the standard install locations.
func findAppBrowser() string {
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		for _, c := range []struct{ env, suffix string }{
			{"ProgramFiles(x86)", `Microsoft\Edge\Application\msedge.exe`},
			{"ProgramFiles", `Microsoft\Edge\Application\msedge.exe`},
			{"ProgramFiles", `Google\Chrome\Application\chrome.exe`},
			{"ProgramFiles(x86)", `Google\Chrome\Application\chrome.exe`},
			{"LocalAppData", `Google\Chrome\Application\chrome.exe`},
			{"ProgramFiles", `BraveSoftware\Brave-Browser\Application\brave.exe`},
		} {
			if base := os.Getenv(c.env); base != "" {
				candidates = append(candidates, filepath.Join(base, c.suffix))
			}
		}
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	// PATH is the primary way on Linux and the fallback for the rest.
	for _, name := range pathBrowserNames() {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func pathBrowserNames() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"msedge.exe", "chrome.exe", "brave.exe"}
	case "darwin":
		return nil
	default:
		return []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "brave-browser", "microsoft-edge"}
	}
}

func openDefault(url string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
	}
}
