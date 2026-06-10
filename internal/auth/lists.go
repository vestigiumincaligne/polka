package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Reading lists live in users.db and survive collection re-import.

const listsSchema = `
CREATE TABLE IF NOT EXISTS lists (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	builtin    TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	UNIQUE (user_id, name)
);

CREATE TABLE IF NOT EXISTS list_books (
	list_id  INTEGER NOT NULL REFERENCES lists (id) ON DELETE CASCADE,
	book_id  INTEGER NOT NULL,
	added_at TEXT NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (list_id, book_id)
) WITHOUT ROWID;
`

// BuiltinWishlist is the built-in "Want to read" list.
const BuiltinWishlist = "wishlist"

var (
	ErrListNotFound  = errors.New("list not found")
	ErrListNameTaken = errors.New("list name already exists")
	ErrListBuiltin   = errors.New("builtin list cannot be modified")
)

type List struct {
	ID      int64
	Name    string
	Builtin string
	Books   int
}

func (s *Service) Lists(ctx context.Context, userID int64) ([]List, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.name, l.builtin,
		       (SELECT count(*) FROM list_books lb WHERE lb.list_id = l.id)
		FROM lists l WHERE l.user_id = ?
		ORDER BY l.builtin DESC, l.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []List
	for rows.Next() {
		var l List
		if err := rows.Scan(&l.ID, &l.Name, &l.Builtin, &l.Books); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Service) CreateList(ctx context.Context, userID int64, name string) (*List, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("list name is required")
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO lists (user_id, name) VALUES (?, ?)`, userID, name)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrListNameTaken
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &List{ID: id, Name: name}, nil
}

// Wishlist returns the user's built-in list, creating it if needed.
func (s *Service) Wishlist(ctx context.Context, userID int64) (*List, error) {
	var l List
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, builtin FROM lists WHERE user_id = ? AND builtin = ?`,
		userID, BuiltinWishlist).Scan(&l.ID, &l.Name, &l.Builtin)
	if err == nil {
		return &l, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO lists (user_id, name, builtin) VALUES (?, ?, ?)`,
		userID, "Хочу прочитать", BuiltinWishlist)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &List{ID: id, Name: "Хочу прочитать", Builtin: BuiltinWishlist}, nil
}

// userList checks that the list belongs to the user.
func (s *Service) userList(ctx context.Context, userID, listID int64) (*List, error) {
	var l List
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, builtin FROM lists WHERE id = ? AND user_id = ?`,
		listID, userID).Scan(&l.ID, &l.Name, &l.Builtin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrListNotFound
	}
	return &l, err
}

func (s *Service) RenameList(ctx context.Context, userID, listID int64, name string) error {
	l, err := s.userList(ctx, userID, listID)
	if err != nil {
		return err
	}
	if l.Builtin != "" {
		return ErrListBuiltin
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("list name is required")
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE lists SET name = ? WHERE id = ?`, name, listID); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrListNameTaken
		}
		return err
	}
	return nil
}

func (s *Service) DeleteList(ctx context.Context, userID, listID int64) error {
	l, err := s.userList(ctx, userID, listID)
	if err != nil {
		return err
	}
	if l.Builtin != "" {
		return ErrListBuiltin
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM lists WHERE id = ?`, listID)
	return err
}

func (s *Service) AddToList(ctx context.Context, userID, listID, bookID int64) error {
	if _, err := s.userList(ctx, userID, listID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO list_books (list_id, book_id) VALUES (?, ?)`, listID, bookID)
	return err
}

func (s *Service) RemoveFromList(ctx context.Context, userID, listID, bookID int64) error {
	if _, err := s.userList(ctx, userID, listID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM list_books WHERE list_id = ? AND book_id = ?`, listID, bookID)
	return err
}

// ListBookIDs returns the list's books, recently added first.
func (s *Service) ListBookIDs(ctx context.Context, userID, listID int64, limit, offset int) ([]int64, error) {
	if _, err := s.userList(ctx, userID, listID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT book_id FROM list_books WHERE list_id = ?
		ORDER BY added_at DESC, book_id DESC LIMIT ? OFFSET ?`, listID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// BookListIDs returns which of the user's lists contain the book.
func (s *Service) BookListIDs(ctx context.Context, userID, bookID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT lb.list_id FROM list_books lb
		JOIN lists l ON l.id = lb.list_id
		WHERE l.user_id = ? AND lb.book_id = ?`, userID, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
