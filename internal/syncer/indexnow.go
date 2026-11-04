package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Mimic890/hyperhack/internal/content"
)

const (
	indexNowURL = "https://api.indexnow.org/indexnow"
	maxIndexNow = 10000 // IndexNow limit per request
)

// changedURLs lists posts that are new, changed or removed between two indexes.
func (s *Syncer) changedURLs(old, cur *content.Index) []string {
	base := s.opts.SiteURL.String() + "/posts/"
	var urls []string
	for _, p := range cur.Posts() {
		if o := old.Get(p.Slug); o == nil || !bytes.Equal(o.Source, p.Source) {
			urls = append(urls, base+p.Slug)
		}
	}
	for _, o := range old.Posts() {
		if cur.Get(o.Slug) == nil {
			urls = append(urls, base+o.Slug)
		}
	}
	return urls[:min(len(urls), maxIndexNow)] // shortcut: drops URLs past 10000 in one sync, batch if a vault ever gets that big
}

// indexNow tells search engines about changed URLs. Failures are logged and not retried.
func (s *Syncer) indexNow(urls []string) {
	body, err := json.Marshal(map[string]any{
		"host":        s.opts.SiteURL.Host,
		"key":         s.opts.IndexNowKey,
		"keyLocation": s.opts.SiteURL.String() + "/" + s.opts.IndexNowKey + ".txt",
		"urlList":     urls,
	})
	if err != nil {
		s.log.Warn("indexnow", "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.indexNowURL, bytes.NewReader(body))
	if err != nil {
		s.log.Warn("indexnow", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.log.Warn("indexnow ping failed", "err", err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		s.log.Warn("indexnow ping rejected", "status", resp.StatusCode, "urls", len(urls))
		return
	}
	s.log.Info("indexnow ping sent", "urls", len(urls))
}
