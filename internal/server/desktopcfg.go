package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/vestigiumincaligne/polka/internal/syncer"
)

// Server connection configuration from the desktop client UI.
// Available only in desktop mode; applying it requires restarting
// the application (routes and the syncer are assembled at startup).

func (s *Server) registerDesktopConfigRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/sync/config", s.handleSyncConfigGet)
	mux.HandleFunc("POST /api/v1/sync/config", s.handleSyncConfigSave)
	mux.HandleFunc("POST /api/v1/sync/disconnect", s.handleSyncDisconnect)
	if s.sync == nil {
		// So the "Server" page can render even without active sync.
		mux.HandleFunc("GET /api/v1/sync/info", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{"enabled": false})
		})
	}
}

func (s *Server) handleSyncConfigGet(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]any{"configured": false, "active": s.sync != nil}
	if cfg, err := syncer.LoadConfig(s.cfg.DataDir); err == nil {
		resp["configured"] = true
		resp["server"] = cfg.Server
		resp["login"] = cfg.Login
	}
	writeJSON(w, resp)
}

func (s *Server) handleSyncConfigSave(w http.ResponseWriter, r *http.Request) {
	var req syncer.Config
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	req.Server = strings.TrimSpace(req.Server)
	req.Login = strings.TrimSpace(req.Login)
	if req.Server == "" || req.Login == "" || req.Password == "" {
		http.Error(w, "server, login and password are required", http.StatusBadRequest)
		return
	}

	if err := syncer.Validate(r.Context(), &req); err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, syncer.ErrBadCredentials) {
			status = http.StatusUnauthorized
		}
		http.Error(w, err.Error(), status)
		return
	}

	if err := syncer.SaveConfig(s.cfg.DataDir, &req); err != nil {
		s.apiError(w, err)
		return
	}
	s.log.Info("sync config saved", "server", req.Server, "login", req.Login)
	writeJSON(w, map[string]any{"ok": true, "restartRequired": true})
}

func (s *Server) handleSyncDisconnect(w http.ResponseWriter, _ *http.Request) {
	if err := syncer.RemoveConfig(s.cfg.DataDir); err != nil {
		s.apiError(w, err)
		return
	}
	s.log.Info("sync config removed")
	writeJSON(w, map[string]any{"ok": true, "restartRequired": true})
}
