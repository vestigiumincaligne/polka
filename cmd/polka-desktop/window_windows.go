//go:build windows

package main

import (
	"log/slog"

	webview2 "github.com/jchv/go-webview2"
)

// runNativeWindow opens a native WebView2 window (the system engine
// on Windows 10/11). Blocks until the window is closed. Returns false
// if the WebView2 Runtime is unavailable — the fallback path takes over then.
func runNativeWindow(url string, log *slog.Logger) bool {
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "Полка",
			Width:  1280,
			Height: 860,
			IconId: 1, // icon from the exe resources (rsrc)
			Center: true,
		},
	})
	if w == nil {
		log.Info("WebView2 runtime is not available, falling back")
		return false
	}
	defer w.Destroy()
	log.Info("native window", "engine", "WebView2")
	w.Navigate(url)
	w.Run()
	return true
}
