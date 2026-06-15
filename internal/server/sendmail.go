package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/vestigiumincaligne/polka/internal/mailer"
)

// Sending a book to an e-reader by e-mail (send-to-Kindle and friends):
// the admin configures SMTP, each user has their own reader address.

const maxSendBytes = 25 << 20 // attachment cap

// smtpConfig reads the SMTP settings (the password is decrypted).
func (s *Server) smtpConfig(r *http.Request) mailer.Config {
	ctx := r.Context()
	get := func(k string) string { return s.users.GetSetting(ctx, "smtp."+k, "") }
	port, _ := strconv.Atoi(get("port"))
	sec := get("security")
	if sec == "" {
		sec = "starttls"
	}
	return mailer.Config{
		Host:     get("host"),
		Port:     port,
		User:     get("user"),
		Password: s.decryptSecret(get("password")),
		From:     get("from"),
		Security: sec,
	}
}

func (s *Server) smtpEnabled(r *http.Request) bool {
	return s.users.GetSetting(r.Context(), "smtp.enabled", "0") == "1" && s.smtpConfig(r).Valid()
}

// readerEmail — the user's personal e-reader address.
func (s *Server) readerEmail(r *http.Request, userID int64) string {
	return s.users.GetSetting(r.Context(), fmt.Sprintf("reader_email:%d", userID), "")
}

// GET/POST /api/v1/me/reader-email — the current user's reader address.
func (s *Server) handleReaderEmail(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		email := strings.TrimSpace(req.Email)
		if email != "" && !strings.Contains(email, "@") {
			http.Error(w, "invalid email", http.StatusBadRequest)
			return
		}
		if err := s.users.SetSetting(r.Context(), fmt.Sprintf("reader_email:%d", u.ID), email); err != nil {
			s.apiError(w, err)
			return
		}
	}
	writeJSON(w, map[string]any{
		"email":     s.readerEmail(r, u.ID),
		"smtpReady": s.smtpEnabled(r),
	})
}

// POST /api/v1/books/{id}/send — e-mail the book to the user's reader.
func (s *Server) handleSendBook(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if !s.smtpEnabled(r) {
		http.Error(w, "email delivery is not configured", http.StatusServiceUnavailable)
		return
	}
	to := s.readerEmail(r, u.ID)
	if to == "" {
		http.Error(w, "set your reader e-mail first", http.StatusBadRequest)
		return
	}
	bookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := s.st.BookFile(r.Context(), bookID)
	if err != nil {
		s.apiError(w, err)
		return
	}
	rc, size, err := s.lib.Open(f.Folder, f.File, f.Ext)
	if err != nil {
		s.apiError(w, err)
		return
	}
	defer rc.Close()
	if size > maxSendBytes {
		http.Error(w, "book is too large to e-mail", http.StatusRequestEntityTooLarge)
		return
	}
	data, err := io.ReadAll(io.LimitReader(rc, maxSendBytes))
	if err != nil {
		s.apiError(w, err)
		return
	}

	d, _ := s.st.BookDetails(r.Context(), bookID)
	title := f.File
	if d != nil && d.Title != "" {
		title = d.Title
	}
	name := sanitizeFileName(title) + "." + f.Ext
	cfg := s.smtpConfig(r)
	if err := cfg.Send(to, title, "Polka — "+title, name, data); err != nil {
		s.log.Warn("send book", "book", bookID, "error", err)
		http.Error(w, "could not send the book", http.StatusBadGateway)
		return
	}
	s.log.Info("book sent", "book", bookID, "to", to)
	writeJSON(w, map[string]any{"sent": true})
}

// POST /admin/smtp/test — verify the SMTP connection (admin only).
func (s *Server) handleSmtpTest(w http.ResponseWriter, r *http.Request) {
	if err := s.smtpConfig(r).Verify(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

type smtpInput struct {
	Enabled  *bool   `json:"enabled"`
	Host     *string `json:"host"`
	Port     *int    `json:"port"`
	User     *string `json:"user"`
	Password *string `json:"password"` // empty string = leave unchanged
	From     *string `json:"from"`
	Security *string `json:"security"`
}

// smtpSettings — config for the admin panel (no password, just a "set" flag).
func (s *Server) smtpSettings(r *http.Request) map[string]any {
	ctx := r.Context()
	get := func(k string) string { return s.users.GetSetting(ctx, "smtp."+k, "") }
	return map[string]any{
		"enabled":     get("enabled") == "1",
		"host":        get("host"),
		"port":        get("port"),
		"user":        get("user"),
		"from":        get("from"),
		"security":    get("security"),
		"hasPassword": get("password") != "",
	}
}

func (s *Server) saveSmtpSettings(r *http.Request, in smtpInput) error {
	ctx := r.Context()
	set := func(k, v string) error { return s.users.SetSetting(ctx, "smtp."+k, v) }
	if in.Enabled != nil {
		v := "0"
		if *in.Enabled {
			v = "1"
		}
		if err := set("enabled", v); err != nil {
			return err
		}
	}
	if in.Host != nil {
		if err := set("host", strings.TrimSpace(*in.Host)); err != nil {
			return err
		}
	}
	if in.Port != nil {
		if err := set("port", strconv.Itoa(*in.Port)); err != nil {
			return err
		}
	}
	if in.User != nil {
		if err := set("user", strings.TrimSpace(*in.User)); err != nil {
			return err
		}
	}
	if in.From != nil {
		if err := set("from", strings.TrimSpace(*in.From)); err != nil {
			return err
		}
	}
	if in.Security != nil {
		if err := set("security", *in.Security); err != nil {
			return err
		}
	}
	if in.Password != nil && *in.Password != "" {
		if err := set("password", s.encryptSecret(*in.Password)); err != nil {
			return err
		}
	}
	return nil
}
