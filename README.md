# hyperhack

Russian version: [README.ru.md](README.ru.md).

A light, fast blog for one author. It replaces Ghost when Ghost is too heavy.

You write posts in Obsidian. The vault syncs to an S3 bucket (Garage) and the blog renders it. The whole service is one Go binary with SQLite. There is no admin panel and no database server.

- Posts are Markdown files with Obsidian properties (YAML frontmatter).
- Pages are rendered on the server and work without JS.
- Light and dark themes. The UI is in English and Russian.
- Search in English and Russian, with typo tolerance and search-as-you-type.
- Tags, RSS, sitemap, Open Graph, JSON-LD, `llms.txt` and IndexNow.
- Each post shows its reading time, view count, likes and dislikes. Votes are anonymous and need an ALTCHA proof of work.
- Private per-post metrics on `/metrics` (Prometheus format), with a Grafana dashboard.

The design is in [SPEC.md](SPEC.md).

## How it fits together

```
Obsidian ──Remotely Save──▶ Garage bucket ◀──poll── hyperhack ──▶ ./data (SQLite)
                                                       │
                                                       └──mirror──▶ ./vault
reader ──▶ Caddy ──┬──▶ hyperhack :8080         (pages, /api/*)
                   └──▶ ./vault/assets/         (/assets/*, served by Caddy)
VictoriaMetrics ──scrape /metrics──▶ hyperhack ;  Grafana ──▶ VictoriaMetrics
```

The bucket is the source of truth. The service polls it and mirrors it to `./vault`. Caddy serves images straight from the mirror, so pages and images keep working while S3 is down.

## Requirements

- Docker with Docker Compose.
- An S3 API bucket over HTTPS. Garage is what this is built and tested for.
- Caddy as the reverse proxy, built with the `caddy-ratelimit` and CrowdSec bouncer plugins. See [Caddy](#caddy).
- Optional: VictoriaMetrics and Grafana for metrics, and CrowdSec.

## Quick start

1. Download the compose file and the env file:

   ```sh
   mkdir hyperhack && cd hyperhack
   curl -fsSLO https://raw.githubusercontent.com/Mimic890/hyperhack/main/docker-compose.yml
   curl -fsSL -o .env https://raw.githubusercontent.com/Mimic890/hyperhack/main/.env.example
   chmod 600 .env
   ```

   `docker-compose.yml` only pulls the released image from GHCR and pins the latest release, for example `ghcr.io/mimic890/hyperhack:0.1.0`. Image tags are the release version without the `v`, and they are never overwritten. There is no `latest` tag.

2. Create the data folders. The container runs as user `10001`:

   ```sh
   mkdir data vault
   sudo chown 10001:10001 data vault
   ```

   Caddy must be able to read `vault/assets/`. The service writes folders as `0755` and files as `0644`. The SQLite file is `0600`.

3. Fill `.env` (see [Configuration](#configuration)). Generate each secret with:

   ```sh
   openssl rand -base64 48
   ```

4. Verify the image and start the service. Use the tag from `image:` in `docker-compose.yml`:

   ```sh
   gh attestation verify oci://ghcr.io/mimic890/hyperhack:<version> -R Mimic890/hyperhack
   docker compose up -d
   docker compose ps        # wait for "healthy"
   docker compose logs -f
   ```

   On the first start the service needs the bucket. If S3 is unreachable and there is no mirror yet, it exits with code 1. Later starts build the index from the mirror and do not wait for S3.

5. Set up Caddy (see [Caddy](#caddy)) and set `TRUSTED_PROXIES`.

The service listens on port `8080` inside the container. Compose publishes it on `127.0.0.1:8080` only. Do not expose it to the internet: Caddy is the only public entry.

## Garage: bucket and keys

The blog gets its own bucket and two keys: a read-only key for the service and a read-write key for Obsidian.

```sh
garage bucket create example-blog
garage key create hyperhack-ro
garage bucket allow --read example-blog --key hyperhack-ro
garage key create obsidian
garage bucket allow --read --write example-blog --key obsidian
```

Put the `hyperhack-ro` key into `.env` as `S3_ACCESS_KEY` and `S3_SECRET_KEY`. `S3_ENDPOINT` must be `https://`. The service uses path-style addressing.

## Configuration

All settings come from environment variables in `.env`. [.env.example](.env.example) lists them all. The service checks every value at startup. If any value is invalid, it prints every problem and exits with code 1. Errors name the variable, never its value.

| Variable | Default | Purpose |
|---|---|---|
| `SITE_NAME` | required | Site name in the top bar, titles and RSS. |
| `SITE_URL` | required | Public URL, `https://example.com`. Used for canonical links, RSS, the sitemap and the `Origin` check. |
| `AUTHOR_NAME` | `SITE_NAME` | Author in JSON-LD and RSS. |
| `DEFAULT_LANG` | `en` | UI language and default post language: `en` or `ru`. |
| `GITHUB_URL` | empty | GitHub link in the top bar. |
| `S3_ENDPOINT` | required | Garage S3 API URL, `https://` only. |
| `S3_REGION` | `garage` | S3 region. |
| `S3_BUCKET` | required | Bucket name. |
| `S3_ACCESS_KEY`, `S3_SECRET_KEY` | required | The read-only key. |
| `POLL_INTERVAL` | `60s` | Bucket polling interval, 10s to 24h. |
| `REINDEX_TOKEN` | required | Bearer token for `POST /reindex`. At least 32 bytes. |
| `ALTCHA_HMAC_KEY` | required | Signs ALTCHA challenges. At least 32 bytes. |
| `VISITOR_SECRET` | required | Key for the daily visitor hash. At least 32 bytes. |
| `ALTCHA_EXPIRES` | `5m` | Challenge lifetime, 10s to 1h. |
| `ALTCHA_MAX_NUMBER` | `100000` | Challenge difficulty, 1 000 to 10 000 000. |
| `READING_WPM` | `200` | Words per minute for the reading time, 50 to 1000. |
| `POPULAR_DAYS`, `POPULAR_LIMIT` | `30`, `5` | "Popular": most views in the last N days, top M posts. |
| `PAGE_SIZE` | `10` | Posts per page on the main page and tag pages. |
| `RSS_LIMIT` | `20` | Posts in `/rss.xml`. |
| `DEFAULT_OG_IMAGE` | empty | Link preview image for pages and posts without `cover`. A file inside `assets/`, e.g. `assets/og.png`. |
| `INDEXNOW_KEY` | empty | Turns on IndexNow pings (Bing, Yandex). 8 to 128 characters `a-z A-Z 0-9 -`. Served at `/<key>.txt`. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `LOG_FORMAT` | `json` | `json` for Docker and Loki, `text` for local runs. |
| `DATA_DIR` | `/data` | SQLite folder (`hyperhack.db`). |
| `VAULT_DIR` | `/vault` | Bucket mirror folder. |
| `RATE_LIMIT_VOTE` | `10/1m` | Per client, `/api/altcha` and `/api/vote` together. |
| `RATE_LIMIT_BEACON` | `30/1m` | Per client, `/api/beacon`. |
| `RATE_LIMIT_SEARCH` | `30/1m` | Per client, `/search`. |
| `RATE_LIMIT_REINDEX` | `5/1m` | Per client, `/reindex`. |
| `METRICS_ALLOW` | loopback and private ranges | Comma-separated CIDRs allowed to read `/metrics`. |
| `REINDEX_ALLOW` | loopback and private ranges | Comma-separated CIDRs allowed to call `/reindex`. |
| `TRUSTED_PROXIES` | empty | IPs or CIDRs allowed to set `X-Forwarded-For`. See [Caddy](#caddy). |
| `MAX_POST_BYTES` | `1048576` (1 MiB) | Larger `.md` files are skipped. |
| `MAX_ASSET_BYTES` | `52428800` (50 MiB) | Larger assets are skipped. |
| `MAX_OBJECTS` | `10000` | Maximum keys in the bucket, ignored keys included. |

Rates are `count/period`, for example `10/1m`. The private ranges are `127.0.0.0/8`, `::1/128`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` and `fc00::/7`.

## Writing posts

Each `.md` file is one post, at any depth in the vault. Folders are only for you: the URL is always `/posts/<slug>`, so moving a file does not break links. The service skips files and folders whose name starts with `.` or `_`. Keep templates and helper notes in `_templates/` or another `_` folder.

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
| `description` | string | no | Short summary for the post list, RSS, meta tags and link previews. |
| `date` | date | yes | Publish date. "Newest" is sorted by it. |
| `updated` | date | no | Last update. Shown on the post and in RSS. |
| `tags` | list | no | Tags. A single string also works. |
| `cover` | string | no | Cover image, a file inside `assets/`. Shown as a full-width banner above the title, cropped to a wide strip around the center: keep the subject in the middle. |
| `slug` | string | no | URL part. Default: made from the file name. |
| `lang` | string | no | Post language, two lower-case letters (`en`, `ru`). Default: `DEFAULT_LANG`. |
| `draft` | bool | no | `true` keeps the post unpublished. Default `false`. |

Rules:

- Dates: `2026-11-03` or `2026-11-03T20:59`, as Obsidian writes them. A time without a zone is UTC.
- Without `title` or `date`, or with invalid YAML, the post is skipped and a WARN is logged. An invalid `date`, `updated`, `slug`, `lang`, `cover` or `draft` also skips the post.
- Slug from the file name: lower case, Russian letters transliterated (`ж` → `zh`, `х` → `kh`, `щ` → `shch`, `ъ` and `ь` dropped), and any other run of characters becomes `-`. A slug in frontmatter must match `^[a-z0-9-]{1,100}$`.
- Tags are lower-cased and duplicates merged. A tag is 1 to 50 letters, digits, `-`, `_` or `/`. At most 20 tags per post.
- If two posts have the same slug, the first by object key wins. The second is skipped and an ERROR is logged.
- Unknown fields are ignored, so Obsidian can add its own.
- Images: standard Markdown, `![Alt text](assets/photo.webp)`. A path `assets/…` becomes `/assets/…` on the site. External images are blocked by the CSP.
- Allowed asset types: `png jpg jpeg webp avif gif svg mp4 webm`.
- `assets/logo.svg`, if present, replaces the built-in logo in the top bar.

## Publishing from Obsidian

Use a separate Obsidian vault for the blog, so personal notes never reach the bucket.

1. Install the **Remotely Save** plugin and choose S3.
2. Endpoint, region, bucket: the same as in `.env`. Keys: the `obsidian` read-write key, not the service key.
3. Use path-style URLs.
4. Leave the remote prefix empty. The blog has its own bucket.
5. Leave encryption off. With encryption on, the service cannot read the files.

Obsidian settings, **Files and links**:

| Setting | Value |
|---|---|
| Use [[Wikilinks]] | off |
| New link format | Absolute path in vault |
| Default location for new attachments | In the folder specified below → `assets` |

With these settings Obsidian inserts images as standard Markdown, `![](assets/abstract-5.jpg)`. Wikilinks (`![[…]]`) are not supported.

A new post appears after the next poll, within `POLL_INTERVAL`. To publish at once, call `/reindex` from the private network:

```sh
curl -X POST -H "Authorization: Bearer $REINDEX_TOKEN" http://127.0.0.1:8080/reindex
```

It waits for the sync and returns `204` when done, `401` for a bad token, `403` for an address outside `REINDEX_ALLOW`, `429` when over the rate limit and `503` when the sync failed.

## Caddy

Caddy terminates TLS and adds the perimeter controls: per-client rate limit, CrowdSec, HSTS and the request body limit. It blocks `/metrics` and `/reindex` and serves `/assets/*` from the mirror.

Build Caddy with the two plugins:

```sh
xcaddy build v2.11.7 \
  --with github.com/mholt/caddy-ratelimit@v0.1.0 \
  --with github.com/hslatman/caddy-crowdsec-bouncer/http@v0.14.1
```

This is [Caddyfile.example](Caddyfile.example). Replace `example.com` and `/srv/hyperhack`:

```caddyfile
{
	order crowdsec first
	order rate_limit after crowdsec
	crowdsec {
		api_url {$CROWDSEC_API_URL}
		api_key {$CROWDSEC_API_KEY}
	}
}

example.com {
	crowdsec

	# Per client IP. Static files and assets are not counted.
	@limited not path /static/* /assets/*
	rate_limit @limited {
		zone per_client {
			key {remote_host}
			events 1200
			window 1m
		}
	}

	header {
		Strict-Transport-Security "max-age=31536000"
		-Server
	}

	encode zstd gzip

	# Private endpoints. VictoriaMetrics and /reindex callers reach the service directly.
	# A handle block, because handle runs before a bare respond.
	@private path /metrics /metrics/* /reindex /reindex/*
	handle @private {
		respond 404
	}

	# Assets straight from the mirror (vault/assets/), allowed extensions only.
	handle /assets/* {
		@allowed path *.png *.jpg *.jpeg *.webp *.avif *.gif *.svg *.mp4 *.webm
		handle @allowed {
			root * /srv/hyperhack/vault
			header {
				X-Content-Type-Options nosniff
				Content-Security-Policy "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox"
			}
			file_server
		}
		respond 404
	}

	handle {
		request_body /api/* {
			max_size 16KB
		}
		reverse_proxy 127.0.0.1:8080
	}
}
```

- `CROWDSEC_API_URL` and `CROWDSEC_API_KEY` are read from Caddy's environment when the config loads. The CrowdSec LAPI also defaults to port `8080`, so move one of them if both run on the host.
- The service sets its own CSP and security headers. Caddy adds only HSTS and removes `Server`.
- The site-wide limit is 1200 requests per minute per client IP. It counts pages and API calls; `/static/*` and `/assets/*` are not counted, so images and fonts never use it up.
- Without the plugins, delete the `order`, `crowdsec`, `@limited` and `rate_limit` lines. The service still rate-limits its write endpoints.

### TRUSTED_PROXIES

Behind Caddy the service sees Caddy's address, not the reader's. Without `TRUSTED_PROXIES`, every reader shares one rate limit and one visitor hash. With it, the service reads the client IP from `X-Forwarded-For`.

With Caddy on the host and the port published on `127.0.0.1:8080`, requests reach the container from the Docker network gateway. Find it with:

```sh
docker network inspect hyperhack_default -f '{{(index .IPAM.Config 0).Gateway}}'
```

Set `TRUSTED_PROXIES` to that address (for example `172.18.0.1`) and restart. If Caddy runs in the same Compose network, use Caddy's container address or the network's subnet.

## Monitoring

`/metrics` serves Prometheus text format on the main port. It answers only `METRICS_ALLOW` addresses, and Caddy blocks it on the public domain. Scrape it directly, not through Caddy.

VictoriaMetrics scrape config (`-promscrape.config`):

```yaml
scrape_configs:
  - job_name: hyperhack
    scrape_interval: 30s
    static_configs:
      - targets: ["127.0.0.1:8080"]
```

If VictoriaMetrics runs in Docker, use an address it can reach, e.g. put it in the same network and use `hyperhack:8080`.

### Grafana

[grafana/hyperhack.json](grafana/hyperhack.json) is the dashboard. In Grafana: **Dashboards → New → Import**, upload the file, then pick the data source. It needs a Prometheus-type data source pointed at VictoriaMetrics. Panels:

- Overview: posts, views, average time on page, likes, dislikes, time since the last sync.
- Posts: views per day, a per-post table (views, time, read to end, code copies, votes), read depth, votes per day.
- Readers: traffic source, device, UI language, theme, outbound clicks.
- Search: queries and zero-result queries per hour, latency, top queries, top zero-result queries.
- HTTP: requests per route, 5xx and 429, p95 latency.
- Sync: bucket objects, failed syncs, failed downloads, time since the last attempt.

The `Post` variable filters the post panels.

### Metrics

| Metric | Type | Labels |
|---|---|---|
| `hyperhack_post_views_total` | counter | `slug` |
| `hyperhack_post_read_seconds_total` | counter | `slug` |
| `hyperhack_post_read_views_total` | counter | `slug` |
| `hyperhack_post_read_depth_total` | counter | `slug`, `depth` (25, 50, 75, 100) |
| `hyperhack_post_source_total` | counter | `slug`, `source` |
| `hyperhack_post_device_total` | counter | `slug`, `device` |
| `hyperhack_post_ui_lang_total` | counter | `slug`, `lang` |
| `hyperhack_post_ui_theme_total` | counter | `slug`, `theme` |
| `hyperhack_post_outbound_clicks_total` | counter | `slug`, `host` |
| `hyperhack_post_code_copies_total` | counter | `slug` |
| `hyperhack_post_votes_total` | counter | `slug`, `vote` |
| `hyperhack_post_likes`, `hyperhack_post_dislikes` | gauge | `slug` |
| `hyperhack_posts` | gauge | |
| `hyperhack_sync_last_attempt_timestamp_seconds`, `hyperhack_sync_last_success_timestamp_seconds` | gauge | |
| `hyperhack_sync_errors_total`, `hyperhack_sync_fetch_failures_total` | counter | |
| `hyperhack_sync_objects` | gauge | |
| `hyperhack_search_queries_total`, `hyperhack_search_zero_results_total` | counter | |
| `hyperhack_search_duration_seconds` | histogram | |
| `hyperhack_search_top_queries`, `hyperhack_search_top_zero_result_queries` | gauge | `query` (top 20) |
| `hyperhack_http_requests_total` | counter | `route`, `code` |
| `hyperhack_http_request_duration_seconds` | histogram | `route` |

The post counters are stored in SQLite and survive restarts. The search and HTTP counters are in memory and restart from zero. `increase()` and `rate()` handle both. Average time on page is `read_seconds / read_views`.

Views and read depth count once per visitor per post per day. A visitor is a daily HMAC of the IP (IPv6 `/64`) and the user agent. It lives in memory only, and raw IPs are never stored or logged. Readers without JS are not counted.

## Logs

JSON to stdout, one record per line, with `time`, `level`, `msg` and `component` (`main`, `sync`, `http`, `render`, `votes`, `metrics`, `store`). `LOG_LEVEL=debug` adds sync details and every HTTP request. Secrets, raw IPs and visitor hashes are never logged.

## Backups

- `data/` holds the SQLite database: views, votes and search stats. Back it up. For a consistent copy, stop the container or copy `hyperhack.db` together with `hyperhack.db-wal` and `hyperhack.db-shm`.
- `vault/` is a mirror of the bucket and does not need a backup. Delete it to force a full resync.
- The bucket is the source of the posts. Back it up with Garage or keep the Obsidian vault in git.

## Rotating secrets

Change the value in `.env`, then `docker compose up -d` to restart.

- `REINDEX_TOKEN`: update every caller of `/reindex` at the same time.
- `ALTCHA_HMAC_KEY`: challenges issued before the restart fail, and readers click again. Votes are kept.
- `VISITOR_SECRET`: only affects view dedup, which is in memory and resets on restart anyway. A reader counted earlier that day can be counted once more.
- S3 keys: create a new read-only key in Garage, put it into `.env`, restart, check the logs for a successful sync, then delete the old key.

Vote cookies (`vid`) are random and do not depend on any secret.

## Upgrading

Versions are listed under [tags](https://github.com/Mimic890/hyperhack/tags). Release `v0.2.0` is image `ghcr.io/mimic890/hyperhack:0.2.0`.

1. Back up `data/` (see [Backups](#backups)).
2. In `docker-compose.yml`, set the new tag in `image:`. If you have not changed the file, you can download it again from `main` instead: it pins the latest release.
3. Verify the new image, pull it and restart:

   ```sh
   gh attestation verify oci://ghcr.io/mimic890/hyperhack:<version> -R Mimic890/hyperhack
   docker compose pull
   docker compose up -d
   docker compose ps        # wait for "healthy"
   ```

To roll back, set the old tag and run `docker compose up -d`. If the old version does not start, restore `data/` from the backup.

## Development

Go 1.27 (`toolchain go1.27.3`).

```sh
set -a; . ./.env; set +a
DATA_DIR=./data VAULT_DIR=./vault LOG_FORMAT=text go run .
# or, in Docker: docker compose -f compose.dev.yml up --build
```

`compose.dev.yml` builds the image from the checkout as `hyperhack:dev` and otherwise uses the settings of `docker-compose.yml`. Compose never loads it on its own, so production stays on the released image.

Checks, run before every commit and in CI:

```sh
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
go test -race ./...
```

The ALTCHA test runs the browser solver in Node when `node` is installed. With `CI` set, a missing `node` fails the test.

Contributions: one change per branch and PR, Conventional Commits, and every design decision in both SPEC files.

## Security

Report vulnerabilities privately, see [SECURITY.md](SECURITY.md).

## License

[AGPL-3.0](LICENSE).
