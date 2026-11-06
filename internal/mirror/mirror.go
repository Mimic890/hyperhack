// Package mirror keeps a local copy of the bucket on disk.
// All file access goes through os.Root, so a key cannot escape the mirror directory.
package mirror

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// StateFile maps object keys to ETags. A "." name never collides with a synced key.
const StateFile = ".hyperhack.json"

// Kind says what the service does with a bucket key.
type Kind int

const (
	Ignore Kind = iota // not mirrored
	Post               // .md file outside assets/
	Asset              // allowlisted file inside assets/
	Home               // home.md in the root: main page blocks, not a post
)

// assetExts matches the Caddy allowlist for /assets/*.
var assetExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".avif": true,
	".gif": true, ".svg": true, ".mp4": true, ".webm": true,
}

// Classify checks a bucket key and says how to treat it.
// Unsafe keys (absolute, "\", NUL, control characters, empty segments) return an error.
func Classify(key string) (Kind, error) {
	if key == "" || len(key) > 1024 || !utf8.ValidString(key) {
		return Ignore, errors.New("invalid key")
	}
	if strings.HasPrefix(key, "/") {
		return Ignore, errors.New("absolute key")
	}
	for _, r := range key {
		if r == '\\' || unicode.IsControl(r) {
			return Ignore, errors.New("key has a backslash or control character")
		}
	}
	if strings.HasSuffix(key, "/") {
		return Ignore, nil // folder marker
	}
	segs := strings.Split(key, "/")
	for _, s := range segs {
		if s == "" {
			return Ignore, errors.New("key has an empty path segment")
		}
		if s[0] == '.' || s[0] == '_' { // also covers "." and ".."
			return Ignore, nil
		}
	}
	ext := strings.ToLower(path.Ext(key))
	if segs[0] == "assets" {
		if len(segs) > 1 && assetExts[ext] {
			return Asset, nil
		}
		return Ignore, nil
	}
	if key == "home.md" {
		return Home, nil
	}
	if ext == ".md" {
		return Post, nil
	}
	return Ignore, nil
}

// Mirror is the local copy. It is not safe for concurrent use.
type Mirror struct {
	root    *os.Root
	state   map[string]string
	existed bool
}

// Open opens dir (it must exist) and loads the state file, if any.
func Open(dir string) (*Mirror, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	m := &Mirror{root: root, state: map[string]string{}}
	b, err := root.ReadFile(StateFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return m, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(b, &m.state); err != nil {
		return nil, fmt.Errorf("%s: %w", StateFile, err)
	}
	m.existed = true
	return m, nil
}

// Existed reports whether a state file was found on Open, i.e. an earlier sync finished.
func (m *Mirror) Existed() bool { return m.existed }

// State returns the key → ETag map. Callers must not change it.
func (m *Mirror) State() map[string]string { return m.state }

// Has reports whether key is on disk with this ETag.
func (m *Mirror) Has(key, etag string) bool {
	if m.state[key] != etag {
		return false
	}
	_, err := m.root.Stat(key)
	return err == nil
}

// Write stores r as key atomically (temp file + rename). More than max bytes is an error.
// shortcut: a crash can leave a ".tmp-*" file (Caddy never serves it); clean up on start if they pile up.
func (m *Mirror) Write(key, etag string, r io.Reader, max int64) error {
	dir := path.Dir(key)
	if err := m.root.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path.Join(dir, ".tmp-"+randHex())
	if err := m.writeFile(tmp, r, max, 0o644); err != nil {
		return err
	}
	if err := m.root.Rename(tmp, key); err != nil {
		_ = m.root.Remove(tmp)
		return err
	}
	m.state[key] = etag
	return nil
}

func (m *Mirror) writeFile(name string, r io.Reader, max int64, mode fs.FileMode) (err error) {
	f, err := m.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = m.root.Remove(name)
		}
	}()
	// Chmod: the umask must not hide files from Caddy.
	if err := f.Chmod(mode); err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, max+1))
	if err != nil {
		return err
	}
	if n > max {
		return fmt.Errorf("larger than %d bytes", max)
	}
	return f.Sync()
}

// Remove deletes key from disk and state.
// shortcut: empty parent folders stay; remove them if they ever pile up.
func (m *Mirror) Remove(key string) error {
	if err := m.root.Remove(key); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	delete(m.state, key)
	return nil
}

// ReadFile reads a mirrored key.
func (m *Mirror) ReadFile(key string) ([]byte, error) { return m.root.ReadFile(key) }

// Save writes the state file atomically.
func (m *Mirror) Save() error {
	b, err := json.Marshal(m.state)
	if err != nil {
		return err
	}
	tmp := StateFile + ".tmp-" + randHex()
	if err := m.writeFile(tmp, bytes.NewReader(b), int64(len(b)), 0o600); err != nil {
		return err
	}
	if err := m.root.Rename(tmp, StateFile); err != nil {
		_ = m.root.Remove(tmp)
		return err
	}
	m.existed = true
	return nil
}

func randHex() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	return hex.EncodeToString(b)
}
