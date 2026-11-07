// Package syncer mirrors the bucket to disk and rebuilds the post index.
package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Mimic890/hyperhack/internal/content"
	"github.com/Mimic890/hyperhack/internal/mirror"
)

const fetchTimeout = 5 * time.Minute

// LogoKey is the logo used when home.md sets none; Caddy serves it as /assets/logo.svg.
const LogoKey = "assets/logo.svg"

// HomeKey lists the main page blocks.
const HomeKey = "home.md"

// Options holds the limits from the config.
type Options struct {
	PollInterval  time.Duration
	MaxPostBytes  int64
	MaxAssetBytes int64
	MaxObjects    int
	SiteURL       *url.URL
	IndexNowKey   string // empty: no IndexNow pings
}

// Syncer runs one sync at a time; Sync calls wait for the running one.
type Syncer struct {
	bucket Bucket
	mirror *mirror.Mirror
	render *content.Renderer
	opts   Options
	log    *slog.Logger // component sync
	rlog   *slog.Logger // component render
	retry  []time.Duration

	indexNowURL string

	mu        sync.Mutex
	cache     map[string]cached // post key → last parse result
	home      []content.Block   // blocks from homeETag
	theme     content.Theme     // site colors from homeETag
	logo      content.Logo      // site logos from homeETag
	homeETag  string            // "" when there is no home.md
	homeRead  bool
	warned    map[string]string // key → ETag already warned about, so polls do not repeat WARNs
	maxWarned bool
	index     atomic.Pointer[content.Index]

	lastAttempt, lastSuccess atomic.Int64 // unix seconds
	errors, fetchFailures    atomic.Int64
	objects                  atomic.Int64
}

// Stats is the sync status for /metrics.
type Stats struct {
	LastAttempt, LastSuccess int64 // unix seconds, 0 if never
	Errors                   int64 // failed syncs (listing or saving state)
	FetchFailures            int64 // objects that failed to download
	Objects                  int64 // keys in the last listing
}

// Stats returns the sync status counters.
func (s *Syncer) Stats() Stats {
	return Stats{
		LastAttempt: s.lastAttempt.Load(), LastSuccess: s.lastSuccess.Load(),
		Errors: s.errors.Load(), FetchFailures: s.fetchFailures.Load(), Objects: s.objects.Load(),
	}
}

type cached struct {
	etag string
	post *content.Post // nil: skipped
}

// New returns a syncer. log is the base logger; components are added here.
func New(b Bucket, m *mirror.Mirror, r *content.Renderer, opts Options, log *slog.Logger) *Syncer {
	return &Syncer{
		bucket: b, mirror: m, render: r, opts: opts,
		log:   log.With("component", "sync"),
		rlog:  log.With("component", "render"),
		retry: []time.Duration{5 * time.Second, 30 * time.Second},

		indexNowURL: indexNowURL,
		cache:       map[string]cached{},
		warned:      map[string]string{},
	}
}

// Index returns the current post index, or nil before the first load.
func (s *Syncer) Index() *content.Index { return s.index.Load() }

// Run builds the index from the local mirror, syncs, then polls until ctx ends.
// It sets ready once an index exists. It returns an error only when there is
// no local mirror and the first sync fails: there is nothing to serve.
func (s *Syncer) Run(ctx context.Context, ready *atomic.Bool) error {
	if s.mirror.Existed() {
		s.mu.Lock()
		s.rebuild()
		s.mu.Unlock()
		ready.Store(true)
		s.log.Info("index loaded from mirror", "posts", len(s.Index().Posts()))
	}
	if err := s.syncRetry(ctx); err != nil && ctx.Err() == nil && !ready.Load() {
		return fmt.Errorf("first sync: %w", err)
	}
	if ctx.Err() == nil {
		ready.Store(true)
	}
	t := time.NewTicker(s.opts.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			_ = s.syncRetry(ctx)
		}
	}
}

func (s *Syncer) syncRetry(ctx context.Context) error {
	for i := 0; ; i++ {
		err := s.Sync(ctx)
		if err == nil || ctx.Err() != nil {
			return err
		}
		if i == len(s.retry) {
			s.log.Error("S3 unavailable after retries", "attempts", i+1, "err", err)
			return err
		}
		s.log.Warn("sync failed, will retry", "in", s.retry[i].String(), "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.retry[i]):
		}
	}
}

// Sync lists the bucket, fetches new and changed objects, removes deleted ones
// and rebuilds the index if anything changed. Failed fetches are retried next poll.
func (s *Syncer) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAttempt.Store(time.Now().Unix())
	err := s.sync(ctx)
	if err != nil && ctx.Err() == nil {
		s.errors.Add(1)
	} else if err == nil {
		s.lastSuccess.Store(time.Now().Unix())
	}
	return err
}

func (s *Syncer) sync(ctx context.Context) error {
	objs, truncated, err := s.bucket.List(ctx, s.opts.MaxObjects)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	s.objects.Store(int64(len(objs)))
	if truncated && !s.maxWarned {
		s.log.Warn("bucket has more objects than MAX_OBJECTS, the rest are skipped", "max", s.opts.MaxObjects)
	}
	s.maxWarned = truncated

	warned := map[string]string{}
	warn := func(o Object, msg string, args ...any) {
		if s.warned[o.Key] != o.ETag {
			s.log.Warn(msg, append([]any{"key", o.Key}, args...)...)
		}
		warned[o.Key] = o.ETag
	}
	keep := map[string]bool{}
	var added, updated, removed, failed int
	assets := false // an image changed: posts must re-render for new width/height
	for _, o := range objs {
		if ctx.Err() != nil {
			break
		}
		kind, err := mirror.Classify(o.Key)
		if err != nil {
			warn(o, "object skipped", "err", err)
			continue
		}
		if kind == mirror.Ignore {
			continue
		}
		limit := s.opts.MaxAssetBytes
		if kind == mirror.Post || kind == mirror.Home {
			limit = s.opts.MaxPostBytes
		}
		if o.Size > limit {
			warn(o, "object skipped: too large", "size", o.Size, "max", limit)
			continue
		}
		keep[o.Key] = true
		if s.mirror.Has(o.Key, o.ETag) {
			continue
		}
		_, existed := s.mirror.State()[o.Key]
		if err := s.fetch(ctx, o, limit); err != nil {
			warn(o, "fetch failed, keeping the old copy", "err", err)
			s.fetchFailures.Add(1)
			failed++
			continue
		}
		s.log.Debug("fetched", "key", o.Key, "etag", o.ETag)
		assets = assets || kind == mirror.Asset
		if existed {
			updated++
		} else {
			added++
		}
	}
	s.warned = warned

	if ctx.Err() == nil { // remove only after a complete pass
		for key := range s.mirror.State() {
			if keep[key] {
				continue
			}
			if err := s.mirror.Remove(key); err != nil {
				s.log.Warn("remove failed", "key", key, "err", err)
				continue
			}
			s.log.Debug("removed", "key", key)
			if k, _ := mirror.Classify(key); k == mirror.Asset {
				assets = true
			}
			removed++
		}
	}

	changed := added+updated+removed > 0
	if changed || !s.mirror.Existed() {
		if err := s.mirror.Save(); err != nil {
			return fmt.Errorf("save mirror state: %w", err)
		}
	}
	old := s.index.Load()
	if assets {
		clear(s.cache)
	}
	if changed || old == nil {
		s.rebuild()
	}
	// No ping without an old index: the first load is not a change.
	if changed && old != nil && s.opts.IndexNowKey != "" {
		if urls := s.changedURLs(old, s.Index()); len(urls) > 0 {
			go s.indexNow(urls)
		}
	}
	level := slog.LevelDebug
	if changed || failed > 0 {
		level = slog.LevelInfo
	}
	s.log.Log(ctx, level, "sync done", "added", added, "updated", updated, "removed", removed, "failed", failed, "posts", len(s.Index().Posts()))
	return ctx.Err()
}

func (s *Syncer) fetch(ctx context.Context, o Object, limit int64) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	rc, err := s.bucket.Get(ctx, o.Key, o.ETag)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	return s.mirror.Write(o.Key, o.ETag, rc, limit)
}

// rebuild parses new and changed posts from disk and swaps in a new index. Caller holds mu.
func (s *Syncer) rebuild() {
	next := map[string]cached{}
	var posts []*content.Post
	for key, etag := range s.mirror.State() {
		if k, _ := mirror.Classify(key); k != mirror.Post {
			continue
		}
		c, ok := s.cache[key]
		if !ok || c.etag != etag {
			c = cached{etag: etag, post: s.parse(key)}
		}
		next[key] = c
		if c.post != nil {
			posts = append(posts, c.post)
		}
	}
	s.cache = next
	ix := content.NewIndex(posts, s.rlog)
	ix.Home = s.loadHome()
	legacy := ""
	if _, ok := s.mirror.State()[LogoKey]; ok {
		legacy = LogoKey
	}
	ix.Logo = s.logo.Or(legacy)
	ix.SetTheme(s.theme)
	s.index.Store(ix)
}

// loadHome returns the main page blocks and sets the site theme and logos. It reads and warns only when
// home.md changed. Caller holds mu.
func (s *Syncer) loadHome() []content.Block {
	etag := s.mirror.State()[HomeKey]
	if s.homeRead && etag == s.homeETag {
		return s.home
	}
	s.homeRead, s.homeETag, s.theme, s.logo = true, etag, content.Theme{}, content.Logo{}
	if etag == "" {
		s.rlog.Warn("no home.md, using the default blocks")
		s.home = content.DefaultHome()
		return s.home
	}
	src, err := s.mirror.ReadFile(HomeKey)
	if err != nil {
		s.rlog.Warn("home.md unreadable, using the default blocks", "err", err)
		s.home = content.DefaultHome()
		return s.home
	}
	var problems, themeProblems, logoProblems []string
	s.home, problems = content.ParseHome(src)
	s.theme, themeProblems = content.ParseSiteTheme(src)
	s.logo, logoProblems = content.ParseSiteLogo(src)
	for _, p := range slices.Concat(problems, themeProblems, logoProblems) {
		s.rlog.Warn("home.md: "+p, "key", HomeKey)
	}
	return s.home
}

func (s *Syncer) parse(key string) *content.Post {
	src, err := s.mirror.ReadFile(key)
	if err != nil {
		s.rlog.Warn("post skipped", "key", key, "err", err)
		return nil
	}
	p, err := s.render.Parse(key, src)
	if err != nil {
		s.rlog.Warn("post skipped", "key", key, "err", err)
		return nil
	}
	for _, w := range p.Warnings {
		s.rlog.Warn("post: "+w, "key", key)
	}
	s.rlog.Debug("post rendered", "key", key, "slug", p.Slug, "draft", p.Draft)
	return p
}
