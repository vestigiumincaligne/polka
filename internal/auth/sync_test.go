package auth

import (
	"context"
	"testing"
	"time"
)

func TestSyncStateMergeLWW(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "owner", "pass1234", "", RoleAdmin)

	// Local state
	s.SaveProgress(ctx, u.ID, 100, Progress{Chapter: 5, Overall: 0.2})
	s.RateBook(ctx, u.ID, 100, 4)

	// Foreign state: progress is newer, rating is older
	in := &SyncState{
		Progress: []ProgressState{
			{BookID: 100, Chapter: 9, Overall: 0.5, UpdatedAt: "2099-01-01 00:00:00"},
			{BookID: 200, Chapter: 1, Overall: 0.1, UpdatedAt: "2099-01-01 00:00:00"},
		},
		Ratings: []RatingState{
			{BookID: 100, Rating: 2, UpdatedAt: "2000-01-01 00:00:00"}, // older — will lose
		},
		Lists: []ListState{
			{Name: "Хочу прочитать", Builtin: BuiltinWishlist, Books: []ListBookState{{BookID: 100, AddedAt: "2024-01-01 00:00:00"}}},
			{Name: "Отпуск", Books: []ListBookState{{BookID: 200, AddedAt: "2024-01-01 00:00:00"}}},
		},
	}
	if err := s.MergeState(ctx, u.ID, in); err != nil {
		t.Fatal(err)
	}

	// Progress: the newer one won, a new book appeared
	p, _ := s.GetProgress(ctx, u.ID, 100)
	if p.Chapter != 9 {
		t.Errorf("progress chapter = %d, want 9 (newer wins)", p.Chapter)
	}
	if p2, err := s.GetProgress(ctx, u.ID, 200); err != nil || p2.Chapter != 1 {
		t.Errorf("new progress = %+v, %v", p2, err)
	}

	// Rating: the old one did not overwrite the fresh local one
	if r := s.UserRating(ctx, u.ID, 100); r != 4 {
		t.Errorf("rating = %d, want 4 (local newer)", r)
	}

	// Lists: builtin was found, the custom one created, books added
	lists, _ := s.Lists(ctx, u.ID)
	if len(lists) != 2 {
		t.Fatalf("lists = %d, want 2", len(lists))
	}
	for _, l := range lists {
		if l.Books != 1 {
			t.Errorf("list %q books = %d, want 1", l.Name, l.Books)
		}
	}

	// Repeated merge is idempotent
	if err := s.MergeState(ctx, u.ID, in); err != nil {
		t.Fatal(err)
	}
	lists, _ = s.Lists(ctx, u.ID)
	for _, l := range lists {
		if l.Books != 1 {
			t.Errorf("after re-merge list %q books = %d", l.Name, l.Books)
		}
	}

	// Export returns everything with timestamps
	state, err := s.ExportState(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Progress) != 2 || len(state.Ratings) != 1 || len(state.Lists) != 2 {
		t.Errorf("export: %d/%d/%d", len(state.Progress), len(state.Ratings), len(state.Lists))
	}
	if state.Progress[0].UpdatedAt == "" {
		t.Error("updatedAt missing in export")
	}
}

// Two writes within the same second must still be ordered: the merge is
// last-write-wins on updated_at, so timestamps need sub-second precision.
func TestSyncStateSubSecondOrdering(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "owner", "pass1234", "", RoleAdmin)

	s.SaveProgress(ctx, u.ID, 1, Progress{Chapter: 1})
	first, _ := s.ExportState(ctx, u.ID)
	time.Sleep(5 * time.Millisecond) // same second, different millisecond
	s.SaveProgress(ctx, u.ID, 1, Progress{Chapter: 2})
	second, _ := s.ExportState(ctx, u.ID)
	if !(second.Progress[0].UpdatedAt > first.Progress[0].UpdatedAt) {
		t.Fatalf("updated_at must grow between writes: %q then %q", first.Progress[0].UpdatedAt, second.Progress[0].UpdatedAt)
	}
	// Replaying the older state must not roll the newer one back.
	if err := s.MergeState(ctx, u.ID, first); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.GetProgress(ctx, u.ID, 1); p.Chapter != 2 {
		t.Errorf("older state overwrote the newer one: chapter %d", p.Chapter)
	}
	// Legacy whole-second timestamps (rows written before the change)
	// still lose to anything newer with fractional seconds.
	old := &SyncState{Progress: []ProgressState{{BookID: 1, Chapter: 9, UpdatedAt: "2000-01-01 00:00:00"}}}
	s.MergeState(ctx, u.ID, old)
	if p, _ := s.GetProgress(ctx, u.ID, 1); p.Chapter != 2 {
		t.Errorf("legacy timestamp must lose: chapter %d", p.Chapter)
	}
	s.RateBook(ctx, u.ID, 1, 3)
	r1, _ := s.ExportState(ctx, u.ID)
	time.Sleep(5 * time.Millisecond)
	s.RateBook(ctx, u.ID, 1, 5)
	r2, _ := s.ExportState(ctx, u.ID)
	if !(r2.Ratings[0].UpdatedAt > r1.Ratings[0].UpdatedAt) {
		t.Errorf("rating timestamps must grow: %q then %q", r1.Ratings[0].UpdatedAt, r2.Ratings[0].UpdatedAt)
	}
}
