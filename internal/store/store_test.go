package store

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}

func TestStore(t *testing.T) {
	s, dir := open(t)
	ctx := context.Background()
	if fi, err := os.Stat(filepath.Join(dir, FileName)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("db file mode: %v %v", fi.Mode(), err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.Add(ctx, "2026-11-01", Inc{"a", View, "", 5}, Inc{"b", View, "", 1}, Inc{"a", Source, "telegram", 2}))
	must(s.Add(ctx, "2026-11-04", Inc{"b", View, "", 3}, Inc{"a", View, "", 1}))
	if n, _ := s.Views(ctx, "a"); n != 6 {
		t.Errorf("views a = %d", n)
	}
	if n, _ := s.Views(ctx, "none"); n != 0 {
		t.Errorf("views none = %d", n)
	}
	if p, _ := s.Popular(ctx, "2026-11-02"); !slices.Equal(p, []string{"b", "a"}) {
		t.Errorf("popular recent = %v", p)
	}
	if p, _ := s.Popular(ctx, "2026-10-01"); !slices.Equal(p, []string{"a", "b"}) {
		t.Errorf("popular all = %v", p)
	}
	tot, _ := s.Totals(ctx)
	if len(tot) != 3 || tot[0] != (Total{"a", Source, "telegram", 2}) {
		t.Errorf("totals = %v", tot)
	}

	vid1, vid2 := []byte("v1"), []byte("v2")
	if prev, _ := s.SetVote(ctx, vid1, "a", Like); prev != None {
		t.Errorf("prev = %d", prev)
	}
	_, _ = s.SetVote(ctx, vid2, "a", Dislike)
	if prev, _ := s.SetVote(ctx, vid2, "a", Like); prev != Dislike {
		t.Errorf("prev = %d", prev)
	}
	if v, _ := s.PostVotes(ctx, "a"); v != (Votes{2, 0}) {
		t.Errorf("votes = %+v", v)
	}
	_, _ = s.SetVote(ctx, vid1, "a", None)
	if v, _ := s.VoteOf(ctx, vid1, "a"); v != None {
		t.Errorf("vote after none = %d", v)
	}
	if m, _ := s.VoteCounts(ctx); m["a"] != (Votes{1, 0}) {
		t.Errorf("vote counts = %v", m)
	}
	if _, err := s.SetVote(ctx, vid1, "a", 7); err == nil {
		t.Error("vote 7 accepted")
	}

	for range 3 {
		must(s.LogSearch(ctx, "2026-11-04", "go", false))
	}
	must(s.LogSearch(ctx, "2026-11-04", "kubernetes", true))
	must(s.LogSearch(ctx, "2026-08-01", "old", true))
	if top, _ := s.TopSearches(ctx, 10, false); len(top) != 3 || top[0] != (QueryCount{"go", 3}) {
		t.Errorf("top = %v", top)
	}
	if top, _ := s.TopSearches(ctx, 10, true); len(top) != 2 || top[0].Query != "kubernetes" {
		t.Errorf("top zero = %v", top)
	}
	must(s.Prune(ctx, "2026-10-01", "2026-11-02", 1))
	if top, _ := s.TopSearches(ctx, 10, false); len(top) != 1 || top[0].Query != "go" {
		t.Errorf("after prune = %v", top)
	}
	if p, _ := s.Popular(ctx, "2000-01-01"); !slices.Equal(p, []string{"b", "a"}) {
		t.Errorf("popular after prune = %v", p)
	}

	// Reopen: data survives.
	_ = s.Close()
	s2, err := Open(dir, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	if n, _ := s2.Views(ctx, "a"); n != 6 {
		t.Errorf("views after reopen = %d", n)
	}
}
