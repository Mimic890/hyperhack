# CLAUDE.md

## Source of truth

- `SPEC.md` is the source of truth for this project. Read it before every task.
- `SPEC.ru.md` is the Russian copy. Keep both files identical in content.
- A change to any decision goes into both SPEC files and their Decision log, in the same PR.
- If the SPEC is unclear or contradicts itself, stop and ask. Do not guess.
- Do not add features that are not in the SPEC. Propose them instead.

## Language

- Everything in English: code, comments, commit messages, PR text, docs, UI strings.
- Every doc has a Russian copy: `README.md` + `README.ru.md`, `SPEC.md` + `SPEC.ru.md`. A doc change updates both.
- Russian UI strings live in the i18n files next to the English ones.

## Stack

- Go 1.27 (`go 1.27`, `toolchain go1.27.3` in `go.mod`). Check go.dev/dl for a newer patch before starting.
- Standard library first. Add a dependency only when the SPEC names it or the stdlib cannot do the job in a reasonable amount of code.
- Pin every dependency version. Verify that a package exists and is maintained before adding it.

## Work in stages

One stage = one branch = one PR. Do not start the next stage before the current PR is merged.

1. Skeleton: config from env, `log/slog` logging, HTTP server with timeouts, `/healthz`, Dockerfile, `docker-compose.yml`, `.env.example`, `.gitignore`, CI.
2. S3 sync: polling, `/reindex`, local mirror via `os.Root`, frontmatter parsing, Markdown render with sanitizing.
3. Pages: main page, post page, top bar, themes, languages, templates, styles.
4. Search, tags, RSS, SEO files.
5. Metrics: beacon, views, votes, ALTCHA, rate limits, `/metrics`.
6. Grafana dashboard, `Caddyfile.example`, full README in both languages.

## Git

- Branches: `feat/…`, `fix/…`, `refactor/…`, `docs/…`, `chore/…`. Never commit to `main` directly.
- Conventional Commits: `feat:`, `fix:`, `docs:`, `refactor:`, `chore:`, `test:`, `ci:`.
- Small commits, one logical change each.
- Never delete branches or commits. Use `git revert`. No rebase or force push on pushed branches.
- Never mention AI, Claude or any assistant in commits, PRs, docs or code comments.

## Security rules

- Follow SPEC → Security for every change.
- Never commit secrets, real domains, bucket names or keys. Real values live only in `.env`. Examples use `example.com`.
- Never log secrets, raw IPs or visitor hashes.
- Validate all input at trust boundaries: HTTP requests, bucket objects, frontmatter, cookies.
- New endpoint → add it to the SPEC endpoint table with its access and protection.

## Quality checks

Run before every commit. Do not say a task is done until all pass:

```
go vet ./...
staticcheck ./...
govulncheck ./...
go test -race ./...
```

- New non-trivial logic gets tests. Parsers and decoders also get Go fuzz tests.
- Security code (sanitizer, path checks, rate limits, ALTCHA, Origin check) always gets tests.

## Docker

- Bind mounts to subfolders only (`./data`, `./vault`). Never named volumes.
- Static binary, distroless or `scratch` runtime image, non-root user. See SPEC → Container.

## Communication

- Short, direct answers. No filler.
- When a task is done: what changed, what was not checked, any risk.
