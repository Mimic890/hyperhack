package syncer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Mimic890/hyperhack/internal/mirror"
)

func writeFile(t *testing.T, dir, key, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func keys(objs []Object) []string {
	var out []string
	for _, o := range objs {
		out = append(out, o.Key)
	}
	return out
}

func TestLocalListSkipsHiddenAndSymlinks(t *testing.T) {
	src, outside := t.TempDir(), t.TempDir()
	writeFile(t, outside, "secret.md", post("Secret", "secret"))
	for _, k := range []string{"a-b.md", "a/b.md", "assets/x.png", ".git/objects/1", ".obsidian/app.json", "_templates/t.md", "sub/.hidden.md", "sub/_draft.md"} {
		writeFile(t, src, k, "body")
	}
	for name, target := range map[string]string{"link.md": filepath.Join(outside, "secret.md"), "linkdir": outside, "inner.md": filepath.Join(src, "a-b.md")} {
		if err := os.Symlink(target, filepath.Join(src, name)); err != nil {
			t.Fatal(err)
		}
	}
	objs, truncated, err := NewLocal(src).List(context.Background(), 100)
	if err != nil || truncated {
		t.Fatalf("truncated %v, err %v", truncated, err)
	}
	want := []string{"a-b.md", "a/b.md", "assets/x.png"}
	if got := keys(objs); !slices.Equal(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	if objs[0].Size != 4 || objs[0].ETag == "" {
		t.Errorf("object = %+v", objs[0])
	}
}

// TestLocalListMax: a large .git must not push posts over MAX_OBJECTS.
func TestLocalListMax(t *testing.T) {
	src := t.TempDir()
	for _, k := range []string{".git/1", ".git/2", ".git/3", "a.md", "b.md"} {
		writeFile(t, src, k, "x")
	}
	objs, truncated, err := NewLocal(src).List(context.Background(), 2)
	if err != nil || truncated || len(objs) != 2 {
		t.Fatalf("objs %v, truncated %v, err %v", objs, truncated, err)
	}
	if objs, truncated, _ = NewLocal(src).List(context.Background(), 1); !truncated || !slices.Equal(keys(objs), []string{"a.md"}) {
		t.Errorf("objs %v, truncated %v", objs, truncated)
	}
}

func TestLocalListEmptyOrMissing(t *testing.T) {
	src := t.TempDir()
	if _, _, err := NewLocal(src).List(context.Background(), 10); !errors.Is(err, errEmptySource) {
		t.Errorf("empty: err = %v", err)
	}
	writeFile(t, src, ".stfolder/marker", "") // a fresh Syncthing folder has only this
	if _, _, err := NewLocal(src).List(context.Background(), 10); !errors.Is(err, errEmptySource) {
		t.Errorf("only hidden: err = %v", err)
	}
	if _, _, err := NewLocal(filepath.Join(src, "missing")).List(context.Background(), 10); err == nil {
		t.Error("missing folder: no error")
	}
}

func TestLocalListCancelled(t *testing.T) {
	src := t.TempDir()
	writeFile(t, src, "a.md", "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := NewLocal(src).List(ctx, 10); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestLocalGet(t *testing.T) {
	src, outside := t.TempDir(), t.TempDir()
	writeFile(t, src, "a.md", "body")
	writeFile(t, outside, "secret.md", "secret")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(src, "link.md")); err != nil {
		t.Fatal(err)
	}
	b := NewLocal(src)
	ctx := context.Background()
	objs, _, err := b.List(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := b.Get(ctx, "a.md", objs[0].ETag)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(got) != "body" {
		t.Errorf("body = %q", got)
	}
	if _, err := b.Get(ctx, "a.md", "stale"); err == nil {
		t.Error("stale ETag: no error")
	}
	for _, key := range []string{"link.md", "../" + filepath.Base(outside) + "/secret.md", "missing.md"} {
		if rc, err := b.Get(ctx, key, "any"); err == nil {
			_ = rc.Close()
			t.Errorf("%s: no error", key)
		}
	}
}

// setMTime makes a rewrite visible even when the clock has not moved between two writes.
func setMTime(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestLocalSyncEditAndDelete(t *testing.T) {
	src, dir := t.TempDir(), t.TempDir()
	writeFile(t, src, "one.md", post("One", "one"))
	writeFile(t, src, "two.md", post("Two", "two"))
	writeFile(t, src, "assets/a.png", "png")
	s := newSyncer(t, dir, NewLocal(src))
	ctx := context.Background()
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := slugs(s); !slices.Equal(got, []string{"one", "two"}) {
		t.Fatalf("slugs = %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "assets", "a.png")); err != nil {
		t.Errorf("asset not mirrored: %v", err)
	}

	writeFile(t, src, "one.md", post("Edited", "one"))
	setMTime(t, filepath.Join(src, "one.md"), time.Now().Add(time.Minute))
	if err := os.Remove(filepath.Join(src, "two.md")); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := slugs(s); !slices.Equal(got, []string{"one"}) {
		t.Fatalf("after delete: slugs = %v", got)
	}
	if p := s.Index().Posts()[0]; p.Title != "Edited" {
		t.Errorf("title = %q", p.Title)
	}
	if _, err := os.Stat(filepath.Join(dir, "two.md")); !os.IsNotExist(err) {
		t.Errorf("deleted post still in the mirror: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "one.md")); err != nil {
		t.Errorf("the source must stay untouched: %v", err)
	}
}

// TestLocalEmptyKeepsMirror: an unmounted folder must fail the sync, not wipe the site.
func TestLocalEmptyKeepsMirror(t *testing.T) {
	src, dir := t.TempDir(), t.TempDir()
	writeFile(t, src, "one.md", post("One", "one"))
	s := newSyncer(t, dir, NewLocal(src))
	ctx := context.Background()
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(src, "one.md")); err != nil {
		t.Fatal(err)
	}
	err := s.Sync(ctx)
	if !errors.Is(err, errEmptySource) || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v", err)
	}
	if got := slugs(s); !slices.Equal(got, []string{"one"}) {
		t.Errorf("slugs = %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "one.md")); err != nil {
		t.Errorf("mirror was wiped: %v", err)
	}
	if m, err := mirror.Open(dir); err != nil || !m.Has("one.md", m.State()["one.md"]) {
		t.Errorf("mirror state lost: %v", err)
	}
}
