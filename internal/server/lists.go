package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/vestigiumincaligne/polka/internal/auth"
)

// requireUser fetches the user or responds with 401.
func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) *auth.User {
	u := s.currentUser(r)
	if u == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
	}
	return u
}

func (s *Server) listError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrListNotFound):
		http.Error(w, "list not found", http.StatusNotFound)
	case errors.Is(err, auth.ErrListNameTaken):
		http.Error(w, "list name already exists", http.StatusConflict)
	case errors.Is(err, auth.ErrListBuiltin):
		http.Error(w, "builtin list cannot be modified", http.StatusConflict)
	default:
		s.apiError(w, err)
	}
}

func listJSON(l auth.List) map[string]any {
	return map[string]any{"id": l.ID, "name": l.Name, "builtin": l.Builtin, "books": l.Books}
}

// GET /api/v1/lists — the user's lists.
func (s *Server) handleListsGet(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	lists, err := s.users.Lists(r.Context(), u.ID)
	if err != nil {
		s.apiError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(lists))
	for _, l := range lists {
		out = append(out, listJSON(l))
	}
	writeJSON(w, map[string]any{"lists": out})
}

// POST /api/v1/lists {name} — create a list.
func (s *Server) handleListCreate(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	l, err := s.users.CreateList(r.Context(), u.ID, req.Name)
	if err != nil {
		s.listError(w, err)
		return
	}
	writeJSON(w, map[string]any{"list": listJSON(*l)})
}

func listIDParam(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil
}

// POST /api/v1/lists/{id} {name} — rename.
func (s *Server) handleListRename(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	id, ok := listIDParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := s.users.RenameList(r.Context(), u.ID, id, req.Name); err != nil {
		s.listError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// POST /api/v1/lists/{id}/delete — delete a list.
func (s *Server) handleListDelete(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	id, ok := listIDParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.users.DeleteList(r.Context(), u.ID, id); err != nil {
		s.listError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// GET /api/v1/lists/{id}/books — the list's books (same format as shelves).
func (s *Server) handleListBooks(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	id, ok := listIDParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	limit := intParam(r, "limit", 60)
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}
	ids, err := s.users.ListBookIDs(r.Context(), u.ID, id, limit+1, offset)
	if err != nil {
		s.listError(w, err)
		return
	}
	hasMore := len(ids) > limit
	if hasMore {
		ids = ids[:limit]
	}
	writeJSON(w, map[string]any{
		"titlesList": s.resolveBooksJSON(r, ids),
		"hasMore":    hasMore,
		"nextOffset": offset + len(ids),
	})
}

// POST /api/v1/lists/{id}/books {bookId} — add a book.
func (s *Server) handleListAddBook(w http.ResponseWriter, r *http.Request) {
	s.listBookOp(w, r, s.users.AddToList)
}

// POST /api/v1/lists/{id}/books/remove {bookId} — remove a book.
func (s *Server) handleListRemoveBook(w http.ResponseWriter, r *http.Request) {
	s.listBookOp(w, r, s.users.RemoveFromList)
}

func (s *Server) listBookOp(w http.ResponseWriter, r *http.Request,
	op func(ctx context.Context, userID, listID, bookID int64) error) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	listID, ok := listIDParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var req struct {
		BookID int64 `json:"bookId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || req.BookID <= 0 {
		http.Error(w, "bookId is required", http.StatusBadRequest)
		return
	}
	if err := op(r.Context(), u.ID, listID, req.BookID); err != nil {
		s.listError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// POST /api/v1/books/{id}/wishlist {add} — "Want to read" toggle.
func (s *Server) handleWishlistToggle(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	bookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Add bool `json:"add"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	wl, err := s.users.Wishlist(r.Context(), u.ID)
	if err != nil {
		s.apiError(w, err)
		return
	}
	if req.Add {
		err = s.users.AddToList(r.Context(), u.ID, wl.ID, bookID)
	} else {
		err = s.users.RemoveFromList(r.Context(), u.ID, wl.ID, bookID)
	}
	if err != nil {
		s.listError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "inWishlist": req.Add, "listId": wl.ID})
}
