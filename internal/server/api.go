package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/altcha-org/altcha-lib-go/v2"

	"github.com/Mimic890/hyperhack/internal/store"
)

var vidRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

var voteValues = map[string]int{"like": store.Like, "dislike": store.Dislike, "none": store.None}

func voteName(v int) string {
	for name, n := range voteValues {
		if n == v {
			return name
		}
	}
	return "none"
}

// sameOrigin is the CSRF check for /api/*: a cross-site Sec-Fetch-Site is refused,
// and a POST must carry the site's own Origin.
func (p *pages) sameOrigin(r *http.Request) bool {
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" && s != "same-origin" {
		return false
	}
	return r.Method == http.MethodGet || r.Header.Get("Origin") == p.origin
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}

// readVID returns the vote cookie if it is well formed.
func readVID(r *http.Request) string {
	if c, err := r.Cookie("vid"); err == nil && vidRe.MatchString(c.Value) {
		return c.Value
	}
	return ""
}

// vidHash is what the store keeps, so a database copy does not hold usable cookies.
func vidHash(vid string) []byte {
	h := sha256.Sum256([]byte(vid))
	return h[:]
}

// ensureVID returns the vote cookie, setting a new random one if missing.
func (p *pages) ensureVID(w http.ResponseWriter, r *http.Request) string {
	if vid := readVID(r); vid != "" {
		return vid
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	vid := hex.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{
		Name: "vid", Value: vid, Path: "/", MaxAge: cookieMaxAge,
		HttpOnly: true, Secure: p.cfg.SiteURL.Scheme == "https", SameSite: http.SameSiteLaxMode,
	})
	return vid
}

// apiAltcha handles GET /api/altcha: a fresh signed challenge.
func (p *pages) apiAltcha(w http.ResponseWriter, r *http.Request) {
	if p.limit(p.lim.vote, "altcha")(w, r) {
		return
	}
	if !p.sameOrigin(r) {
		p.vlog.Warn("origin check failed", "endpoint", "altcha")
		apiError(w, http.StatusForbidden)
		return
	}
	ch, err := p.altcha.challenge()
	if err != nil {
		p.vlog.Error("altcha challenge", "err", err)
		apiError(w, http.StatusInternalServerError)
		return
	}
	writeJSON(w, ch)
}

type voteRequest struct {
	Slug   string         `json:"slug"`
	Vote   string         `json:"vote"`
	Altcha altcha.Payload `json:"altcha"`
}

type voteResponse struct {
	Likes    int64  `json:"likes"`
	Dislikes int64  `json:"dislikes"`
	Vote     string `json:"vote"`
}

// apiVote handles POST /api/vote: one vote per vid cookie per post, each behind ALTCHA.
func (p *pages) apiVote(w http.ResponseWriter, r *http.Request) {
	if p.limit(p.lim.vote, "vote")(w, r) {
		return
	}
	if !p.sameOrigin(r) {
		p.vlog.Warn("origin check failed", "endpoint", "vote")
		apiError(w, http.StatusForbidden)
		return
	}
	var req voteRequest
	if status, err := decodeJSON(w, r, &req); err != nil {
		apiError(w, status)
		return
	}
	vote, ok := voteValues[req.Vote]
	if !ok {
		apiError(w, http.StatusBadRequest)
		return
	}
	ix := p.index()
	if ix == nil {
		apiError(w, http.StatusServiceUnavailable)
		return
	}
	post := ix.Get(req.Slug)
	if post == nil {
		apiError(w, http.StatusNotFound)
		return
	}
	if err := p.altcha.verify(req.Altcha); err != nil {
		p.vlog.Warn("altcha failed", "reason", err)
		apiError(w, http.StatusForbidden)
		return
	}
	ctx := r.Context()
	prev, err := p.store.SetVote(ctx, vidHash(p.ensureVID(w, r)), post.Slug, vote)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable)
		return
	}
	if vote != store.None && vote != prev {
		_ = p.store.Add(ctx, store.Day(p.visitors.now()), store.Inc{Slug: post.Slug, Metric: store.VoteCast, Label: req.Vote, N: 1})
	}
	counts, err := p.store.PostVotes(ctx, post.Slug)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable)
		return
	}
	p.vlog.Debug("vote", "slug", post.Slug, "vote", req.Vote, "was", voteName(prev))
	writeJSON(w, voteResponse{Likes: counts.Likes, Dislikes: counts.Dislikes, Vote: req.Vote})
}

// apiBeacon handles POST /api/beacon: one delta of a page view's metrics.
func (p *pages) apiBeacon(w http.ResponseWriter, r *http.Request) {
	if p.limit(p.lim.beacon, "beacon")(w, r) {
		return
	}
	if !p.sameOrigin(r) {
		p.mlog.Warn("origin check failed", "endpoint", "beacon")
		apiError(w, http.StatusForbidden)
		return
	}
	var b beacon
	if status, err := decodeJSON(w, r, &b); err != nil {
		apiError(w, status)
		return
	}
	if b.validate() != nil {
		apiError(w, http.StatusBadRequest)
		return
	}
	ix := p.index()
	if ix == nil {
		apiError(w, http.StatusServiceUnavailable)
		return
	}
	post := ix.Get(b.Slug)
	if post == nil {
		apiError(w, http.StatusNotFound) // nothing stored for unknown slugs
		return
	}
	day, incs := p.visitors.apply(&b, post, clientIP(r, p.cfg.TrustedProxies), r.UserAgent())
	if err := p.store.Add(r.Context(), day, incs...); err != nil {
		apiError(w, http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
