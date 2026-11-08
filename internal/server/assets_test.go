package server

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Mimic890/hyperhack/internal/content"
)

func assetHandler(t *testing.T, local bool) (http.Handler, string) {
	t.Helper()
	vault := t.TempDir()
	for name, body := range map[string]string{
		"assets/a.png":    "\x89PNG\r\n\x1a\n",
		"assets/x.svg":    `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"assets/note.md":  "# private",
		"assets/x.html":   "<script>alert(1)</script>",
		"assets/.tmp-1":   "partial",
		"hello.md":        "# post",
		".hyperhack.json": "{}",
	} {
		p := filepath.Join(vault, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := testConfig()
	cfg.VaultDir = vault
	if local {
		cfg.LocalVaultDir = "/source"
	}
	return New(slog.New(slog.DiscardHandler), cfg, testDeps(t, func() *content.Index { return nil })), vault
}

func TestAssetsLocalMode(t *testing.T) {
	h, vault := assetHandler(t, true)
	for path, wantType := range map[string]string{"/assets/a.png": "image/png", "/assets/x.svg": "image/svg+xml"} {
		rec := do(h, "GET", path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != wantType {
			t.Errorf("%s type = %q", path, got)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != assetCSP {
			t.Errorf("%s CSP = %q", path, got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s nosniff = %q", path, got)
		}
	}
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(vault, "assets", "link.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(vault, "assets", "dir.png"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/assets/note.md", "/assets/x.html", "/assets/.tmp-1", "/assets/", "/assets/missing.png",
		"/assets/link.png", "/assets/dir.png", "/hello.md", "/.hyperhack.json",
		"/assets/..%2fhello.md", "/assets/%2e%2e/.hyperhack.json",
	} {
		if got := do(h, "GET", path).Code; got != http.StatusNotFound && got != http.StatusMovedPermanently && got != http.StatusTemporaryRedirect {
			t.Errorf("%s = %d, want 404", path, got)
		}
		if body := do(h, "GET", path).Body.String(); body == "secret" || body == "{}" || body == "# post" {
			t.Errorf("%s leaked %q", path, body)
		}
	}
}

// TestAssetsNotServedWithS3: with S3 the service must keep leaving /assets/* to Caddy.
func TestAssetsNotServedWithS3(t *testing.T) {
	h, _ := assetHandler(t, false)
	if got := do(h, "GET", "/assets/a.png").Code; got != http.StatusNotFound {
		t.Errorf("/assets/a.png = %d, want 404", got)
	}
}
