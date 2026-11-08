package server

import (
	"net/http"
	"os"

	"github.com/Mimic890/hyperhack/internal/mirror"
)

// assetCSP matches /assets/* in Caddyfile.example: a script inside an SVG cannot run.
const assetCSP = "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox"

// asset serves /assets/* from the mirror. Registered only with LOCAL_VAULT_DIR; with S3, Caddy does this.
// It opens its own os.Root because mirror.Mirror is not safe for concurrent use.
func (p *pages) asset(w http.ResponseWriter, r *http.Request) {
	key := "assets/" + r.PathValue("path")
	if k, err := mirror.Classify(key); err != nil || k != mirror.Asset {
		p.notFound(w, r)
		return
	}
	root, err := os.OpenRoot(p.cfg.VaultDir)
	if err != nil {
		p.log.Error("open vault dir", "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(key)
	if err != nil {
		p.notFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		p.notFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", assetCSP)
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}
