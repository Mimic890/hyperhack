package syncer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mimic890/hyperhack/internal/content"
	"github.com/Mimic890/hyperhack/internal/mirror"
)

type fakeObj struct {
	etag string
	body string
}

type fakeBucket struct {
	mu      sync.Mutex
	objs    map[string]fakeObj
	listErr error
	getErr  map[string]error
	gets    int
}

func (b *fakeBucket) List(_ context.Context, max int) ([]Object, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listErr != nil {
		return nil, false, b.listErr
	}
	var out []Object
	for k, o := range b.objs {
		out = append(out, Object{Key: k, ETag: o.etag, Size: int64(len(o.body))})
	}
	slices.SortFunc(out, func(a, b Object) int { return strings.Compare(a.Key, b.Key) })
	if len(out) > max {
		return out[:max], true, nil
	}
	return out, false, nil
}

func (b *fakeBucket) Get(_ context.Context, key, etag string) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gets++
	if err := b.getErr[key]; err != nil {
		return nil, err
	}
	o, ok := b.objs[key]
	if !ok || o.etag != etag {
		return nil, errors.New("precondition failed")
	}
	return io.NopCloser(strings.NewReader(o.body)), nil
}

func (b *fakeBucket) set(key, etag, body string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objs[key] = fakeObj{etag, body}
}

func post(title, slug string) string {
	return "---\ntitle: " + title + "\ndate: 2026-11-03\nslug: " + slug + "\n---\nText\n"
}

var opts = Options{PollInterval: time.Hour, MaxPostBytes: 1 << 10, MaxAssetBytes: 1 << 10, MaxObjects: 100}

func newSyncer(t *testing.T, dir string, b Bucket) *Syncer {
	t.Helper()
	m, err := mirror.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := New(b, m, content.NewRenderer("en"), opts, slog.New(slog.DiscardHandler))
	s.retry = []time.Duration{0}
	return s
}

func slugs(s *Syncer) []string {
	var out []string
	for _, p := range s.Index().Posts() {
		out = append(out, p.Slug)
	}
	slices.Sort(out)
	return out
}

func TestSync(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBucket{objs: map[string]fakeObj{
		"one.md":                {"e1", post("One", "one")},
		"deep/folder/two.md":    {"e1", post("Two", "two")},
		"broken.md":             {"e1", "no frontmatter"},
		"assets/img/a.png":      {"e1", "png"},
		"assets/evil.html":      {"e1", "<script>"},
		".obsidian/app.json":    {"e1", "{}"},
		"_templates/t.md":       {"e1", post("T", "t")},
		"big.md":                {"e1", strings.Repeat("x", 2<<10)},
		"/abs.md":               {"e1", post("Abs", "abs")},
		"bad\\key.md":           {"e1", post("Bad", "bad")},
		"assets/../../../x.png": {"e1", "x"},
	}}
	s := newSyncer(t, dir, b)
	ctx := context.Background()
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := slugs(s); !slices.Equal(got, []string{"one", "two"}) {
		t.Fatalf("slugs = %v", got)
	}
	if s.Index().Logo {
		t.Error("Logo without assets/logo.svg")
	}
	for _, f := range []string{"one.md", "deep/folder/two.md", "broken.md", "assets/img/a.png", mirror.StateFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	for _, f := range []string{"assets/evil.html", ".obsidian", "_templates", "big.md", "x.png"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s should not be mirrored", f)
		}
	}

	// Nothing changed: no downloads.
	gets := b.gets
	if err := s.Sync(ctx); err != nil || b.gets != gets {
		t.Fatalf("second sync: err %v, gets %d → %d", err, gets, b.gets)
	}

	// Update, delete, fix the broken post, add a logo.
	b.set(LogoKey, "e1", "<svg/>")
	b.set("one.md", "e2", post("One v2", "one"))
	b.set("broken.md", "e2", post("Fixed", "fixed"))
	delete(b.objs, "deep/folder/two.md")
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := slugs(s); !slices.Equal(got, []string{"fixed", "one"}) {
		t.Fatalf("slugs = %v", got)
	}
	if s.Index().Get("one").Title != "One v2" {
		t.Error("one.md not updated")
	}
	if !s.Index().Logo {
		t.Error("Logo not set after assets/logo.svg appeared")
	}
	if _, err := os.Stat(filepath.Join(dir, "deep/folder/two.md")); err == nil {
		t.Error("deleted object still on disk")
	}

	// A failed fetch keeps the old copy.
	b.set("one.md", "e3", post("One v3", "one"))
	b.getErr = map[string]error{"one.md": errors.New("boom")}
	if err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Index().Get("one").Title != "One v2" {
		t.Error("old copy lost after a failed fetch")
	}

	// A list error changes nothing.
	b.listErr = errors.New("down")
	if err := s.Sync(ctx); err == nil {
		t.Fatal("want list error")
	}
	if len(s.Index().Posts()) != 2 {
		t.Error("index changed after list error")
	}

	// Restart from disk, S3 down: the index comes from the mirror.
	s2 := newSyncer(t, dir, b)
	var ready atomic.Bool
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error)
	go func() { done <- s2.Run(runCtx, &ready) }()
	deadline := time.Now().Add(5 * time.Second)
	for !ready.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !ready.Load() || !slices.Equal(slugs(s2), []string{"fixed", "one"}) {
		t.Fatalf("restart: ready %v, slugs %v", ready.Load(), slugs(s2))
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestRunNoMirrorS3Down(t *testing.T) {
	s := newSyncer(t, t.TempDir(), &fakeBucket{listErr: errors.New("down")})
	var ready atomic.Bool
	if err := s.Run(context.Background(), &ready); err == nil || ready.Load() {
		t.Fatalf("Run = %v, ready %v; want error, not ready", err, ready.Load())
	}
}

func TestRunEmptyBucket(t *testing.T) {
	dir := t.TempDir()
	s := newSyncer(t, dir, &fakeBucket{objs: map[string]fakeObj{}})
	var ready atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- s.Run(ctx, &ready) }()
	deadline := time.Now().Add(5 * time.Second)
	for !ready.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil || !ready.Load() {
		t.Fatalf("Run = %v, ready %v", err, ready.Load())
	}
	if _, err := os.Stat(filepath.Join(dir, mirror.StateFile)); err != nil {
		t.Error("state file not written for an empty bucket")
	}
}

func TestSyncMaxObjects(t *testing.T) {
	b := &fakeBucket{objs: map[string]fakeObj{}}
	for _, k := range []string{"a", "b", "c"} {
		b.set(k+".md", "e", post(k, k))
	}
	s := newSyncer(t, t.TempDir(), b)
	s.opts.MaxObjects = 2
	if err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := slugs(s); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("slugs = %v", got)
	}
}

func TestSyncConcurrent(t *testing.T) {
	b := &fakeBucket{objs: map[string]fakeObj{"a.md": {"e", post("A", "a")}}}
	s := newSyncer(t, t.TempDir(), b)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := s.Sync(context.Background()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if b.gets != 1 {
		t.Errorf("gets = %d, want 1", b.gets)
	}
}
