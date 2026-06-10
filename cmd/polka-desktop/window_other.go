//go:build !windows

package main

import "log/slog"

// A native window exists only on Windows for now (WebView2);
// other platforms use chromium-app/browser.
func runNativeWindow(string, *slog.Logger) bool { return false }
