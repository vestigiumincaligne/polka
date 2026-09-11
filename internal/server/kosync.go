package server

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"

	"github.com/vestigiumincaligne/polka/internal/auth"
)

// KOReader progress sync (the kosync protocol, as implemented by
// koreader-sync-server and the kosync.koplugin client). A KOReader device
// logs in with the Polka login and a device password set in the web UI,
// then pushes/pulls reading positions keyed by a partial-MD5 digest of the
// book file. Digests of files served by Polka are recorded on download, so
// device positions also show up on the "Reading now" shelf.
//
// Paths are dictated by the protocol and live at the root:
//   POST /users/create              — registration is handled in the web UI, always 402
//   GET  /users/auth                — check credentials
//   PUT  /users/password            — change the device key from the device
//   PUT  /syncs/progress            — push a position
//   GET  /syncs/progress/{document} — pull a position
//   GET  /healthcheck

func (s *Server) registerKosyncRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /users/create", s.handleKosyncCreate)
	mux.HandleFunc("GET /users/auth", s.handleKosyncAuth)
	mux.HandleFunc("PUT /users/password", s.handleKosyncPassword)
	mux.HandleFunc("PUT /syncs/progress", s.handleKosyncUpdate)
	mux.HandleFunc("GET /syncs/progress/{document}", s.handleKosyncGet)
	mux.HandleFunc("GET /healthcheck", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"state": "OK"})
	})

	// The device key management for the web UI.
	mux.HandleFunc("GET /api/v1/me/kosync", s.protected(s.handleKosyncSettings))
	mux.HandleFunc("POST /api/v1/me/kosync", s.protected(s.handleKosyncSettings))
}

// kosyncError mirrors the reference server's error bodies.
func kosyncError(w http.ResponseWriter, status, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message})
}

func kosyncUnauthorized(w http.ResponseWriter) {
	kosyncError(w, http.StatusUnauthorized, 2001, "Unauthorized")
}

// kosyncUser authenticates the x-auth-user / x-auth-key headers against
// the user's device key. Only failed attempts count against the login
// limiter — successful syncs may be frequent.
func (s *Server) kosyncUser(r *http.Request) *auth.User {
	login := r.Header.Get("x-auth-user")
	key := r.Header.Get("x-auth-key")
	if login == "" || key == "" || s.loginLimiter.blocked(clientIP(r)) {
		return nil
	}
	u, err := s.users.GetByLogin(r.Context(), login)
	if err == nil {
		stored := s.users.KosyncKey(r.Context(), u.ID)
		if stored != "" && subtle.ConstantTimeCompare([]byte(stored), []byte(key)) == 1 {
			return u
		}
	}
	s.loginLimiter.allow(clientIP(r)) // record the failure
	return nil
}

// POST /users/create — device-side registration would let anyone claim a
// key for a known login, so the key is set in the web UI instead.
func (s *Server) handleKosyncCreate(w http.ResponseWriter, _ *http.Request) {
	kosyncError(w, http.StatusPaymentRequired, 2005,
		"Registration is disabled: log in with your Polka username and the device password set in the Polka web interface")
}

func (s *Server) handleKosyncAuth(w http.ResponseWriter, r *http.Request) {
	if s.kosyncUser(r) == nil {
		kosyncUnauthorized(w)
		return
	}
	writeJSON(w, map[string]any{"authorized": "OK"})
}

// PUT /users/password {password} — change the device key (KOReader sends
// an MD5 of the new password, same form as the auth key).
func (s *Server) handleKosyncPassword(w http.ResponseWriter, r *http.Request) {
	u := s.kosyncUser(r)
	if u == nil {
		kosyncUnauthorized(w)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || !kosyncKeyRe.MatchString(req.Password) {
		kosyncError(w, http.StatusForbidden, 2003, "Invalid request")
		return
	}
	if err := s.users.SetKosyncKey(r.Context(), u.ID, req.Password); err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, map[string]any{"updated": true})
}

var (
	kosyncDocRe = regexp.MustCompile(`^[0-9a-fA-F]{32,64}$`)
	kosyncKeyRe = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
)

func (s *Server) handleKosyncUpdate(w http.ResponseWriter, r *http.Request) {
	u := s.kosyncUser(r)
	if u == nil {
		kosyncUnauthorized(w)
		return
	}
	var req struct {
		Document   string          `json:"document"`
		Progress   json.RawMessage `json:"progress"` // a string xpointer or a bare page number
		Percentage *float64        `json:"percentage"`
		Device     string          `json:"device"`
		DeviceID   string          `json:"device_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		kosyncError(w, http.StatusForbidden, 2003, "Invalid request")
		return
	}
	if !kosyncDocRe.MatchString(req.Document) {
		kosyncError(w, http.StatusForbidden, 2004, "Field 'document' not provided.")
		return
	}
	progress := rawToString(req.Progress)
	if progress == "" || req.Percentage == nil || req.Device == "" {
		kosyncError(w, http.StatusForbidden, 2003, "Invalid request")
		return
	}
	ts, err := s.users.KosyncUpdate(r.Context(), u.ID, &auth.KosyncProgress{
		Document: req.Document, Progress: progress, Percentage: *req.Percentage,
		Device: req.Device, DeviceID: req.DeviceID,
	})
	if err != nil {
		s.apiError(w, err)
		return
	}
	// The bridge to Polka's own shelves: when the digest is known (the file
	// was downloaded through Polka), the overall progress follows the device.
	if bookID, err := s.st.BookIDByDigest(r.Context(), req.Document); err == nil {
		if p := *req.Percentage; p >= 0 && p <= 1 {
			if err := s.users.SaveOverall(r.Context(), u.ID, bookID, p); err != nil {
				s.log.Warn("kosync bridge", "error", err)
			}
		}
	}
	writeJSON(w, map[string]any{"document": req.Document, "timestamp": ts})
}

// rawToString accepts both "xpointer" strings and bare numbers (paged
// documents report the page as a number).
func rawToString(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var num float64
	if json.Unmarshal(raw, &num) == nil {
		return string(raw)
	}
	return ""
}

func (s *Server) handleKosyncGet(w http.ResponseWriter, r *http.Request) {
	u := s.kosyncUser(r)
	if u == nil {
		kosyncUnauthorized(w)
		return
	}
	doc := r.PathValue("document")
	if !kosyncDocRe.MatchString(doc) {
		kosyncError(w, http.StatusForbidden, 2004, "Field 'document' not provided.")
		return
	}
	p, err := s.users.KosyncGet(r.Context(), u.ID, doc)
	if errors.Is(err, auth.ErrNotFound) {
		writeJSON(w, map[string]any{}) // the reference server answers 200 with no fields
		return
	}
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"document": p.Document, "progress": p.Progress, "percentage": p.Percentage,
		"device": p.Device, "device_id": p.DeviceID, "timestamp": p.Timestamp,
	})
}

// GET/POST /api/v1/me/kosync — device password management from the web UI.
// POST {password}: a plain password to enable (stored as its MD5, the form
// KOReader sends); an empty one disables sync.
func (s *Server) handleKosyncSettings(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		key := ""
		if req.Password != "" {
			if len(req.Password) < 8 {
				http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
				return
			}
			sum := md5.Sum([]byte(req.Password))
			key = hex.EncodeToString(sum[:])
		}
		if err := s.users.SetKosyncKey(r.Context(), u.ID, key); err != nil {
			s.apiError(w, err)
			return
		}
	}
	writeJSON(w, map[string]any{
		"enabled": s.users.KosyncKey(r.Context(), u.ID) != "",
		"login":   u.Login,
	})
}

// --- Digest of served files ---

// KOReader identifies a book by a partial MD5 of the file: MD5 over
// 1024-byte samples at offsets 0, 1024, 4096, 16384, … (1024·4ⁱ, twelve
// samples up to 1 GB) — see util.partialMD5 in KOReader. digestTee captures
// exactly those ranges while the file is streamed to the client.
type digestTee struct {
	samples [12][]byte
	pos     int64
}

// kosyncSampleOffset mirrors KOReader's loop (i = -1..10, offset =
// lshift(1024, 2i) with LuaJIT 32-bit shift semantics): 0, 1024, 4096,
// 16384, …, 1 GB.
func kosyncSampleOffset(i int) int64 {
	if i == 0 {
		return 0
	}
	return 1024 << (2 * (i - 1))
}

func (d *digestTee) Write(p []byte) (int, error) {
	for i := 0; i < len(d.samples); i++ {
		off := kosyncSampleOffset(i)
		end := off + 1024
		if d.pos+int64(len(p)) <= off || d.pos >= end || len(d.samples[i]) >= 1024 {
			continue
		}
		from := off - d.pos
		if from < 0 {
			from = 0
		}
		to := end - d.pos
		if to > int64(len(p)) {
			to = int64(len(p))
		}
		need := 1024 - len(d.samples[i])
		chunk := p[from:to]
		if len(chunk) > need {
			chunk = chunk[:need]
		}
		d.samples[i] = append(d.samples[i], chunk...)
	}
	d.pos += int64(len(p))
	return len(p), nil
}

// digest returns the partial-MD5 hex, replicating KOReader's read loop:
// samples are hashed in order until the first offset past the end of file.
func (d *digestTee) digest() string {
	h := md5.New()
	for i := 0; i < len(d.samples); i++ {
		if kosyncSampleOffset(i) >= d.pos {
			break
		}
		h.Write(d.samples[i])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// recordDigest computes the digest of a fully served file and remembers it.
func (s *Server) recordDigest(tee *digestTee, bookID, want, copied int64) {
	if copied != want || copied == 0 {
		return // the client aborted mid-download: the digest would be wrong
	}
	if err := s.st.SetBookDigest(context.Background(), tee.digest(), bookID); err != nil {
		s.log.Warn("book digest", "book", bookID, "error", err)
	}
}

var _ io.Writer = (*digestTee)(nil)
