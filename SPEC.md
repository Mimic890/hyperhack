# hyperhack — project spec

Russian version: [SPEC.ru.md](SPEC.ru.md). Keep both files in sync.

Name: `hyperhack`.
A lightweight replacement for Ghost. Ghost is too heavy for one author.

## Goal

A light, fast blog for one author.
Posts are Markdown files in S3.
The author writes in Obsidian and publishes by syncing the vault to the bucket.

## Scope

### In scope (MVP)

- Main page.
- Post pages rendered from Markdown.
- Search over posts.
- "Newest" and "Popular" lists.
- Per-post metrics: view count (public), time on page (metric only).
- Estimated reading time on each post.
- Anonymous likes and dislikes.
- RSS feed.
- UI in English and Russian. Default language from `.env`, visitor switch in the top bar.
- `/metrics` endpoint and a Grafana dashboard.
- Structured logging with levels.

### Out of scope

- Multiple users, registration, roles.
- Web editor or admin panel for posts.
- WebDAV. S3 is the only storage backend.
- Site-wide traffic analytics. Reverse proxy logs and Grafana cover it.
- Migration of old Ghost posts.

## Decisions

| Area | Decision |
|---|---|
| Users | One author. No registration. Public side is read-only. |
| Backend | Go 1.27, latest stable (go1.27.3 on 2026-11-03). `go.mod`: `go 1.27` + `toolchain go1.27.3`. Builder image: `golang:1.27`. Check go.dev/dl before starting; Dependabot keeps it current. |
| Frontend | Server-side rendering with `html/template`. No framework. Minimal vanilla JS: votes, ALTCHA, view and time beacon. |
| Themes | Light and dark, toggle in the top bar. System theme by default until the visitor picks one. The choice is stored in a cookie and rendered on the server (no inline JS). The toggle is a form that posts to `/prefs`, so it works without JS. |
| Design | Beautiful, minimal, responsive. Designed during implementation. Fonts: Inter (text) and JetBrains Mono (code), OFL, self-hosted, Latin + Cyrillic subsets. |
| Languages | Code, docs, UI: English. UI also in Russian. Default: `DEFAULT_LANG` (`en` / `ru`) in `.env`. Visitor switch in the top bar; the choice is stored in a cookie (the server must know it to render the page). Post text is not translated. |
| Storage | Garage (S3 API), path-style addressing. Client: `github.com/minio/minio-go/v7`. |
| Post format | One `.md` = one post. Metadata: Obsidian properties (YAML frontmatter). |
| Metrics | Prometheus text format on `/metrics`, scraped by VictoriaMetrics, Grafana dashboard. |
| Logs | `log/slog`, levels DEBUG / INFO / WARN / ERROR. |
| Deployment | Docker + Docker Compose. Image built by GitHub Actions. |
| Repository | Public, GitHub. License AGPL-3.0 (the author adds it when creating the repo). |

## Pages

### Main page

- Top bar, left to right:
  - Logo, then site name.
  - Center: empty.
  - Right: theme toggle, language switch (`en` / `ru`), RSS link, author's GitHub link (URL from `.env`).
- Search.
- Newest posts.
- Popular posts (by view count).

### Post page

- Markdown from S3 rendered to HTML.
- Cover (`cover`), if set.
- Estimated reading time.
- View count.
- Likes and dislikes.

## Configuration

All settings and secrets come from environment variables (`.env`).
The repo has `.env.example` only. Real values never go into the repo.

Proposed variables (final names set during implementation):

| Variable | Purpose |
|---|---|
| `SITE_NAME`, `SITE_URL` | Site name and public URL. |
| `AUTHOR_NAME` | Optional. Author in JSON-LD and RSS. Default: `SITE_NAME`. |
| `DEFAULT_LANG` | UI language and default post language: `en` or `ru`. Default `en`. |
| `GITHUB_URL` | Link in the top bar. |
| `S3_ENDPOINT`, `S3_REGION`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY` | Bucket access. `S3_ENDPOINT` must be `https://`. `S3_REGION` default: `garage`. |
| `POLL_INTERVAL` | Bucket polling interval. Default: `60s`, range 10s–24h. |
| `REINDEX_TOKEN` | Token for `POST /reindex`. |
| `ALTCHA_HMAC_KEY` | ALTCHA secret. |
| `READING_WPM` | Words per minute for estimated reading time. Default: `200`. |
| `LOG_LEVEL`, `LOG_FORMAT` | Log level and format (`json` / `text`). Defaults: `info`, `json`. |
| `DATA_DIR` | Directory for SQLite (`hyperhack.db`). Default: `/data`. |
| `VAULT_DIR` | Directory for the local bucket mirror. Default: `/vault`. |
| `RATE_LIMIT_VOTE`, `RATE_LIMIT_BEACON`, `RATE_LIMIT_REINDEX` | Per-client limits on write endpoints. Defaults: `10/1m`, `30/1m`, `5/1m`. |
| `POPULAR_DAYS`, `POPULAR_LIMIT` | "Popular" window and size. Defaults: `30`, `5`. |
| `PAGE_SIZE` | Posts per page in "Newest" and tag pages. Default: `10`. |
| `RSS_LIMIT` | Posts in the RSS feed. Default: `20`. |
| `DEFAULT_OG_IMAGE` | Optional. Preview image for pages and posts without `cover`: a file inside `assets/`, same rules as `cover`. |
| `INDEXNOW_KEY` | Optional. Enables IndexNow pings. 8–128 characters `a-z A-Z 0-9 -`. Served at `/<INDEXNOW_KEY>.txt`. |
| `RATE_LIMIT_SEARCH` | Search limit per client. Default: `30/1m`. |
| `VISITOR_SECRET` | HMAC key for the daily visitor hash, ≥ 32 bytes. |
| `ALTCHA_EXPIRES`, `ALTCHA_MAX_NUMBER` | Challenge lifetime and difficulty. Defaults: `5m`, `100000`. |
| `METRICS_ALLOW`, `REINDEX_ALLOW` | CIDRs allowed to call `/metrics` and `/reindex`. Default: loopback and private ranges. |
| `MAX_POST_BYTES`, `MAX_ASSET_BYTES`, `MAX_OBJECTS` | Bucket content limits. Defaults: 1 MiB, 50 MiB, 10 000. |
| `TRUSTED_PROXIES` | Proxy IPs/CIDRs allowed to set `X-Forwarded-For`. Without it the client IP is the TCP peer. Needed for correct limits and visitor hashes behind Caddy. |

## Deployment

- The service runs in Docker and starts with Docker Compose.
- GitHub Actions builds the image and pushes it to GHCR (`ghcr.io`).
- `docker-compose.yml` is for production: it only pulls the released image from GHCR, with no `build:`, and pins the latest release. Local builds use `compose.dev.yml` (extends the production service, adds `build: .`, image `hyperhack:dev`), run only with `docker compose -f compose.dev.yml`. No `docker-compose.override.yml`: Compose loads it automatically, so it would reach production.
- Compose uses bind mounts to subfolders (`./data`, …). No named volumes.
- The service port is not published to the internet: localhost, Docker network or mesh IP only. The reverse proxy (Caddy) handles TLS.
- The agent writes `.gitignore` (at least: `.env`, `data/`, build output).
- Domain: from `SITE_URL`.
- The repo has `Caddyfile.example` for this service with domain `example.com`: reverse proxy to the service, `/metrics` and `/reindex` blocked, `/assets/*` served from `vault/assets/` with the headers and allowlist from Security → Perimeter. README shows the same block.

## Bucket layout

- `assets/` — all assets: covers, images in posts. Decided.
- `assets/logo.svg` — optional site logo in the top bar and favicon. Without it the service shows a built-in mark.
- The service ignores keys where any path segment starts with `.` or `_` (same rule as Remotely Save).

### Posts

- Every `.md` file in the vault is a post candidate, at any depth, except `assets/` and ignored paths (`.` / `_`).
- Folders are for the author only. They do not affect the URL.
- URL: `/posts/<slug>`. The slug comes from frontmatter or the file name, so moving a file does not break links.
- No numbering. Order comes from `date`.
- A file without valid frontmatter (`title` + `date`) is skipped with WARN. Templates and helper notes go to `_templates/` or another `_` folder, so they are not synced.

### Local vault mirror and assets

- During sync the service mirrors the bucket to `VAULT_DIR` on disk: downloads new and changed objects, deletes removed ones. The bucket stays the source of truth.
- Mirror state (object key → `ETag`) is in `VAULT_DIR/.hyperhack.json`, mode `0600`, so a restart does not download everything again. Deleting `vault/` forces a full resync.
- Compose layout (bind mounts only, no Docker volumes):
  ```
  hyperhack/
  ├── docker-compose.yml
  ├── .env
  ├── data/    # DATA_DIR: SQLite
  └── vault/   # VAULT_DIR: bucket mirror
  ```
- Caddy (on the host) serves `/assets/*` straight from `vault/assets/` with `file_server`. The service does not serve assets.
- Caddy serves only `/assets/*`. `.md` files and other vault paths are not exposed.
- The Caddy user needs read access to `vault/assets/`.
- Pages and images keep working when S3 is down; only new changes wait for the next successful sync.
- S3 down at startup: the index is built from the mirror and the service is ready. With no mirror yet, the first sync must succeed, else exit 1.

Rejected: Redis. One instance, few posts: in-memory index, Go maps for rate limits, SQLite for counters, disk + OS page cache for files are enough.

## Post format

Metadata: Obsidian properties — a YAML block between `---` at the top of the file.

```markdown
---
title: Leaving Ghost
description: Why Ghost is heavy and what I wrote instead
date: 2026-11-03
updated: 2026-11-05
tags:
  - go
  - selfhost
cover: assets/covers/ghost.webp
slug: leaving-ghost
lang: en
draft: false
---

Post text…
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `title` | string | yes | Post title. |
| `description` | string | no | Short summary: post list, RSS, meta tags, link previews in Telegram and search engines. |
| `date` | date | yes | Publish date. Sort key for "Newest". |
| `updated` | date | no | Last update date. Shown on the post and in RSS. |
| `tags` | list | no | Tags. |
| `cover` | string | no | Cover path inside `assets/`. |
| `slug` | string | no | URL part. Default: from the file name. |
| `lang` | string | no | Post language, ISO 639-1 (`en`, `ru`). Default: `DEFAULT_LANG`. Used in `<html lang>`, RSS, meta tags. |
| `draft` | bool | no | `true` — not published. Default `false`. |

Rules:

- `date` and `updated`: Obsidian "date" (`2026-11-03`) or "date & time" (`2026-11-03T20:59`). The parser accepts both.
- No `title` or `date`, or invalid YAML → post skipped, WARN log.
- Invalid `date`, `updated`, `slug`, `lang` (not two lower-case letters), `cover` (not an allowed file in `assets/`) or `draft` → post skipped, WARN log.
- Slug from the file name: lower case, Russian letters transliterated (`ж` → `zh`, `х` → `kh`, `щ` → `shch`, `ъ` and `ь` dropped), any other character run → `-`. Heading anchors use the same rule.
- `tags`: lower-cased (`Go` → `go`), duplicates merged. Letters of any script, digits, `-`, `_`, `/`; 1–50 characters each; at most 20 per post. Anything else → post skipped, WARN log.
- Two posts with one `slug` → the first by object key wins, the second is skipped, ERROR log.
- Unknown fields are ignored. Obsidian can add its own.
- README describes every field in this table.

## Publishing from Obsidian

- The blog has its own Obsidian vault. Personal notes never reach the blog bucket.
- Obsidian syncs files to Garage with the Remotely Save plugin.
- Vault layout = bucket layout. The Remotely Save remote prefix stays empty: the blog has its own bucket.
- Remotely Save encryption is off for this bucket. Otherwise the service cannot read the files.
- Remotely Save does not sync files and folders whose name starts with `.` or `_`.
- Remotely Save does not call `/reindex`. A new post appears after the next poll.

### Obsidian settings ("Files and links")

| Setting | Value |
|---|---|
| Use [[Wikilinks]] | off |
| New link format | absolute path in vault |
| Default location for new attachments | folder specified below → `assets` |

With these settings an image is inserted as standard Markdown: `![](assets/abstract-5.jpg)`.
The renderer supports standard Markdown links only. Wikilinks (`![[…]]`) are not supported.
A path `assets/…` in a post maps to `/assets/…` on the site.
README describes these settings.

Proposed: if a post contains `![[`, log WARN so the author sees the wrong link.

## Content sync

Garage does not send bucket notifications (no S3 event API).
The service finds changes in two ways:

1. **Polling.** Every `POLL_INTERVAL` (30–60 s), call `ListObjectsV2` on the bucket.
   Compare each object's `ETag` with the cache.
   Fetch and re-render only new or changed objects.
   Remove posts whose objects are gone.
2. **Manual trigger.** `POST /reindex` with a token.
   Called over the private network (blocked on the public domain), so a post appears at once.

Polling is the fallback. The endpoint is the fast path.

- S3 errors: up to 3 attempts per poll (retry after 5 s, then 30 s), WARN per failed attempt, ERROR after the last.
- A failed download keeps the old copy; the next poll retries it.
- `POST /reindex` waits for the sync: `204` done, `401` bad token, `403` address not in `REINDEX_ALLOW`, `503` sync failed.

## Post metrics

Site-wide traffic is already in reverse proxy logs and Grafana. The service does not duplicate it.
The service stores per-post metrics only. Storage: SQLite.

### View count

- Counted by the client script, not on page render. The same script sends time on page.
- Clients without JS (most bots) are not counted. Readers with JS off are not counted either. Accepted.
- Public: shown on the post.
- Feeds the "Popular" list.
- Repeat views from one visitor are not counted: daily visitor hash (see Security → Votes and visitor identity); raw IP is not stored.

### Time on page

- The server cannot measure it alone. It needs the client script.
- The script sends active reading time when the page is hidden or closed (`visibilitychange` / `pagehide`, `fetch` with `keepalive`).
- Only time with the tab visible counts.
- Every delta carries a random per-page-view id. The server sums the deltas of a view and clamps the sum to 4 h, then adds it to the post aggregate: average time = total seconds / views with reading time.
- Private. Not shown on the site. Metric only (see Monitoring).

### Estimated reading time

- Public: shown on the post (e.g. "5 min read").
- Computed from the post text at render time: word count / `READING_WPM`, rounded up, at least 1 minute.
- Proposed: code blocks and images are counted separately.
- Not related to measured time on page.

### Likes and dislikes

- Anonymous. No account.
- One vote per `vid` cookie per post (see Security → Votes and visitor identity). A visitor can change or remove the vote.
- Counts are public on the post.
- Each vote must pass ALTCHA (see Security → ALTCHA).

## Content features

### Code highlighting

- Server-side at render time: `github.com/yuin/goldmark` + `github.com/yuin/goldmark-highlighting/v2` (chroma v2). No JS.
- CSS classes, not inline styles (`chromahtml.WithClasses`), so light and dark themes each get their own palette.

### Search

Goal: the best search possible without an extra service.

- Embedded full-text index: Bleve v2 (`github.com/blevesearch/bleve/v2`), in memory, rebuilt on sync.
- Language-aware analysis by post `lang`: English and Russian analyzers (Russian: Snowball stemmer + stop words, package `analysis/lang/ru`). "Постов" finds "пост".
- Fields and weight: `title` > `tags` > `description` > body.
- Typo tolerance (fuzzy match), prefix match for search-as-you-type.
- Result snippets with highlighted matches.
- Works without JS: `GET /search?q=…` renders results. With JS: results update while typing.
- Filter by tag.
- The results page has `noindex`.

Rejected: Meilisearch and similar. Better typo handling, but a separate service.

### Popular and Newest

- "Popular": most views in the last `POPULAR_DAYS` days, top `POPULAR_LIMIT`.
- "Newest": by `date`, `PAGE_SIZE` per page, with pagination.

### Tags

- Page `/tags/<tag>`: posts with this tag, paginated.
- Each tag has its own color. The color is computed from a hash of the tag name, so it is stable without config.
- Colors are tuned per theme to keep text contrast at WCAG AA.

### RSS

- RSS 2.0 at `/rss.xml`, latest `RSS_LIMIT` posts.
- Full post HTML in `content:encoded`, `description` as summary. All URLs absolute.
- Auto-discovery `<link rel="alternate">` on every page.

### SEO and discoverability

- Server-rendered semantic HTML. Fast pages: no render-blocking JS, image `width`/`height`, `loading="lazy"`, `alt` from Markdown.
- `<title>`, meta description from `description`, canonical URL from `SITE_URL`, `<html lang>` from post `lang`.
- Open Graph and Twitter Card (`summary_large_image`): image from `cover`, else `DEFAULT_OG_IMAGE`.
- JSON-LD `BlogPosting`: headline, dates, author, image, language, keywords.
- `sitemap.xml` with `lastmod` from `updated` or `date`.
- `robots.txt`: allow all crawlers, including AI crawlers; link to the sitemap.
- Raw Markdown for agents: `/posts/<slug>.md`.
- IndexNow ping on new or changed posts, if `INDEXNOW_KEY` is set (Bing, Yandex; Google does not use IndexNow).
- `/llms.txt`: generated, because it is cheap. Real use by AI assistants is near zero today.
- Real `404` status for missing pages.

### Healthcheck

- `GET /healthz`: `200` when the index is loaded, `503` otherwise.
- The image has no curl, so the binary has a `healthcheck` subcommand for the Compose healthcheck.

### Additional post metrics

All private: metrics only, not shown on the site. Collected by the same client script and the server. No tracking cookies, no fingerprinting beyond the daily visitor hash.

| Metric | Source | Purpose |
|---|---|---|
| Views per day per post | script | Trends; base for "Popular". |
| Read depth: share of readers who reach 25 / 50 / 75 / 100 % of the text | script (`IntersectionObserver` on markers) | Do readers finish the post? |
| Traffic source per post: `telegram`, `google`, `yandex`, `bing`, `github`, `direct`, `internal` (from this site), `other` | script (`document.referrer` domain, mapped to a fixed list) | Where readers come from. |
| Outbound link clicks per post (target domain) | script | Which links readers use. |
| Code copies per post | script (`copy` event inside code blocks) | Which code is useful. |
| Device class: `mobile` / `tablet` / `desktop` | script (viewport width bucket) | Layout priorities. |
| UI language and theme chosen | script | Which language and theme readers use. |
| Votes per day per post | server | Vote trends. |
| Search: query count, zero-result count, top queries, top zero-result queries | server | What readers look for and do not find. |

Rules:

- Prometheus labels must have bounded values: post slug, fixed source list, fixed device list. Never raw URLs, referrers or queries as labels.
- Search queries are stored in SQLite (normalized, lower case). `/metrics` exposes only the top `N` as a gauge, refreshed periodically.
- The script sends one beacon request per hide (`visibilitychange` / `pagehide`) with what changed since its last send, tagged with the page-view id. The view is counted once. Rate-limited by `RATE_LIMIT_BEACON`.

## Monitoring

- The service exposes Prometheus text format on `/metrics`, on the main port.
- The reverse proxy blocks `/metrics` on the public domain.
- VictoriaMetrics scrapes the service directly, not through the proxy. No Prometheus server.
- A Grafana dashboard (JSON) is in the repo: `grafana/hyperhack.json`.
- Metrics: all post metrics (see Post metrics); sync status (last poll, errors, object count); search latency; HTTP requests, errors, latency.

## Logging

- Library: `log/slog` from the Go standard library.
- Levels: `DEBUG`, `INFO`, `WARN`, `ERROR`. Set by `LOG_LEVEL`.
- Format: JSON to stdout (for Docker and Loki). Text for local development. Set by `LOG_FORMAT`.
- Fields in every record: `time`, `level`, `msg`, `component` (`main`, `sync`, `http`, `render`, `votes`, `metrics`, `store`). `main`: startup, config and shutdown.
- HTTP records: `request_id`, method, path, status, duration.
- Startup error (config, SQLite; S3 only when there is no local mirror yet) → `ERROR` log and exit code 1.

| Level | What is logged |
|---|---|
| `DEBUG` | Sync details (each object, `ETag` compare), post render, SQLite queries, each HTTP request. |
| `INFO` | Startup and config summary (no secrets), poll result (added / updated / removed), `/reindex` call, shutdown. |
| `WARN` | Post skipped (no `title` or `date`, invalid YAML), rate limit hit, ALTCHA failed, temporary S3 error with retry. |
| `ERROR` | `slug` conflict, S3 unavailable after retries, SQLite write error, recovered panic in a handler. |

Never log: `/reindex` token, S3 keys, ALTCHA secret, raw IP, visitor hash.

Proposed: each HTTP request on `DEBUG` only. The reverse proxy already logs requests.

## Security

### Threat model

| Asset | Threat | Main control |
|---|---|---|
| Pages and readers | XSS, defacement, clickjacking | Escaping, sanitizer, strict CSP, security headers |
| Votes and views | Bots, vote stuffing, CSRF | ALTCHA, rate limits, vote cookie, Origin check |
| Host and service | DoS, resource exhaustion, container escape | Limits, timeouts, hardened container |
| Bucket content | A compromised author device writes bad files | Read-only key, size limits, sanitizer, path checks |
| Secrets | Leak via repo, logs, image | `.env` only, never logged, secret scanning |
| Supply chain | Malicious dependency or action, image tampering | Pinned actions, Dependabot, `govulncheck`, attestations |
| Reader privacy | Tracking, IP leaks | No raw IP, daily HMAC hash, no third-party requests |

Trust boundaries: internet → Caddy → service; bucket → service (semi-trusted: only the author writes, but the author's devices can be compromised); CI → GHCR → host.

### Perimeter (Caddy, outside the service)

- `caddy-ratelimit` + CrowdSec bouncer. Rate limit: 1200 requests per minute per client IP; `/static/*` and `/assets/*` are not counted.
- Blocked on the public domain: `/metrics`, `/reindex`.
- `/assets/*`:
  - Extension allowlist: `png jpg jpeg webp avif gif svg mp4 webm`. Others → `404`.
  - Headers: `X-Content-Type-Options: nosniff` and `Content-Security-Policy: default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox`. An SVG with a script cannot run.
- Request body limit for `/api/*`: 16 KB.
- `Strict-Transport-Security: max-age=31536000` (no `includeSubDomains`, no preload by default).
- `Server` header removed.
- No Cloudflare: it is throttled in Russia since June 2025, and most readers are in Russia. No upstream DDoS protection: accepted risk.
- Rejected: a challenge on site entry (e.g. Anubis). It can block search engines and link-preview bots.

### Endpoints

| Endpoint | Method | Access | Protection |
|---|---|---|---|
| Pages, `/search`, `/tags/*`, `/rss.xml`, `/sitemap.xml`, `/robots.txt`, `/llms.txt`, `/posts/<slug>.md` | GET | public | `RATE_LIMIT_SEARCH` on `/search` |
| `/api/altcha` | GET | public | `RATE_LIMIT_VOTE` |
| `/api/vote` | POST | public | ALTCHA, vote cookie, `RATE_LIMIT_VOTE`, Origin check |
| `/api/beacon` | POST | public | `RATE_LIMIT_BEACON`, Origin check, strict schema |
| `/reindex` | POST | private network | Caddy block, `REINDEX_ALLOW`, `REINDEX_TOKEN` |
| `/metrics` | GET | private network | Caddy block, `METRICS_ALLOW` |
| `/healthz` | GET | public | constant body, no data |
| `/prefs` | POST | public | form body ≤ 4 KB, enum check for `theme` / `lang`, redirect only to a local path |
| `/favicon.ico` | GET | public | built-in SVG mark, or `302` to `/assets/logo.svg` when the bucket has a logo; never `404` |
| `/static/*` | GET | public | embedded CSS, JS and fonts only, no directory listing |
| `/<INDEXNOW_KEY>.txt` | GET | public | only if `INDEXNOW_KEY` is set; body is the key |

Other methods → `405`. Unknown paths → `404`.

### Input validation

- Body size: `http.MaxBytesReader`, 4 KB for `/api/*`.
- JSON: `Content-Type: application/json` required, `DisallowUnknownFields`, one object only.
- Slug must exist in the current index. Unknown slug → `404`, nothing stored. This also bounds metric labels.
- Numbers are clamped: time on page 0–4 h; read depth only 0 / 25 / 50 / 75 / 100.
- Enums only: vote (`like` / `dislike` / `none`), source, device, theme, lang. Other values → `400`.
- Search query: max 200 chars, max 10 terms. The query is built in code (match + fuzzy + prefix), never from Bleve query-string syntax: this blocks regex and wildcard DoS. Fuzziness ≤ 2, prefix min 2 chars. Page number bounded.
- Cookies `lang` and `theme`: enum check; invalid → default.

### Output and XSS

- All HTML through `html/template` (context-aware escaping). Templates parsed at startup.
- Markdown: goldmark with GFM (tables, strikethrough, autolinks), without `WithUnsafe`. By default goldmark does not render raw HTML or dangerous URLs.
- Defense in depth: post HTML passes `bluemonday` (UGC policy + chroma classes + heading ids).
- External links get `rel="noopener noreferrer"`.
- Images only from `assets/`. External images are blocked by CSP (also keeps reader IPs private).
- JSON-LD from `json.Marshal` (escapes `<`, `>`, `&`).
- RSS and sitemap via `encoding/xml`.
- `/posts/<slug>.md`: `Content-Type: text/markdown; charset=utf-8`, `nosniff`.
- Error pages show no internals.

### Security headers (set by the service)

- `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; worker-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; upgrade-insecure-requests`. Add `blob:` to `worker-src` only if the ALTCHA build needs it.
- No inline scripts or styles. Theme and language come from cookies and are rendered on the server, so no inline script is needed against a theme flash.
- `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, `Cross-Origin-Opener-Policy: same-origin`, `X-Frame-Options: DENY`, `Permissions-Policy: camera=(), microphone=(), geolocation=(), browsing-topics=()`.
- All JS, CSS, fonts and the ALTCHA solver are self-hosted. No CDN, no third-party requests.

### CSRF and CORS

- No CORS headers.
- `/api/*` requires `Content-Type: application/json` (a cross-site form cannot send it without a preflight) and same-origin `Origin` / `Sec-Fetch-Site`.

### Votes and visitor identity

- Vote identity: anonymous random cookie `vid` (128 bit, `HttpOnly; Secure; SameSite=Lax; Max-Age=1 year`). No personal data in it.
- One vote per `vid` per post. Clearing the cookie allows a new vote, but each vote costs an ALTCHA solution and hits `RATE_LIMIT_VOTE`.
- Why not an IP hash: many readers share one IP (mobile CGNAT), and a daily hash would allow one vote per day.
- View dedup: `HMAC-SHA256(daily_key, ip_prefix + user_agent)`, `daily_key = HMAC(VISITOR_SECRET, date)`. `ip_prefix`: the IPv4 address or the IPv6 `/64`. Today's hashes live in memory only; old daily keys are not stored, so days cannot be linked. Raw IPs are never stored or logged.

### ALTCHA

- Library: `github.com/altcha-org/altcha-lib-go/v2` (v2 challenges). Invisible: the post script solves the challenge on click with its own small solver (`static/altcha.js`), no widget.
- `ALTCHA_HMAC_KEY` ≥ 32 random bytes.
- Challenge expiry `ALTCHA_EXPIRES` (default 5 min). Expired → reject.
- Replay protection in the service: used challenges stay in memory until expiry; reuse → reject. The library does not track this.
- Difficulty: `ALTCHA_MAX_NUMBER`.

### Rate limiting (service)

- Key: client IP via `TRUSTED_PROXIES`; IPv6 grouped by `/64`.
- Token bucket in memory with a hard cap on tracked clients (oldest evicted), so a flood of IPs cannot exhaust RAM.
- Limits: `RATE_LIMIT_VOTE`, `RATE_LIMIT_BEACON`, `RATE_LIMIT_SEARCH`, `RATE_LIMIT_REINDEX`. Over limit → `429` + `Retry-After`, WARN log.

### Private endpoints

- `/metrics` and `/reindex`: blocked in Caddy, and accepted only from `METRICS_ALLOW` / `REINDEX_ALLOW` CIDRs (default: loopback and private ranges). Two independent layers.
- `REINDEX_TOKEN` ≥ 32 bytes, sent as `Authorization: Bearer`, compared with `crypto/subtle.ConstantTimeCompare`.
- Only one reindex runs at a time. Extra calls wait for the running one.

### Bucket and local mirror

- The service has its own Garage key with **read** permission only. Remotely Save uses a separate key with read + write.
- S3 over HTTPS with certificate verification.
- Size limits: `MAX_POST_BYTES` (default 1 MiB), `MAX_ASSET_BYTES` (50 MiB), `MAX_OBJECTS` (10 000). Over limit → skip + WARN. `MAX_OBJECTS` counts every key in the bucket, ignored ones too.
- Mirror writes go through `os.Root` (Go ≥ 1.24): no escape via `..` or symlinks. Keys with an absolute path, `\`, NUL or control characters are rejected.
- Assets: same extension allowlist as Caddy.
- Atomic writes (temp file + rename), so Caddy never serves a partial file.
- Modes: dirs `0755`, files `0644` (Caddy needs read). SQLite file `0600`.
- Frontmatter size is checked before YAML parsing. `cover` must resolve inside `assets/`.
- Slug: `^[a-z0-9-]{1,100}$`. A file name with other characters (e.g. Cyrillic) is transliterated. If the result is empty and there is no `slug` → skip + WARN.

### HTTP server

- `ReadHeaderTimeout` 5 s, `ReadTimeout` 10 s, `WriteTimeout` 30 s, `IdleTimeout` 120 s, `MaxHeaderBytes` 16 KB.
- Context timeouts on S3 calls.
- Panic recovery middleware → `500` + ERROR log.
- Graceful shutdown on SIGTERM.
- No pprof or debug endpoints in release builds.

### Data

- SQLite: parameterized queries only; WAL; `busy_timeout`.
- Bounded growth: per-day aggregates, not raw events; search query table capped, with TTL.
- `data/` is backed up by the host (outside the service).

### Container

- Static binary (`CGO_ENABLED=0`, `-trimpath`), pure-Go SQLite driver `modernc.org/sqlite`.
- Runtime image: distroless static non-root, or `scratch`. No shell, no package manager.
- Compose example:
  ```yaml
  services:
    hyperhack:
      image: ghcr.io/<owner>/hyperhack:<version>
      user: "10001:10001"
      read_only: true
      cap_drop: [ALL]
      security_opt: [no-new-privileges:true]
      tmpfs: [/tmp]
      ports: ["127.0.0.1:8080:8080"]
      volumes:
        - ./data:/data
        - ./vault:/vault
      env_file: .env
      mem_limit: 256m
      pids_limit: 128
      logging:
        options: { max-size: "10m", max-file: "3" }
      healthcheck:
        test: ["CMD", "/hyperhack", "healthcheck"]
      restart: unless-stopped
  ```
- `.env` mode `0600`.

### Secrets

- Only in `.env`. `.env.example` has placeholders.
- Startup check: required secrets present and long enough, else exit 1.
- Never logged, never in error messages, never in the image.
- README documents rotation: `REINDEX_TOKEN`, `ALTCHA_HMAC_KEY`, `VISITOR_SECRET`, S3 keys.

### Supply chain and CI

- GitHub Actions pinned to a full commit SHA.
- Minimal `permissions:` per job: `contents: read` by default; `packages: write`, `id-token: write`, `attestations: write` only in the release job.
- No `pull_request_target` with checkout of PR code. Fork PRs get no secrets.
- Dependabot: `gomod`, `github-actions`, `docker`.
- CI checks: `go vet`, `staticcheck`, `govulncheck`, tests with `-race`, CodeQL (Go).
- Release: build image, push to GHCR, attest with `actions/attest` (`push-to-registry: true`). Verify on the host: `gh attestation verify oci://ghcr.io/<owner>/hyperhack:<version> -R <owner>/hyperhack`.
- Image tags: semver without the `v` (git tag `v0.1.0` → image `0.1.0`), never overwritten. Compose pins a version, not `latest`. After a release, a PR moves the tag in `docker-compose.yml` to the new version.
- Repo settings: branch protection on `main` (PR and checks required, no force push), secret scanning + push protection, private vulnerability reporting. `SECURITY.md` explains how to report.

### Security tests

- XSS payload corpus through the Markdown → HTML pipeline.
- Path traversal keys for the mirror.
- Go fuzz tests: frontmatter parser, Markdown render, beacon decoder.
- Rate limiter, ALTCHA replay, Origin check, private endpoint CIDR checks.

## Proposed (not confirmed)

## Open questions


## Development rules

- Everything in English: code, comments, commits, docs, UI.
- Docs have a Russian copy: `README.md` + `README.ru.md`, `SPEC.md` + `SPEC.ru.md`. Every doc change updates both.
- Commits: Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`, `chore:`).
- Never push to `main`. Branches (`feat/…`, `fix/…`, `refactor/…`, `dev`) and PRs.
- Never delete branches or commits. Revert instead. No rebase or force-push on published branches.
- No author-specific values in code or docs (domains, bucket names, keys). They go to `.env`.
- Confirm the path for service data before using it.
- Every decision change goes into this file and the Decision log.

## Decision log

- 2026-11-03 — One author, no registration.
- 2026-11-03 — S3 (Garage) only; WebDAV dropped.
- 2026-11-03 — Sync: `ListObjectsV2` polling + `POST /reindex` with token.
- 2026-11-03 — No site-wide analytics in the service. Per-post metrics only.
- 2026-11-03 — Anonymous likes and dislikes with a bot check.
- 2026-11-03 — Security: Caddy rate limit + CrowdSec, service write limits, ALTCHA on vote. No upstream DDoS protection. No challenge on entry.
- 2026-11-03 — Frontend: `html/template`, no framework. Light and dark themes.
- 2026-11-03 — Top bar: logo + name left, empty center, theme + RSS + GitHub right. RSS in MVP.
- 2026-11-03 — Views counted by the client script.
- 2026-11-03 — Public estimated reading time. Measured time on page is metric only. `/metrics` for VictoriaMetrics + Grafana dashboard.
- 2026-11-03 — `/metrics` on the main port, blocked in the reverse proxy.
- 2026-11-03 — Logs: `log/slog`, DEBUG / INFO / WARN / ERROR, JSON to stdout.
- 2026-11-03 — Post format: Obsidian properties `title`, `description`, `date`, `updated`, `tags`, `cover`, `slug`, `lang`, `draft`. Assets in `assets/`.
- 2026-11-03 — Separate Obsidian vault for the blog, synced by Remotely Save, no encryption.
- 2026-11-03 — Images: standard Markdown links; Obsidian wikilinks off, absolute paths, attachments in `assets`.
- 2026-11-03 — Project name: `hyperhack`.
- 2026-11-03 — Docker + Compose. Settings and secrets in `.env`.
- 2026-11-03 — Open source on GitHub, AGPL-3.0, GitHub Actions. Everything in English, Russian copy of docs, Russian UI via `DEFAULT_LANG`.
- 2026-11-03 — `DATA_DIR`, domain (`SITE_URL`) and rate limits in `.env`. `Caddyfile.example` with `example.com`. Visitor language switch next to the theme toggle. Ghost migration out of scope.
- 2026-11-03 — Posts: any `.md` at any depth except `assets/`, `.` and `_`; URL `/posts/<slug>`. Service mirrors the bucket to `./vault`; Caddy serves `/assets/*` from it. No Redis.
- 2026-11-03 — Server-side code highlighting (chroma). Bleve search (ru/en, fuzzy, prefix, boosts). Popular over `POPULAR_DAYS`. Pagination. Tag pages with hash colors. Full-text RSS. Full SEO set. `/healthz`. Limits and sizes in `.env`.
- 2026-11-03 — Extra private post metrics: daily views, read depth, traffic source, outbound clicks, code copies, device class, language/theme, daily votes, search stats.
- 2026-11-03 — Everything is one Go binary. External: Garage, Caddy, VictoriaMetrics, Grafana (already running).
- 2026-11-03 — Full security design (see Security). Votes keyed by anonymous `vid` cookie, not IP. `/reindex` private like `/metrics`. Theme in a cookie, no inline JS. Images only from `assets/`.
- 2026-11-03 — Go 1.27 (go1.27.3).
- 2026-11-03 — Config defaults: `POLL_INTERVAL` 60s, `READING_WPM` 200, `LOG_LEVEL` info, `LOG_FORMAT` json, `DATA_DIR` /data, `VAULT_DIR` /vault, `ALTCHA_MAX_NUMBER` 100000, `S3_REGION` garage. Log component `main`. The service listens on fixed port 8080.
- 2026-11-03 — Sync: S3 client `minio-go/v7`, frontmatter via `go.yaml.in/yaml/v3`, GFM. Mirror state in `VAULT_DIR/.hyperhack.json`. S3 down at startup: serve from the mirror; exit 1 only with no mirror. No Remotely Save prefix. Keys with a `.` or `_` segment are ignored (was proposed). `MAX_OBJECTS` counts all keys. An invalid frontmatter field skips the post. 3 S3 attempts per poll. `/reindex` waits for the sync and returns `204`.
- 2026-11-04 — Pages: theme and language switch via a no-JS form `POST /prefs`; cookies `theme` / `lang` (`HttpOnly; SameSite=Lax; Max-Age=1 year`, `Secure` on https). Logo from `assets/logo.svg`, built-in mark otherwise. Fonts Inter + JetBrains Mono, self-hosted. Pagination `/?page=N`: page 1 is `/`, other out-of-range values → `404`. `<html lang>` from the post; the top bar keeps the UI language. CSS and fonts at `/static/*`.
- 2026-11-04 — Search, tags, RSS, SEO: Bleve `v2.6.1`, in-memory index, one document mapping per language (`en`, `ru`). Fuzziness by word length: ≤ 3 letters 0, ≤ 6 letters 1, longer 2; prefix on the last word. Snippets keep only `<mark>`. Search works without JS; `search.js` swaps the results while typing (debounced). Results page `noindex`. Tags lower case, Obsidian characters, `/tags/<tag>` paginated like the main page. 24 tag hue buckets (FNV-1a hash), contrast checked in tests. RSS: `dc:creator` from `AUTHOR_NAME`, `atom:updated` from `updated`. Image `width`/`height` read from the mirror (`golang.org/x/image` for WebP); an asset change re-renders all posts. IndexNow: one ping per sync with new, changed and removed post URLs, not on the first index load. New `AUTHOR_NAME`; `DEFAULT_OG_IMAGE` is a path inside `assets/`.
- 2026-11-04 — Metrics, votes, limits: SQLite `DATA_DIR/hyperhack.db` (`modernc.org/sqlite` `v1.61.0`, `0600`, WAL). All-time counters per post and label (Grafana gets daily trends with `increase()`), daily views only for "Popular" (pruned after `POPULAR_DAYS`). Search queries: live requests from `search.js` (`X-Search: live`) are not stored, the settled query is; TTL 90 days, at most 10 000 rows, top 20 in `/metrics`, refreshed every 5 min. ALTCHA: `altcha-lib-go/v2` `v2.4.1`, v2 challenge with SHA-256, cost 1, `uint32` counter up to `ALTCHA_MAX_NUMBER`, key signature; own small solver `static/altcha.js` instead of the widget; signing keys derived from `ALTCHA_HMAC_KEY`; replay map capped at 100 000. Beacon: a delta on every hide and `pagehide`, sent with `fetch(…, {keepalive: true})` (JSON body), each with a random per-page-view id; the server sums time per view and clamps the sum to 4 h; time on page = average over views with reading time. Views and read depth: once per visitor per post per day; daily hashes in memory only (cap 200 000), `ip_prefix` = IPv4 address or IPv6 `/64`. Source `internal` for referrers from the site itself. Outbound hosts limited to links in the post. `/api/altcha` and `/api/vote` share `RATE_LIMIT_VOTE`; limiter tracks at most 10 000 clients. Vote cookie stored as SHA-256. "Popular" after "Newest" on page 1. `/metrics` in Prometheus text format written with the standard library.
- 2026-11-04 — Grafana, Caddy, README: dashboard `grafana/hyperhack.json` for a Prometheus-type data source on VictoriaMetrics (uses the VictoriaMetrics function `sort_by_label_numeric`); daily bars cover the day that starts at the bar, so today is shown. `Caddyfile.example` for Caddy `v2.11.7` with `caddy-ratelimit` `v0.1.0` (1200 requests per minute per client IP; `/static/*` and `/assets/*` are not counted) and `caddy-crowdsec-bouncer` `v0.14.1`; `/metrics` and `/reindex` (and their subpaths) answer `404` on the public domain; compression `zstd`/`gzip`. README: behind Caddy on the host, `TRUSTED_PROXIES` is the gateway of the Compose network. Full README in English and Russian.
- 2026-11-04 — Production Compose: image registry GHCR (confirmed). `docker-compose.yml` only pulls the released image, no `build:`, pinned to the latest release (`0.1.0`; image tags are the git tag without `v`). Local builds in `compose.dev.yml`, used only with `-f`; no `docker-compose.override.yml`. README Quick start uses the downloaded `docker-compose.yml` as is, and README documents upgrades and rollback.
- 2026-11-04 — Favicon: every page has `<link rel="icon" type="image/svg+xml">` pointing to `/assets/logo.svg` when the bucket has it, else to `/favicon.ico`. `GET /favicon.ico` serves the built-in mark as SVG (accent background, white mark, fixed colors), or answers `302` to `/assets/logo.svg` when the logo exists; `Cache-Control: max-age=3600`, since the logo can change with any sync. Never `404`.
