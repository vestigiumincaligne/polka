package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/vestigiumincaligne/polka/internal/auth"
)

const sessionCookie = "polka_session"

func (s *Server) authRequired() bool { return s.cfg.Auth == "required" || s.cfg.Auth == "" }

// currentUser returns the user from the session cookie or from
// HTTP Basic (OPDS readers and other non-browser clients).
// In desktop mode all local requests act on behalf of the owner.
func (s *Server) currentUser(r *http.Request) *auth.User {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if u, err := s.users.GetByToken(r.Context(), c.Value); err == nil {
			return u
		}
	}
	if login, password, ok := r.BasicAuth(); ok {
		if u := s.basicUser(r, login, password); u != nil {
			return u
		}
	}
	if s.desktop != nil {
		return s.desktop
	}
	return nil
}

// basicCache avoids recomputing argon2 on every reader request:
// OPDS clients send the Basic header with every cover and file.
type basicCache struct {
	mu      sync.Mutex
	entries map[string]basicEntry
}

type basicEntry struct {
	user    *auth.User
	expires time.Time
}

const basicCacheTTL = 10 * time.Minute

func (s *Server) basicUser(r *http.Request, login, password string) *auth.User {
	sum := sha256.Sum256([]byte(login + "\x00" + password))
	key := hex.EncodeToString(sum[:])

	s.basic.mu.Lock()
	if e, ok := s.basic.entries[key]; ok && time.Now().Before(e.expires) {
		s.basic.mu.Unlock()
		// Re-read the disabled flag so a revoked account loses access
		// immediately instead of lingering until the cache expires.
		if fresh, err := s.users.GetByID(r.Context(), e.user.ID); err == nil && !fresh.Disabled {
			return fresh
		}
		return nil
	}
	s.basic.mu.Unlock()

	u, err := s.users.Verify(r.Context(), login, password)
	if err != nil {
		return nil
	}

	s.basic.mu.Lock()
	if s.basic.entries == nil {
		s.basic.entries = make(map[string]basicEntry, 16)
	}
	if len(s.basic.entries) > 256 {
		s.basic.entries = make(map[string]basicEntry, 16)
	}
	s.basic.entries[key] = basicEntry{user: u, expires: time.Now().Add(basicCacheTTL)}
	s.basic.mu.Unlock()
	return u
}

// protected requires authentication when the server is not in public mode.
func (s *Server) protected(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authRequired() && s.currentUser(r) == nil {
			// OPDS clients (MoonReader etc.) use HTTP Basic and have no
			// session cookie. Without a WWW-Authenticate header they don't
			// realize Basic is expected and loop on the password prompt
			// when downloading (and fail to load covers). A browser reaches
			// this with a cookie, so we don't challenge it — otherwise it
			// would pop up a native Basic dialog.
			if _, err := r.Cookie(sessionCookie); err != nil {
				w.Header().Set("WWW-Authenticate", `Basic realm="Polka", charset="UTF-8"`)
			}
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// adminOnly requires an active administrator session regardless of mode.
func (s *Server) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.currentUser(r)
		if u == nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if !u.IsAdmin() {
			http.Error(w, "admin role required", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func userJSON(u *auth.User) map[string]any {
	return map[string]any{
		"id":          u.ID,
		"login":       u.Login,
		"displayName": u.DisplayName,
		"role":        u.Role,
		"disabled":    u.Disabled,
		"createdAt":   u.CreatedAt,
	}
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// --- /auth/* ---

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !s.loginLimiter.allow(clientIP(r)) {
		http.Error(w, "too many login attempts, try again later", http.StatusTooManyRequests)
		return
	}
	token, u, err := s.users.Login(r.Context(), req.Login, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		time.Sleep(300 * time.Millisecond) // slow single attempts on top of the limiter
		http.Error(w, "invalid login or password", http.StatusUnauthorized)
		return
	}
	if err != nil {
		s.apiError(w, err)
		return
	}
	s.setSessionCookie(w, r, token, int((7 * 24 * time.Hour).Seconds()))
	writeJSON(w, map[string]any{"user": userJSON(u)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.users.Logout(r.Context(), c.Value)
	}
	s.setSessionCookie(w, r, "", -1)
	writeJSON(w, map[string]any{"ok": true})
}

// handleMe tells the frontend who is logged in and whether login is required at all.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"user":         nil,
		"authRequired": s.authRequired(),
		"desktop":      s.desktop != nil,
		"version":      s.cfg.Version,
	}
	if u := s.currentUser(r); u != nil {
		resp["user"] = userJSON(u)
	}
	if s.sync != nil {
		resp["sync"] = map[string]any{
			"enabled": true,
			"online":  s.sync.Online(),
			"server":  s.sync.Server(),
		}
	}
	writeJSON(w, resp)
}

// --- /admin/users/* ---

func (s *Server) handleUsersList(w http.ResponseWriter, r *http.Request) {
	users, err := s.users.ListUsers(r.Context())
	if err != nil {
		s.apiError(w, err)
		return
	}
	list := make([]map[string]any, 0, len(users))
	for i := range users {
		list = append(list, userJSON(&users[i]))
	}
	writeJSON(w, map[string]any{"users": list})
}

type userPayload struct {
	Login       string  `json:"login"`
	Password    *string `json:"password"`
	DisplayName *string `json:"displayName"`
	Role        *string `json:"role"`
	Disabled    *bool   `json:"disabled"`
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	var req userPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	password, displayName, role := "", "", auth.RoleUser
	if req.Password != nil {
		password = *req.Password
	}
	if req.DisplayName != nil {
		displayName = *req.DisplayName
	}
	if req.Role != nil {
		role = *req.Role
	}
	u, err := s.users.CreateUser(r.Context(), req.Login, password, displayName, role)
	if err != nil {
		s.authAdminError(w, err)
		return
	}
	writeJSON(w, map[string]any{"user": userJSON(u)})
}

func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var req userPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.users.UpdateUser(r.Context(), id, req.Password, req.DisplayName, req.Role, req.Disabled); err != nil {
		s.authAdminError(w, err)
		return
	}
	u, err := s.users.GetByID(r.Context(), id)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, map[string]any{"user": userJSON(u)})
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.users.DeleteUser(r.Context(), id); err != nil {
		s.authAdminError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// authAdminError maps user-management errors to meaningful responses.
func (s *Server) authAdminError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		http.Error(w, "user not found", http.StatusNotFound)
	case errors.Is(err, auth.ErrLoginTaken):
		http.Error(w, "login already exists", http.StatusConflict)
	case errors.Is(err, auth.ErrLastAdmin):
		http.Error(w, "cannot remove the last administrator", http.StatusConflict)
	default:
		s.apiError(w, err)
	}
}
