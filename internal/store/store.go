// Package store keeps per-post metrics, votes and search stats in SQLite.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver "sqlite"
)

// FileName is the database file inside DATA_DIR.
const FileName = "hyperhack.db"

// Metric names in the counters table. Labels come from fixed lists or post content.
const (
	View     = "view"     // label ""
	ReadSecs = "read_s"   // label "": seconds, summed per view and clamped first
	ReadView = "read_n"   // label "": views that reported any reading time
	Depth    = "depth"    // label "25" / "50" / "75" / "100"
	Source   = "source"   // label: fixed source list
	Device   = "device"   // label: mobile / tablet / desktop
	Lang     = "lang"     // label: UI language
	Theme    = "theme"    // label: light / dark
	Outbound = "outbound" // label: a host linked from the post
	Copy     = "copy"     // label ""
	VoteCast = "vote"     // label "like" / "dislike": votes cast, for vote trends
)

// Day is the UTC calendar day used for daily rows.
func Day(t time.Time) string { return t.UTC().Format(time.DateOnly) }

const schema = `
CREATE TABLE IF NOT EXISTS counters (
	slug TEXT NOT NULL, metric TEXT NOT NULL, label TEXT NOT NULL, n INTEGER NOT NULL,
	PRIMARY KEY (slug, metric, label)) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS daily_views (
	day TEXT NOT NULL, slug TEXT NOT NULL, n INTEGER NOT NULL,
	PRIMARY KEY (day, slug)) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS votes (
	vid BLOB NOT NULL, slug TEXT NOT NULL, vote INTEGER NOT NULL CHECK (vote IN (-1, 1)),
	PRIMARY KEY (vid, slug)) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS votes_slug ON votes (slug, vote);
CREATE TABLE IF NOT EXISTS searches (
	query TEXT PRIMARY KEY, n INTEGER NOT NULL, zero INTEGER NOT NULL, day TEXT NOT NULL) WITHOUT ROWID;
PRAGMA user_version = 1;
`

// Store is safe for concurrent use.
type Store struct {
	db  *sql.DB
	log *slog.Logger // component store
}

// Open creates or opens dir/hyperhack.db with mode 0600, WAL and a busy timeout.
func Open(dir string, log *slog.Logger) (*Store, error) {
	path := filepath.Join(dir, FileName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	if err := os.Chmod(path, 0o600); err != nil { // also fixes a file created by hand
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; reads are small
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db, log: log.With("component", "store")}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Inc adds N to one counter.
type Inc struct {
	Slug, Metric, Label string
	N                   int64
}

// Add applies counter increments in one transaction. View increments also go to the daily table.
func (s *Store) Add(ctx context.Context, day string, incs ...Inc) error {
	if len(incs) == 0 {
		return nil
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		for _, in := range incs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO counters (slug, metric, label, n) VALUES (?, ?, ?, ?)
				ON CONFLICT (slug, metric, label) DO UPDATE SET n = n + excluded.n`, in.Slug, in.Metric, in.Label, in.N); err != nil {
				return err
			}
			if in.Metric == View {
				if _, err := tx.ExecContext(ctx, `INSERT INTO daily_views (day, slug, n) VALUES (?, ?, ?)
					ON CONFLICT (day, slug) DO UPDATE SET n = n + excluded.n`, day, in.Slug, in.N); err != nil {
					return err
				}
			}
		}
		return nil
	})
	s.log.Debug("counters added", "rows", len(incs), "err", err)
	return s.fail("add counters", err)
}

// Total is one counter value.
type Total struct {
	Slug, Metric, Label string
	N                   int64
}

// Totals returns every counter, for /metrics.
func (s *Store) Totals(ctx context.Context) ([]Total, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT slug, metric, label, n FROM counters ORDER BY slug, metric, label`)
	if err != nil {
		return nil, s.fail("read counters", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Total
	for rows.Next() {
		var t Total
		if err := rows.Scan(&t.Slug, &t.Metric, &t.Label, &t.N); err != nil {
			return nil, s.fail("read counters", err)
		}
		out = append(out, t)
	}
	return out, s.fail("read counters", rows.Err())
}

// Views is the public all-time view count of a post.
func (s *Store) Views(ctx context.Context, slug string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT n FROM counters WHERE slug = ? AND metric = ? AND label = ''`, slug, View).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, s.fail("read views", err)
}

// Viewed is a post with its views in a time window.
type Viewed struct {
	Slug  string
	Views int64
}

// Popular returns posts by views since day (inclusive), most viewed first.
// Ties go to the slug order, so the list is stable.
func (s *Store) Popular(ctx context.Context, since string) ([]Viewed, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT slug, SUM(n) FROM daily_views WHERE day >= ? GROUP BY slug ORDER BY SUM(n) DESC, slug`, since)
	if err != nil {
		return nil, s.fail("read popular", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Viewed
	for rows.Next() {
		var v Viewed
		if err := rows.Scan(&v.Slug, &v.Views); err != nil {
			return nil, s.fail("read popular", err)
		}
		out = append(out, v)
	}
	return out, s.fail("read popular", rows.Err())
}

// Vote values. None removes the vote.
const (
	Dislike = -1
	None    = 0
	Like    = 1
)

// SetVote stores the vote of vid (a hash of the vote cookie) and returns the previous one.
func (s *Store) SetVote(ctx context.Context, vid []byte, slug string, vote int) (prev int, err error) {
	err = s.tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT vote FROM votes WHERE vid = ? AND slug = ?`, vid, slug).Scan(&prev)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if vote == None {
			_, err = tx.ExecContext(ctx, `DELETE FROM votes WHERE vid = ? AND slug = ?`, vid, slug)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO votes (vid, slug, vote) VALUES (?, ?, ?)
				ON CONFLICT (vid, slug) DO UPDATE SET vote = excluded.vote`, vid, slug, vote)
		}
		return err
	})
	return prev, s.fail("set vote", err)
}

// VoteOf returns the vote of vid on slug, or None.
func (s *Store) VoteOf(ctx context.Context, vid []byte, slug string) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx, `SELECT vote FROM votes WHERE vid = ? AND slug = ?`, vid, slug).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return None, nil
	}
	return v, s.fail("read vote", err)
}

// Votes is the like and dislike count of one post.
type Votes struct{ Likes, Dislikes int64 }

// VoteCounts returns the current counts for every voted post.
func (s *Store) VoteCounts(ctx context.Context) (map[string]Votes, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT slug, vote, COUNT(*) FROM votes GROUP BY slug, vote`)
	if err != nil {
		return nil, s.fail("read votes", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]Votes{}
	for rows.Next() {
		var slug string
		var vote int
		var n int64
		if err := rows.Scan(&slug, &vote, &n); err != nil {
			return nil, s.fail("read votes", err)
		}
		v := out[slug]
		if vote == Like {
			v.Likes = n
		} else {
			v.Dislikes = n
		}
		out[slug] = v
	}
	return out, s.fail("read votes", rows.Err())
}

// PostVotes returns the counts for one post.
func (s *Store) PostVotes(ctx context.Context, slug string) (Votes, error) {
	var v Votes
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(vote = 1), 0), COALESCE(SUM(vote = -1), 0) FROM votes WHERE slug = ?`, slug).Scan(&v.Likes, &v.Dislikes)
	return v, s.fail("read votes", err)
}

// LogSearch counts a normalized query; zero marks a query with no results.
func (s *Store) LogSearch(ctx context.Context, day, query string, zero bool) error {
	z := 0
	if zero {
		z = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO searches (query, n, zero, day) VALUES (?, 1, ?, ?)
		ON CONFLICT (query) DO UPDATE SET n = n + 1, zero = zero + excluded.zero, day = excluded.day`, query, z, day)
	return s.fail("log search", err)
}

// QueryCount is one stored search query.
type QueryCount struct {
	Query string
	N     int64
}

// TopSearches returns the n most frequent queries; zero selects zero-result counts.
func (s *Store) TopSearches(ctx context.Context, n int, zero bool) ([]QueryCount, error) {
	col := "n"
	if zero {
		col = "zero"
	}
	// col is one of two constants, never input.
	rows, err := s.db.QueryContext(ctx, `SELECT query, `+col+` FROM searches WHERE `+col+` > 0 ORDER BY `+col+` DESC, query LIMIT ?`, n)
	if err != nil {
		return nil, s.fail("read searches", err)
	}
	defer func() { _ = rows.Close() }()
	var out []QueryCount
	for rows.Next() {
		var q QueryCount
		if err := rows.Scan(&q.Query, &q.N); err != nil {
			return nil, s.fail("read searches", err)
		}
		out = append(out, q)
	}
	return out, s.fail("read searches", rows.Err())
}

// Prune drops search queries last seen before searchDay, keeps at most maxSearches
// (most frequent), and drops daily views before viewDay.
func (s *Store) Prune(ctx context.Context, searchDay, viewDay string, maxSearches int) error {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM searches WHERE day < ?`, searchDay); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM searches WHERE query NOT IN
			(SELECT query FROM searches ORDER BY n DESC, day DESC, query LIMIT ?)`, maxSearches); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM daily_views WHERE day < ?`, viewDay)
		return err
	})
	return s.fail("prune", err)
}

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// fail logs a write or read error once, as ERROR, and wraps it.
func (s *Store) fail(op string, err error) error {
	if err == nil {
		return nil
	}
	s.log.Error("sqlite", "op", op, "err", err)
	return fmt.Errorf("%s: %w", op, err)
}
