package mirror

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		key     string
		want    Kind
		wantErr bool
	}{
		{"post.md", Post, false},
		{"home.md", Home, false},
		{"notes/home.md", Post, false},
		{"Home.md", Post, false},
		{"_home.md", Ignore, false},
		{"notes/deep/Пост.MD", Post, false},
		{"assets/a.webp", Asset, false},
		{"assets/covers/b.JPG", Asset, false},
		{"assets/x.md", Ignore, false},
		{"assets/x.html", Ignore, false},
		{"assets", Ignore, false},
		{"readme.txt", Ignore, false},
		{"folder/", Ignore, false},
		{".obsidian/app.json", Ignore, false},
		{"_templates/t.md", Ignore, false},
		{"a/.hidden.md", Ignore, false},
		{"../escape.md", Ignore, false},
		{"a/../../escape.md", Ignore, false},
		{"assets/../x.md", Ignore, false},
		{"./post.md", Ignore, false},
		{"/etc/passwd.md", Ignore, true},
		{`a\..\b.md`, Ignore, true},
		{"a\x00.md", Ignore, true},
		{"a\nb.md", Ignore, true},
		{"a\u0085b.md", Ignore, true},
		{"a//b.md", Ignore, true},
		{"", Ignore, true},
		{"\xff.md", Ignore, true},
		{strings.Repeat("a", 1025) + ".md", Ignore, true},
	}
	for _, tt := range tests {
		got, err := Classify(tt.key)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("Classify(%q) = %v, %v; want %v, err %v", tt.key, got, err, tt.want, tt.wantErr)
		}
	}
}

// FuzzClassify checks that every mirrored key is a clean relative path.
func FuzzClassify(f *testing.F) {
	for _, s := range []string{"post.md", "assets/a.png", "../x.md", "a/./b.md", `a\b.md`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, key string) {
		k, err := Classify(key)
		if err != nil || k == Ignore {
			return
		}
		if path.Clean(key) != key || path.IsAbs(key) || strings.HasPrefix(key, "..") {
			t.Fatalf("Classify(%q) = %v for an unclean path", key, k)
		}
	})
}

func TestMirror(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Existed() {
		t.Fatal("Existed on empty dir")
	}
	if err := m.Write("a/b/post.md", "e1", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	if !m.Has("a/b/post.md", "e1") || m.Has("a/b/post.md", "e2") {
		t.Fatal("Has wrong")
	}
	if err := m.Write("big.md", "e1", strings.NewReader("123456"), 5); err == nil {
		t.Fatal("want size error")
	}
	if _, err := os.Stat(filepath.Join(dir, "big.md")); !os.IsNotExist(err) {
		t.Fatal("oversized file left on disk")
	}
	fi, err := os.Stat(filepath.Join(dir, "a/b/post.md"))
	if err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, %v", fi.Mode(), err)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, StateFile)); fi.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %v", fi.Mode())
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "a/b"))
	if len(entries) != 1 {
		t.Fatalf("temp files left: %v", entries)
	}

	m2, err := Open(dir)
	if err != nil || !m2.Existed() || m2.State()["a/b/post.md"] != "e1" {
		t.Fatalf("reopen: %v %v", err, m2.State())
	}
	if b, err := m2.ReadFile("a/b/post.md"); err != nil || string(b) != "hello" {
		t.Fatalf("ReadFile = %q, %v", b, err)
	}
	if err := m2.Remove("a/b/post.md"); err != nil {
		t.Fatal(err)
	}
	if m2.Has("a/b/post.md", "e1") {
		t.Fatal("still there after Remove")
	}
	if err := m2.Remove("missing.md"); err != nil {
		t.Fatal(err)
	}
}

func TestMirrorSymlinkEscape(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skip(err)
	}
	m, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Write("link/x.md", "e", strings.NewReader("x"), 10); err == nil {
		t.Fatal("write through a symlink out of the root succeeded")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("file escaped the root: %v", entries)
	}
}
