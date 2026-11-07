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
| Frontend | Server-side rendering with `html/template`. No framework. Minimal vanilla JS: votes, ALTCHA, view and time beacon, search palette, table of contents, copy and share buttons, post transitions, Mermaid diagrams (Pele). |
| Themes | Light and dark, toggle in the top bar. System theme by default until the visitor picks one. The choice is stored in a cookie and rendered on the server (no inline JS). The toggle is a form that posts to `/prefs`, so it works without JS. |
| Design | See Design. Fonts: Onest (text) and JetBrains Mono (code and heading markers), OFL, self-hosted, Latin + Cyrillic subsets. |
| Languages | Code, docs, UI: English. UI also in Russian. Default: `DEFAULT_LANG` (`en` / `ru`) in `.env`. Visitor dropdown in the top bar; the choice is stored in a cookie (the server must know it to render the page). Post text is not translated. |
| Storage | Garage (S3 API), path-style addressing. Client: `github.com/minio/minio-go/v7`. |
| Post format | One `.md` = one post. Metadata: Obsidian properties (YAML frontmatter). |
| Metrics | Prometheus text format on `/metrics`, scraped by VictoriaMetrics, Grafana dashboard. |
| Logs | `log/slog`, levels DEBUG / INFO / WARN / ERROR. |
| Deployment | Docker + Docker Compose. Image built by GitHub Actions. |
| Repository | Public, GitHub. License AGPL-3.0 (the author adds it when creating the repo). |

## Design

Applies to every page. Pages work without JS; JS only improves them.

### Colors

CSS custom properties. The theme mechanism does not change: `prefers-color-scheme`, cookie, `data-theme`.

| Token | Dark | Light | Role |
|---|---|---|---|
| `--bg` | `#0A0A0A` | `#FFFFFF` | page background |
| `--raise` | `#171717` | `#F5F5F5` | cards, code, menus |
| `--line` | `#262626` | `#E5E5E5` | borders, dividers |
| `--text` | `#EDEDED` | `#171717` | main text |
| `--muted` | `#A3A3A3` | `#5C5C5C` | metadata, captions |
| `--uv` | `#3B82F6` | `#2563EB` | accent: links, `#`, active states |
| `--ember` | `#3B82F6` | `#2563EB` | second accent: "New post", gradient end; the same blue by default |
| `--uv-soft` | `rgba(59,130,246,.14)` | `rgba(37,99,235,.10)` | background of active elements |

- The `--uv` → `--ember` gradient appears only in the reading progress bar.
- Text contrast is at least 4.5:1, checked in a test.
- Text on `--uv-soft` is always `--text`. There `--uv` is only for icons and borders.
- `--ember` text goes only on `--raise` or `--bg`, the pairs the contrast test checks.

#### Own colors

The owner can change every token, for each theme: `theme` in `home.md` for the whole site, `theme` in a post's frontmatter for that post's page.

```yaml
theme:
  light:
    background: "#F6F5FB"
    text: "#1B1930"
    accent: "#5B3DE6"
  dark:
    background: "#101119"
    accent-soft: "#8B6CFF24"
```

- Names: `background` (`--bg`), `surface` (`--raise`), `border` (`--line`), `text` (`--text`), `muted` (`--muted`), `accent` (`--uv`), `accent-2` (`--ember`), `accent-soft` (`--uv-soft`).
- Value: a hex color `#rgb`, `#rgba`, `#rrggbb` or `#rrggbbaa`; the last two digits are opacity, as `accent-soft` needs. The `#` may be left out. A color with `#` goes in quotes: in YAML an unquoted `#` starts a comment, and the service warns about that case.
- Any color may be left out, and an empty value is no color. A post color wins over the `home.md` one, which wins over the built-in one.
- An unknown name, a bad value or a key other than `light` / `dark` → only that entry is ignored, WARN; a post stays published. Nothing but a validated hex color reaches the CSS.
- The service builds one stylesheet per color set, named by its content hash, and links it after `style.css`: `/static/theme/<hash>.css`. No inline styles, so the CSP stays as it is.
- Contrast of own colors is the owner's choice and is not checked. Code highlighting keeps its palette.

### Typography

- Text: Onest (OFL, Latin + Cyrillic), variable `woff2`, normal style, self-hosted in `static/fonts/` with `OFL-Onest.txt`.
- JetBrains Mono only for code and heading markers.
- Body 17px / 1.65, post text 18px / 1.72.
- Post title 54px / 1.05 / 800, letter-spacing −0.04em; 34px at ≤ 640px.
- Post h2 29px / 750, h3 22px / 700.
- No all-caps captions. Metadata is separated with `gap`, not with `·`.

### Shape and size

- Radius: cards 20px, code and callouts 14px, buttons 12px, chips `999px`.
- Minimum touch target 44px.
- Container `max-width: 1180px`, side padding 32px (16px at ≤ 640px).

### Motion

Everything is inside `@media (prefers-reduced-motion: no-preference)`. New CSS features go through `@supports`; without them the page still looks right.

1. List → post: `@view-transition { navigation: auto; }` on all pages. The title and the cover share a `view-transition-name` from the slug between the list and the post, 280ms. CSP forbids inline styles, so a small script sets the names through the CSSOM on `pageswap` and `pagereveal`. It is blocking in `<head>`, with `<link rel="expect" href="#main" blocking="render">`, because `pagereveal` fires before the first frame. Elements outside the window get no name. Without JS or these events there is a normal page transition.
2. Reading progress on post pages: a 2px bar at the bottom of the top bar, gradient `--uv` → `--ember`, `animation-timeline: scroll(root)`.
3. Search palette: backdrop 120ms, window from `scale(.98)` and `opacity: 0` in 160ms.
4. Like: `scale(.94)` on press, the number moves from bottom to top when it changes.
5. Language menu: appears in 120ms.
6. Post title in the top bar: fades in and out in 160ms.
7. Side table of contents: one `--uv` bar slides to the active item in 320ms. The leading edge moves first and the trailing one 90ms later, so the bar stretches on the way and settles to the item's height. `post.js` sets `top` and `bottom` through the CSSOM. With reduced motion it jumps.

No other animations on load or on card hover.

### Responsive layout and accessibility

- Checked at widths 375, 768, 1280 and 1440.
- Visible `:focus-visible` on all interactive elements.
- Icons without text have `aria-label`.
- `aria-current`, `aria-expanded` and `aria-pressed` wherever there is state.

## Pages

### Top bar

- On all pages. Height 60px, `position: sticky`, background `--bg` at 82% with `backdrop-filter: blur(12px)`, bottom border `--line`.
- Left: logo in a 47×34px box (`object-fit: contain`), then site name 21px / 750. At ≤ 640px the name is hidden and stays in the link's `aria-label`, so the bar fits one row.
- Logos come from `logo` in `home.md`: one path for all, or a map `light` / `dark` (top bar, per effective theme) and `favicon`. Each is an image inside `assets/` (svg, png, jpg, jpeg, webp, avif, gif); a leading `/` is dropped. A bad value or unknown key → only that entry is ignored, WARN. Fallbacks: a missing top bar logo takes the other one; the favicon takes `light`, then `dark`; then `assets/logo.svg` if the bucket has it; then the built-in mark. With two top bar logos both `<img>` are in the page and CSS shows the one of the current theme, like the theme buttons.
- On post pages at ≥ 1000px: the post title in the middle of the bar, 13px `--muted`, centered, at most two lines (`line-clamp: 2`), up to 36rem wide. It appears (160 ms fade) only after the page title has scrolled under the bar, so the two titles are never seen together; `post.js` watches the `h1` with an `IntersectionObserver`. Without JS it stays hidden. Below 1000px the bar has no title.
- Right, left to right: search button, theme toggle, RSS link, Telegram, X and GitHub links, language dropdown (`en` / `ru`) last.
- Social links come from `telegram`, `x` and `github` in `home.md`. A missing or empty field → no button. Each value is an absolute `https` URL with a host and no credentials, at most 500 characters; anything else is ignored with WARN. The buttons have `rel="noopener noreferrer me"`. Without any of the three in `home.md`, GitHub falls back to `GITHUB_URL`. At ≤ 30rem the icon buttons narrow from 44px to 36px, so the bar with all links still fits 375px.
- Search button: on the main page a field-like button "Search posts" with the hint `Ctrl K`, width ≥ 220px, height 36px. On other pages and at ≤ 640px: icon only. Without JS it is a link to `/search`.
- Language dropdown: a borderless button `RU ▾`, menu on `--raise`, radius 14px, shadow, active language in `--uv`.

### Main page

Built from blocks listed in `home.md`, a file in the vault root. The name is reserved: this file is not a post and stays out of the index, search, RSS, sitemap, `/llms.txt` and `/posts/<slug>.md`. A `home.md` in a subfolder is an ordinary post candidate. The name has no `_` because Remotely Save does not sync such files.

The file holds only YAML frontmatter with a `blocks` list and optional `theme` (see Design → Colors → Own colors), `logo`, `github`, `x` and `telegram` (see Top bar); its body is ignored. Blocks appear from top to bottom in file order. The file is read again on sync, like posts.

```yaml
---
blocks:
  - type: intro
    title: Long posts about self-hosting and Go.
    text: Notes from running my own infrastructure.
  - type: tags
    limit: 6
  - type: featured
  - type: posts
    group: year
  - type: popular
    limit: 4
    area: side
---
```

| `type` | Fields | Shows |
|---|---|---|
| `intro` | `title`, `text` (both optional) | Heading 46px / 800 and `--muted` subtitle, width ≤ 720px. Without both fields it renders nothing. |
| `tags` | `limit` (1–20, default 6) | Chip "All posts" and the most used tags: by number of published posts, ties by name. Each chip: `#` in `--uv`, name, post count in `--muted`. |
| `featured` | none | The latest post. Card on `--raise` with a `--line` border, radius 20px, padding 20px. Left: cover at 1/4 width (`4/3`, radius 14px). Right: "New post" in `--ember`, title 30px, description, date, reading time, tags. No cover: text at full width. |
| `posts` | `group: year \| none` (default `none`), `limit` (1–100, default `PAGE_SIZE`) | Post list with pagination. Row: date on the left (92px), title 21px / 650 with `--muted` description, reading time on the right. With `year`: year headings 40px / 800 with the year's post count in this list as words ("5 posts"; Russian "1 пост / 2 поста / 5 постов"), and dates without the year. |
| `popular` | `limit` (1–10, default 4) | Numbered list (1, 2, 3…). Under each title: an eye icon with views in the `POPULAR_DAYS` window, then like and dislike icons with the current vote counts, icons at text size, a wider gap between views and votes. Each icon has a visually hidden label. No RSS link: the top bar has one. |

- `area: main | side` on any block, default `main`. `side` is a right column of 300px at ≥ 860px; below that, side blocks go after the main ones.
- Text fields are a string or a map by language: `title: {ru: "…", en: "…"}`. Language keys are two lower-case letters. No entry for the UI language: the first one in the file.
- Plain text only, escaped by the template. `title` ≤ 200 characters, `text` ≤ 500. At most 20 blocks; the rest are ignored with WARN.
- No `home.md`, invalid YAML, no `blocks` or none of them valid → default set: the example above without `intro` text, and WARN.
- Unknown or missing `type` → the block is skipped, WARN. An invalid or unknown field → only that field is ignored, WARN; the block keeps its other fields and the defaults, so an `intro` keeps its title when its `text` is broken. An empty value, also for one language in a map, is no text. A number or `true`/`false` is taken as its text.
- WARNs repeat only when the file changes, not on every poll.
- Only the first `posts` block is used, since a page has one pagination; others are skipped with WARN.
- The `featured` post does not repeat in `posts`, on any page.
- Page 2+ shows only the `posts` block and pagination. Without a `posts` block, `/?page=2` is `404`.
- One `h1` per page: the `intro` title, else the site name, visually hidden.

### Post page

- Markdown from S3 rendered to HTML.
- Banner: the cover (`cover`) above the title at full window width, height `min(48vh, 460px)`, 260px at ≤ 640px, `object-fit: cover`, no rounded corners. Its lower part fades into `--bg`: a gradient `transparent 30%` → `--bg 100%` over the image, and `backdrop-filter: blur(14px)` under the mask `linear-gradient(to bottom, transparent, #000 70%)` in the lower 45%. The `<img>` keeps `width`/`height`. No cover: no banner.
- Post header: aligned to the left edge of the text column, overlaps the banner by 150px (100px at < 1000px); no overlap without a banner. Order: tags, title, metadata (date, `updated` if set, reading time, views). Over a banner the metadata is `--text` and the title has a `--bg` halo (`text-shadow`), so both stay readable on any cover.
- Grid: text (`max-width: 680px`) on the left, table of contents 250px on the right, gap 80px. Below 1000px the contents move into `<details>` "Contents" above the text.
- Table of contents: built on the server from h2–h4, which already have ids. A tree: h3 indented 30px, h4 46px with a smaller font. Each item has a 2px `--line` line on its left. Active item: `--text`, `--uv` line, weight 600; its parent h2 is `--text` too. In the side column the `--uv` line is one sliding bar (see Motion). The column is `position: sticky; top: 84px`. `post.js` marks the active item: the last heading that has reached the top bar (+32px), where a contents link puts it; at the end of the page the last visible heading. A clicked item stays active until the reader scrolls (wheel, touch, key), so a short section does not hand it to the next heading. No headings: no contents.
- Heading markers: `##` hangs in the margin left of h2 and `###` left of h3. CSS `::before`, not text; JetBrains Mono, `--uv`, opacity 0.6 and 0.45. Brighter on hover; a click copies the link to the section (JS). None below 1000px.
- Code blocks and callouts: see Content features.
- Post footer: "Was this post helpful?", like, dislike. On the right, "Share" as a 44px icon button (three linked dots, the label in `aria-label` and `title`): `navigator.share()` with title and URL when available, otherwise copies the URL, and the icon turns into a check mark in `--text` for 1.5 s; "Link copied" goes only to a visually hidden `role="status"`. No button without JS. Below, two cards: "Previous post" (the next older one, if any) and "All posts". Cards: min height 64px, padding 12px 18px, radius 16px, title at most two lines. At ≤ 640px the question takes its own row, the buttons and "Share" the next.

## Configuration

All settings and secrets come from environment variables (`.env`).
The repo has `.env.example` only. Real values never go into the repo.

Proposed variables (final names set during implementation):

| Variable | Purpose |
|---|---|
| `SITE_NAME`, `SITE_URL` | Site name and public URL. |
| `AUTHOR_NAME` | Optional. Author in JSON-LD and RSS. Default: `SITE_NAME`. |
| `DEFAULT_LANG` | UI language and default post language: `en` or `ru`. Default `en`. |
| `GITHUB_URL` | GitHub link in the top bar when `home.md` sets no social links. |
| `S3_ENDPOINT`, `S3_REGION`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY` | Bucket access. `S3_ENDPOINT` must be `https://`. `S3_REGION` default: `garage`. |
| `POLL_INTERVAL` | Bucket polling interval. Default: `60s`, range 10s–24h. |
| `REINDEX_TOKEN` | Token for `POST /reindex`. |
| `ALTCHA_HMAC_KEY` | ALTCHA secret. |
| `READING_WPM` | Words per minute for estimated reading time. Default: `200`. |
| `LOG_LEVEL`, `LOG_FORMAT` | Log level and format (`json` / `text`). Defaults: `info`, `json`. |
| `DATA_DIR` | Directory for SQLite (`hyperhack.db`). Default: `/data`. |
| `VAULT_DIR` | Directory for the local bucket mirror. Default: `/vault`. |
| `RATE_LIMIT_VOTE`, `RATE_LIMIT_BEACON`, `RATE_LIMIT_REINDEX` | Per-client limits on write endpoints. Defaults: `10/1m`, `30/1m`, `5/1m`. |
| `POPULAR_DAYS` | "Popular" window. Default: `30`. The list size is the `limit` of the `popular` block. |
| `PAGE_SIZE` | Posts per page in the `posts` block (unless its `limit` is set) and tag pages. Default: `10`. |
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
- `assets/logo.svg` — site logo in the top bar and favicon when `home.md` sets no `logo` (see Pages → Top bar). Without both the service shows a built-in mark.
- `home.md` — optional main page blocks, in the root only (see Pages → Main page).
- The service ignores keys where any path segment starts with `.` or `_` (same rule as Remotely Save).

### Posts

- Every `.md` file in the vault is a post candidate, at any depth, except `assets/`, ignored paths (`.` / `_`) and `home.md` in the root.
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
| `theme` | map | no | Colors of this post page over the site ones. See Design → Colors → Own colors. |

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
- Header 44px above each block, indented ones too: language on the left in `--muted` (empty if not set), "Copy" button on the right. After a click: a check mark and "Copied" for 1.5 s. The button appears only with JS. The language is an attribute shown by CSS, so it stays out of search text and RSS.

### Callouts

- Obsidian syntax: `> [!note]`, `> [!tip]`, `> [!warning]`, case-insensitive, with an optional title after the type. Parsed by the renderer. Any other type stays a plain blockquote.
- Block on `--uv-soft`, radius 14px, icon and type title, no colored left stripe. Icons come from CSS.
- Default titles follow the post `lang`, since post HTML is rendered once: Russian for `ru`, English otherwise.

### Math

- Obsidian syntax: `$…$` inline, `$$…$$` display (on its own lines or on one line). As in Obsidian, the opening `$` is not followed by a space and the closing `$` is not preceded by a space or followed by a digit, so `$5 and $10` stays text. `\$` is a literal dollar. Code spans and code blocks are not parsed.
- Server-side: TeX → MathML with `github.com/wyatt915/treeblood` `v0.1.16` (pure Go, MIT). Browsers show MathML natively: no JS, no CSP change, formulas also work in RSS.
- A formula over 4096 bytes, or one the converter rejects, is shown as its TeX in `<code class="math-error">`.
- MathML passes `bluemonday` with an allowlist: presentation elements and plain attributes with strict values; no `style`, `href`, `annotation-xml`, `mglyph` or `maction`. The TeX source stays in `<annotation>`; search text takes the TeX, not the MathML.
- A wide display formula scrolls sideways inside its own box.

### Diagrams

- Obsidian syntax: a ` ```mermaid ` code block. Drawn in the browser by Pele (Mermaid → SVG, MIT), vendored as `static/pele.js` (1.13.0, `dist/pele.global.js` checked against the npm integrity hash, license in `static/pele-LICENSE.txt`).
- `pele.js` and `diagram.js` load only on posts with a ` ```mermaid ` block. Without JS, in RSS, and when a diagram has an error, the code block stays.
- Pele writes `style="…"` attributes, which the CSP blocks. `diagram.js` renames them before parsing and sets the same styles through CSSOM, which the CSP allows. Pele zoom is off: its buttons set style attributes.
- Frame like a code block; colors from the theme tokens through the `--pele-*` variables. The layout adapts to the column width.

### Search

Goal: the best search possible without an extra service.

- Embedded full-text index: Bleve v2 (`github.com/blevesearch/bleve/v2`), in memory, rebuilt on sync.
- Language-aware analysis by post `lang`: English and Russian analyzers (Russian: Snowball stemmer + stop words, package `analysis/lang/ru`). "Постов" finds "пост".
- Fields and weight: `title` > `tags` > `description` > body.
- Typo tolerance (fuzzy match), prefix match for search-as-you-type.
- Result snippets with highlighted matches.
- Works without JS: `GET /search?q=…` renders results.
- With JS: `Ctrl K`, `Cmd K` and `/` open a palette, a `<dialog>` of 660px, centered, 110px from the top, with a dimmed backdrop. Results update while typing, from `/search`.
- Tag filter inside the query: `#self-host garage`. The server takes `#tag` tokens from `q` and filters by them, several tags mean AND. A query of tags only lists their posts by date. A token with invalid tag syntax is ignored. The `tag` parameter stays for old links: it becomes a `#tag` token in the query. The search form has no tag dropdown.
- While a `#` token is being typed, the palette shows tag suggestions with post counts instead of results. Suggestions match the start of the tag, most used first. A click or `Enter` completes the tag and adds a space. Chosen tags appear as chips under the field.
- Tags with counts come as `<script type="application/json">` in the page; no new endpoint.
- Palette footer: `#tag` filters by tag, `↑ ↓` select, `Enter` opens, `Esc` closes. The first result is selected; going past either end selects nothing, and `Enter` then opens the results page.
- A match in a title is underlined and colored `--uv`; on a selected row it is `--text`. A title too long for the highlighter stays plain.
- No results: "Nothing found for “…”. Check the word or remove #tag from the query."
- The results page has `noindex`.

Rejected: Meilisearch and similar. Better typo handling, but a separate service.

### Popular and Newest

- "Popular": most views in the last `POPULAR_DAYS` days, top `limit` of the `popular` block.
- "Newest": by `date`, the `posts` block, with pagination.

### Tags

- Page `/tags/<tag>`: posts with this tag, paginated.
- Tag pages use the same post rows as the `posts` block.
- All tags look the same: `#` in `--uv`, then the name. No per-tag colors.

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
| Code copies per post | script (`copy` event inside code blocks, "Copy" button) | Which code is useful. |
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
| `/favicon.ico` | GET | public | built-in SVG mark, or `302` to the favicon from `home.md` / `/assets/logo.svg`; never `404` |
| `/static/*` | GET | public | embedded CSS, JS and fonts, and `/static/theme/<hash>.css` built from validated colors (unknown hash → `404`); no directory listing |
| `/<INDEXNOW_KEY>.txt` | GET | public | only if `INDEXNOW_KEY` is set; body is the key |

Other methods → `405`. Unknown paths → `404`.

### Input validation

- Body size: `http.MaxBytesReader`, 4 KB for `/api/*`.
- JSON: `Content-Type: application/json` required, `DisallowUnknownFields`, one object only.
- Slug must exist in the current index. Unknown slug → `404`, nothing stored. This also bounds metric labels.
- Numbers are clamped: time on page 0–4 h; read depth only 0 / 25 / 50 / 75 / 100.
- Enums only: vote (`like` / `dislike` / `none`), source, device, theme, lang. Other values → `400`.
- Search query: max 200 chars, max 10 terms, `#tag` tokens included. The query is built in code (match + fuzzy + prefix), never from Bleve query-string syntax: this blocks regex and wildcard DoS. Fuzziness ≤ 2, prefix min 2 chars. Page number bounded.
- Cookies `lang` and `theme`: enum check; invalid → default.
- Theme colors from `home.md` and posts: known names only, values only hex colors. Anything else is ignored.

### Output and XSS

- All HTML through `html/template` (context-aware escaping). Templates parsed at startup.
- Markdown: goldmark with GFM (tables, strikethrough, autolinks), without `WithUnsafe`. By default goldmark does not render raw HTML or dangerous URLs.
- Defense in depth: post HTML passes `bluemonday` (UGC policy + chroma classes + heading ids + callout and code block classes + code language attribute + MathML allowlist).
- External links get `rel="noopener noreferrer"`.
- Images only from `assets/`. External images are blocked by CSP (also keeps reader IPs private).
- JSON-LD from `json.Marshal` (escapes `<`, `>`, `&`).
- RSS and sitemap via `encoding/xml`.
- `/posts/<slug>.md`: `Content-Type: text/markdown; charset=utf-8`, `nosniff`.
- Error pages show no internals.

### Security headers (set by the service)

- `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; worker-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; upgrade-insecure-requests`. Add `blob:` to `worker-src` only if the ALTCHA build needs it.
- No inline scripts or styles. Theme and language come from cookies and are rendered on the server, so no inline script is needed against a theme flash. `<script type="application/json">` is a data block that does not run, so the CSP does not change.
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
- Go fuzz tests: frontmatter parser, Markdown render, TeX math, beacon decoder.
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
- 2026-11-04 — Post cover as a full-width banner between the top bar and the text column: height `min(30vh, 320px)` (`min(25vh, 200px)` on screens up to 30rem), `object-fit: cover` centered, no rounded corners, `width`/`height` kept. Rendered from a `banner` block in the layout, outside `<main>`. Posts without cover unchanged.
- 2026-11-04 — Favicon: every page has `<link rel="icon" type="image/svg+xml">` pointing to `/assets/logo.svg` when the bucket has it, else to `/favicon.ico`. `GET /favicon.ico` serves the built-in mark as SVG (accent background, white mark, fixed colors), or answers `302` to `/assets/logo.svg` when the logo exists; `Cache-Control: max-age=3600`, since the logo can change with any sync. Never `404`.
- 2026-11-06 — Language switch as a dropdown: the top bar shows the current language, its menu lists English and Русский. A native `popover`, placed with CSS anchor positioning, so it opens and closes without JS; the menu buttons still post the `/prefs` form. Without `popover` support the buttons show inline.
- 2026-11-06 — Redesign. New tokens (`--uv`, `--ember`, `--uv-soft`…), font Onest instead of Inter, sticky 72px top bar; at ≤ 640px the site name is hidden so the bar fits one row. Text on `--uv-soft` is `--text`, because `--uv` text stays below 4.5:1 there in the dark theme. Hash colors for tags removed. Main page built from blocks in `home.md`, a reserved name in the vault root; default set without author text; `POPULAR_LIMIT` removed in favor of the `limit` of the `popular` block; only the first `posts` block is used. Post page: banner `min(48vh, 460px)`, server-built table of contents, heading markers, code block header, Obsidian callouts (`note`, `tip`, `warning`), "Share", "Previous post". Code copies also count the "Copy" button. Search palette on `Ctrl K`, `Cmd K` and `/`; `#tag` tokens in `q` (AND), counted in the 10-term limit; tag dropdown removed, the `tag` parameter stays. Motion only under `prefers-reduced-motion: no-preference`; CSP forbids inline styles, so a script sets `view-transition-name` through the CSSOM. No new endpoints.
- 2026-11-06 — `home.md` with no valid block falls back to the default set, so a typo cannot leave the main page empty. An unknown field in a block counts as invalid, so a typo in a field name is reported.
- 2026-11-06 — Post page details: over a banner the metadata is `--text` and the title has a `--bg` halo, because measured on a white cover in the dark theme `--muted` metadata and the bare title fall below the contrast minimum. The code language is an attribute shown by CSS, so it stays out of search text and RSS. Indented code gets the same header as fenced code.
- 2026-11-06 — Search palette details: `search.js` loads on every page and also serves the live results on `/search`, so the palette needs no new endpoint. `#tag` tokens stay out of the stored search queries: only words are stored, so typed tags cannot fill the table. Past either end of the list nothing is selected, and `Enter` opens the results page, the only way from the palette to results beyond the first page.
- 2026-11-07 — Motion details: `transition.js` is the only blocking script, because a deferred one misses `pagereveal`. The cover name goes on the whole banner, so the fade stays during the transition, and the post header gets its own name, so the cover does not paint over tags and metadata. Names go on block boxes only, since a wrapped inline link would skip the transition. `scale(.94)` and the number animation apply to like and dislike.
- 2026-11-07 — Math and diagrams in Obsidian syntax. Math: `$…$` / `$$…$$` turned into MathML on the server with `treeblood` `v0.1.16` and an own goldmark parser (the `goldmark-treeblood` wrapper treats any `$` as math); MathML allowlist in `bluemonday`. Diagrams: ` ```mermaid ` drawn in the browser by Pele 1.13.0, vendored and loaded only on posts that have one; Pele style attributes move to CSSOM, so the CSP stays as it is; zoom off. Fallback in both: the source stays visible.
- 2026-11-07 — Top bar and featured card: the language dropdown moves to the right end of the top bar (its own `/prefs` form). The main page search button is 36px high instead of 44px. The featured card gets a `--uv` tint so it stands out from the page; its "New post" label mixes `--ember` with 20% `--text`, because plain `--ember` on the light tint measured 4.24:1 (now 5.57:1 light, 8.01:1 dark). The top bar is 60px instead of 72px; on post pages the title sits in the middle of the bar, centered, up to two lines. "Share" becomes an icon button. The cards below the votes are lower: min height 64px instead of 88px, title at most two lines.
- 2026-11-07 — Main page: the `popular` block loses its "Subscribe via RSS" link, since the top bar already has RSS. A bad or unknown field in a `home.md` block no longer skips the whole block: only that field is ignored with WARN (the typo is still reported). An empty language entry is no text instead of an error, and a number is text, because clearing one language of `text` in the vault dropped the whole `intro` with its title.
- 2026-11-07 — Table of contents: the active `--uv` line in the side column becomes one bar that slides between items and stretches on the way (leading edge first, trailing edge 90ms later, 320ms). Position through the CSSOM, so the CSP stays; no animation with reduced motion. The fade of the top bar title is now listed in Motion too.
- 2026-11-07 — Year headings say "5 posts" instead of a bare number. Plural forms come from `key_one`, `key_few`, `key_many` in the i18n files; a language with a `_few` form uses the Russian rule, others the English one.
- 2026-11-07 — Own colors: `theme` with `light` and `dark` maps in `home.md` sets any color token of the site, and the same field in a post sets its page; missing colors fall back to `home.md`, then the built-in ones. Only hex values pass, and the colors go out as a separate content-hashed stylesheet under `/static/theme/`, so the CSP needs no inline styles. A bad entry is ignored with WARN, as in `home.md` blocks.
- 2026-11-07 — The `popular` block shows likes and dislikes next to views, and an eye icon instead of the word "Views". The votes come from one query for all posts, made only when the block has posts.
- 2026-11-07 — Built-in palette: plain neutral dark (`#0A0A0A`) and white (`#FFFFFF`) with gray surfaces, borders and secondary text; the only color is a plain blue accent (`#2563EB` light, `#3B82F6` dark), also used for `--ember`, `--uv-soft` and the favicon. The featured card loses its tint and sits on `--raise`. Code highlighting keeps its palette. Own colors in `home.md` work as before.
- 2026-11-07 — Top bar logo box grows from 40×29px to 47×34px (about 17%), same aspect ratio: the mark looked too small next to the 21px site name.
- 2026-11-07 — Logos in `home.md`: `logo` sets the top bar logo for the light and the dark theme and a separate favicon, as paths inside `assets/`. `assets/logo.svg` stays as the fallback, so existing sites do not change. The icon link loses `type="image/svg+xml"`, since the favicon can now be a PNG.
- 2026-11-07 — Social links in `home.md`: `github`, `x` and `telegram` add Telegram and X buttons with their marks and move the GitHub link out of `.env`; a missing field hides its button. `GITHUB_URL` stays as the fallback when `home.md` sets none of the three. Icon buttons narrow to 36px at ≤ 30rem so the bar keeps one row.
