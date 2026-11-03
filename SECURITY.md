# Security policy

Russian version: [SECURITY.ru.md](SECURITY.ru.md).

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub private vulnerability reporting:

1. Open the repository's **Security** tab.
2. Click **Report a vulnerability**.

Direct link: <https://github.com/Mimic890/hyperhack/security/advisories/new>

Do not open a public issue, pull request or discussion for a vulnerability.

Please include:

- what is affected and the impact;
- steps to reproduce or a proof of concept;
- the version (image tag or commit).

## What happens next

- I reply within 7 days.
- I confirm the issue, prepare a fix and publish a new release.
- The advisory is published after the fix is released. You are credited unless you ask not to be.

## Supported versions

Only the latest release gets security fixes.

## Scope

In scope: the `hyperhack` service, its container image and this repository's CI.

Out of scope: Caddy, Garage, VictoriaMetrics, Grafana and other external services, unless the issue is in the configuration this repository ships (`docker-compose.yml`, `Caddyfile.example`).
