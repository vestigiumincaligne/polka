package auth

import (
	"context"
	"slices"
	"testing"
)

func TestReadMarks(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "reader", "pass1234", "", RoleUser)
	if err != nil {
		t.Fatal(err)
	}

	// Marking read moves the book out of "Want to read".
	wl, _ := s.Wishlist(ctx, u.ID)
	s.AddToList(ctx, u.ID, wl.ID, 10)
	rl, err := s.SetRead(ctx, u.ID, 10, true)
	if err != nil || rl.Builtin != BuiltinRead {
		t.Fatalf("SetRead = %+v, %v", rl, err)
	}
	if ids, _ := s.ListBookIDs(ctx, u.ID, wl.ID, 10, 0); len(ids) != 0 {
		t.Errorf("still in wishlist: %v", ids)
	}
	if ids, _ := s.ReadBookIDs(ctx, u.ID); !slices.Equal(ids, []int64{10}) {
		t.Errorf("read ids = %v", ids)
	}
	// The builtin list cannot be renamed or deleted.
	if err := s.DeleteList(ctx, u.ID, rl.ID); err != ErrListBuiltin {
		t.Errorf("delete builtin read list: %v", err)
	}
	if _, err := s.SetRead(ctx, u.ID, 10, false); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.ReadBookIDs(ctx, u.ID); len(ids) != 0 {
		t.Errorf("unmark failed: %v", ids)
	}
}

func TestReadListNameClash(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "reader", "pass1234", "", RoleUser)
	if _, err := s.CreateList(ctx, u.ID, "Прочитано"); err != nil {
		t.Fatal(err)
	}
	rl, err := s.ReadList(ctx, u.ID)
	if err != nil || rl.Builtin != BuiltinRead || rl.Name == "Прочитано" {
		t.Fatalf("read list = %+v, %v", rl, err)
	}
	again, _ := s.ReadList(ctx, u.ID)
	if again.ID != rl.ID {
		t.Errorf("second ReadList created another list")
	}
}

func TestAutoReadAtThreshold(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "reader", "pass1234", "", RoleUser)

	s.SaveProgress(ctx, u.ID, 7, Progress{Overall: 0.5})
	if ids, _ := s.ReadBookIDs(ctx, u.ID); len(ids) != 0 {
		t.Fatalf("marked read at 50%%: %v", ids)
	}
	// The reader crosses 97% -> read; "continue reading" drops it.
	s.SaveProgress(ctx, u.ID, 7, Progress{Overall: 0.97})
	if ids, _ := s.ReadBookIDs(ctx, u.ID); !slices.Equal(ids, []int64{7}) {
		t.Fatalf("not marked at 97%%: %v", ids)
	}
	if p, _ := s.ListProgress(ctx, u.ID, 10); len(p) != 0 {
		t.Errorf("read book still in continue-reading: %v", p)
	}

	// Unmarked by the user: later saves near the end do not re-mark it.
	s.SetRead(ctx, u.ID, 7, false)
	s.SaveProgress(ctx, u.ID, 7, Progress{Overall: 0.99})
	if ids, _ := s.ReadBookIDs(ctx, u.ID); len(ids) != 0 {
		t.Errorf("re-marked after unmark: %v", ids)
	}

	// KOReader progress (overall only) marks too.
	s.SaveOverall(ctx, u.ID, 8, 0.3)
	s.SaveOverall(ctx, u.ID, 8, 0.98)
	if ids, _ := s.ReadBookIDs(ctx, u.ID); !slices.Contains(ids, 8) {
		t.Errorf("kosync progress did not mark read: %v", ids)
	}

	// Marked by hand at 40%: hidden from "continue reading".
	s.SaveProgress(ctx, u.ID, 9, Progress{Overall: 0.4})
	s.SetRead(ctx, u.ID, 9, true)
	if p, _ := s.ListProgress(ctx, u.ID, 10); len(p) != 0 {
		t.Errorf("manually read book in continue-reading: %v", p)
	}
}

func TestReadBackfill(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "reader", "pass1234", "", RoleUser)
	// Progress stored before the feature existed (no crossing seen).
	s.db.Exec(`INSERT INTO reading_progress (user_id, book_id, overall, updated_at) VALUES (?, 1, 0.97, datetime('now')), (?, 2, 0.5, datetime('now'))`, u.ID, u.ID)
	s.db.Exec(`DELETE FROM settings WHERE key = 'read_backfill_v1'`)
	if err := s.backfillRead(ctx); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.ReadBookIDs(ctx, u.ID); !slices.Equal(ids, []int64{1}) {
		t.Errorf("backfill read ids = %v", ids)
	}
	// Runs once: a later unmark is not undone by another start.
	s.SetRead(ctx, u.ID, 1, false)
	s.backfillRead(ctx)
	if ids, _ := s.ReadBookIDs(ctx, u.ID); len(ids) != 0 {
		t.Errorf("backfill ran twice: %v", ids)
	}
}
