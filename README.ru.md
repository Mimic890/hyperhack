# hyperhack

English version: [README.md](README.md).

Лёгкий и быстрый блог для одного автора. Замена Ghost, когда Ghost слишком тяжёлый.

Вы пишете посты в Obsidian. Хранилище синхронизируется с бакетом S3 (Garage), а блог рендерит его. Весь сервис — один бинарник на Go с SQLite. Нет админки и нет сервера базы данных.

- Посты — файлы Markdown со свойствами Obsidian (YAML frontmatter).
- Страницы рендерятся на сервере и работают без JS.
- Светлая и тёмная темы. Интерфейс на английском и русском.
- Поиск на английском и русском: с учётом опечаток и поиском по мере ввода.
- Теги, RSS, sitemap, Open Graph, JSON-LD, `llms.txt` и IndexNow.
- На каждом посте — время чтения, число просмотров, лайки и дизлайки. Голоса анонимные и требуют решения ALTCHA (proof of work).
- Приватные метрики по постам на `/metrics` (формат Prometheus) и дашборд Grafana.

Устройство описано в [SPEC.ru.md](SPEC.ru.md).

## Как всё связано

```
Obsidian ──Remotely Save──▶ бакет Garage ◀──опрос── hyperhack ──▶ ./data (SQLite)
                                                       │
                                                       └──зеркало──▶ ./vault
читатель ──▶ Caddy ──┬──▶ hyperhack :8080       (страницы, /api/*)
                     └──▶ ./vault/assets/       (/assets/*, отдаёт Caddy)
VictoriaMetrics ──scrape /metrics──▶ hyperhack ;  Grafana ──▶ VictoriaMetrics
```

Источник истины — бакет. Сервис опрашивает его и зеркалирует в `./vault`. Caddy отдаёт картинки прямо из зеркала, поэтому страницы и картинки работают, даже когда S3 недоступен.

## Что нужно

- Docker с Docker Compose.
- Бакет с S3 API по HTTPS. Сервис сделан и проверен под Garage.
- Caddy как обратный прокси, собранный с плагинами `caddy-ratelimit` и CrowdSec bouncer. См. [Caddy](#caddy).
- По желанию: VictoriaMetrics и Grafana для метрик, CrowdSec.

## Быстрый старт

1. Скачайте compose-файл и файл окружения:

   ```sh
   mkdir hyperhack && cd hyperhack
   curl -fsSLO https://raw.githubusercontent.com/Mimic890/hyperhack/main/docker-compose.yml
   curl -fsSL -o .env https://raw.githubusercontent.com/Mimic890/hyperhack/main/.env.example
   chmod 600 .env
   ```

   `docker-compose.yml` только скачивает выпущенный образ из GHCR и закрепляет последний релиз, например `ghcr.io/mimic890/hyperhack:0.1.0`. Тег образа — версия релиза без `v`, теги никогда не перезаписываются. Тега `latest` нет.

2. Создайте папки для данных. Контейнер работает от пользователя `10001`:

   ```sh
   mkdir data vault
   sudo chown 10001:10001 data vault
   ```

   Caddy должен иметь право читать `vault/assets/`. Сервис создаёт папки с правами `0755`, файлы — `0644`. Файл SQLite — `0600`.

3. Заполните `.env` (см. [Настройки](#настройки)). Каждый секрет генерируйте так:

   ```sh
   openssl rand -base64 48
   ```

4. Проверьте образ и запустите сервис. Тег возьмите из `image:` в `docker-compose.yml`:

   ```sh
   gh attestation verify oci://ghcr.io/mimic890/hyperhack:<version> -R Mimic890/hyperhack
   docker compose up -d
   docker compose ps        # дождитесь "healthy"
   docker compose logs -f
   ```

   При первом запуске сервису нужен бакет. Если S3 недоступен, а зеркала ещё нет, сервис завершается с кодом 1. При следующих запусках индекс строится из зеркала, и сервис не ждёт S3.

5. Настройте Caddy (см. [Caddy](#caddy)) и задайте `TRUSTED_PROXIES`.

Внутри контейнера сервис слушает порт `8080`. Compose публикует его только на `127.0.0.1:8080`. Не открывайте его в интернет: единственный публичный вход — Caddy.

## Garage: бакет и ключи

У блога свой бакет и два ключа: ключ только на чтение для сервиса и ключ на чтение и запись для Obsidian.

```sh
garage bucket create example-blog
garage key create hyperhack-ro
garage bucket allow --read example-blog --key hyperhack-ro
garage key create obsidian
garage bucket allow --read --write example-blog --key obsidian
```

Ключ `hyperhack-ro` впишите в `.env` как `S3_ACCESS_KEY` и `S3_SECRET_KEY`. `S3_ENDPOINT` должен начинаться с `https://`. Сервис использует path-style адресацию.

## Настройки

Все настройки берутся из переменных окружения в `.env`. В [.env.example](.env.example) перечислены все. Сервис проверяет каждое значение при старте. Если что-то неверно, он выводит все ошибки сразу и завершается с кодом 1. В ошибках называется переменная, но никогда её значение.

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `SITE_NAME` | обязательна | Название сайта в верхней панели, заголовках и RSS. |
| `SITE_URL` | обязательна | Публичный адрес, `https://example.com`. Нужен для canonical-ссылок, RSS, sitemap и проверки `Origin`. |
| `AUTHOR_NAME` | `SITE_NAME` | Автор в JSON-LD и RSS. |
| `DEFAULT_LANG` | `en` | Язык интерфейса и язык постов по умолчанию: `en` или `ru`. |
| `GITHUB_URL` | пусто | Ссылка на GitHub в верхней панели. |
| `S3_ENDPOINT` | обязательна | Адрес S3 API Garage, только `https://`. |
| `S3_REGION` | `garage` | Регион S3. |
| `S3_BUCKET` | обязательна | Имя бакета. |
| `S3_ACCESS_KEY`, `S3_SECRET_KEY` | обязательны | Ключ только на чтение. |
| `POLL_INTERVAL` | `60s` | Интервал опроса бакета, от 10s до 24h. |
| `REINDEX_TOKEN` | обязательна | Bearer-токен для `POST /reindex`. Не меньше 32 байт. |
| `ALTCHA_HMAC_KEY` | обязательна | Подпись задач ALTCHA. Не меньше 32 байт. |
| `VISITOR_SECRET` | обязательна | Ключ дневного хеша посетителя. Не меньше 32 байт. |
| `ALTCHA_EXPIRES` | `5m` | Время жизни задачи, от 10s до 1h. |
| `ALTCHA_MAX_NUMBER` | `100000` | Сложность задачи, от 1 000 до 10 000 000. |
| `READING_WPM` | `200` | Слов в минуту для времени чтения, от 50 до 1000. |
| `POPULAR_DAYS`, `POPULAR_LIMIT` | `30`, `5` | «Популярное»: больше всего просмотров за последние N дней, первые M постов. |
| `PAGE_SIZE` | `10` | Постов на странице на главной и на страницах тегов. |
| `RSS_LIMIT` | `20` | Постов в `/rss.xml`. |
| `DEFAULT_OG_IMAGE` | пусто | Картинка превью ссылки для страниц и постов без `cover`. Файл внутри `assets/`, например `assets/og.png`. |
| `INDEXNOW_KEY` | пусто | Включает пинги IndexNow (Bing, Яндекс). От 8 до 128 символов `a-z A-Z 0-9 -`. Отдаётся по адресу `/<key>.txt`. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` или `error`. |
| `LOG_FORMAT` | `json` | `json` для Docker и Loki, `text` для локального запуска. |
| `DATA_DIR` | `/data` | Папка SQLite (`hyperhack.db`). |
| `VAULT_DIR` | `/vault` | Папка зеркала бакета. |
| `RATE_LIMIT_VOTE` | `10/1m` | На клиента, `/api/altcha` и `/api/vote` вместе. |
| `RATE_LIMIT_BEACON` | `30/1m` | На клиента, `/api/beacon`. |
| `RATE_LIMIT_SEARCH` | `30/1m` | На клиента, `/search`. |
| `RATE_LIMIT_REINDEX` | `5/1m` | На клиента, `/reindex`. |
| `METRICS_ALLOW` | loopback и частные диапазоны | CIDR через запятую, которым можно читать `/metrics`. |
| `REINDEX_ALLOW` | loopback и частные диапазоны | CIDR через запятую, которым можно вызывать `/reindex`. |
| `TRUSTED_PROXIES` | пусто | IP или CIDR, которым разрешено задавать `X-Forwarded-For`. См. [Caddy](#caddy). |
| `MAX_POST_BYTES` | `1048576` (1 МиБ) | Файлы `.md` больше этого пропускаются. |
| `MAX_ASSET_BYTES` | `52428800` (50 МиБ) | Ассеты больше этого пропускаются. |
| `MAX_OBJECTS` | `10000` | Максимум ключей в бакете, включая игнорируемые. |

Лимиты записываются как `число/период`, например `10/1m`. Частные диапазоны: `127.0.0.0/8`, `::1/128`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` и `fc00::/7`.

## Как писать посты

Каждый файл `.md` — один пост, на любой глубине хранилища. Папки нужны только вам: адрес всегда `/posts/<slug>`, поэтому перенос файла не ломает ссылки. Сервис пропускает файлы и папки, имя которых начинается с `.` или `_`. Шаблоны и служебные заметки держите в `_templates/` или другой папке на `_`.

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

Текст поста…
```

| Поле | Тип | Обязательно | Назначение |
|---|---|---|---|
| `title` | строка | да | Заголовок поста. |
| `description` | строка | нет | Короткое описание для списка постов, RSS, мета-тегов и превью ссылок. |
| `date` | дата | да | Дата публикации. По ней сортируется «Новое». |
| `updated` | дата | нет | Дата обновления. Показывается в посте и в RSS. |
| `tags` | список | нет | Теги. Одна строка тоже подходит. |
| `cover` | строка | нет | Обложка, файл внутри `assets/`. |
| `slug` | строка | нет | Часть адреса. По умолчанию — из имени файла. |
| `lang` | строка | нет | Язык поста, две строчные буквы (`en`, `ru`). По умолчанию `DEFAULT_LANG`. |
| `draft` | bool | нет | `true` — пост не публикуется. По умолчанию `false`. |

Правила:

- Даты: `2026-11-03` или `2026-11-03T20:59`, как их пишет Obsidian. Время без часового пояса считается UTC.
- Без `title` или `date` или с неверным YAML пост пропускается, в лог пишется WARN. Неверные `date`, `updated`, `slug`, `lang`, `cover` или `draft` тоже пропускают пост.
- Slug из имени файла: строчные буквы, русские буквы транслитерируются (`ж` → `zh`, `х` → `kh`, `щ` → `shch`, `ъ` и `ь` выпадают), любая другая последовательность символов становится `-`. Slug во frontmatter должен соответствовать `^[a-z0-9-]{1,100}$`.
- Теги приводятся к нижнему регистру, дубли сливаются. Тег — от 1 до 50 букв, цифр, `-`, `_` или `/`. Не больше 20 тегов на пост.
- Если у двух постов одинаковый slug, побеждает первый по ключу объекта. Второй пропускается, в лог пишется ERROR.
- Неизвестные поля игнорируются, так что Obsidian может добавлять свои.
- Картинки — обычный Markdown, `![Подпись](assets/photo.webp)`. Путь `assets/…` на сайте становится `/assets/…`. Внешние картинки блокирует CSP.
- Разрешённые типы ассетов: `png jpg jpeg webp avif gif svg mp4 webm`.
- `assets/logo.svg`, если есть, заменяет встроенный логотип в верхней панели и фавиконку.

## Публикация из Obsidian

Заведите для блога отдельное хранилище Obsidian, чтобы личные заметки никогда не попали в бакет.

1. Установите плагин **Remotely Save** и выберите S3.
2. Endpoint, регион, бакет — те же, что в `.env`. Ключи — ключ `obsidian` на чтение и запись, не ключ сервиса.
3. Используйте path-style адреса.
4. Оставьте префикс на удалённой стороне пустым. У блога свой бакет.
5. Оставьте шифрование выключенным. С шифрованием сервис не сможет прочитать файлы.

Настройки Obsidian, **Files and links** («Файлы и ссылки»):

| Настройка | Значение |
|---|---|
| Use [[Wikilinks]] | выкл. |
| New link format | Absolute path in vault |
| Default location for new attachments | In the folder specified below → `assets` |

С этими настройками Obsidian вставляет картинки обычным Markdown: `![](assets/abstract-5.jpg)`. Вики-ссылки (`![[…]]`) не поддерживаются.

Новый пост появляется после следующего опроса, в пределах `POLL_INTERVAL`. Чтобы опубликовать сразу, вызовите `/reindex` из частной сети:

```sh
curl -X POST -H "Authorization: Bearer $REINDEX_TOKEN" http://127.0.0.1:8080/reindex
```

Вызов ждёт окончания синхронизации и возвращает `204`, когда всё готово, `401` при неверном токене, `403` для адреса вне `REINDEX_ALLOW`, `429` при превышении лимита и `503`, если синхронизация не удалась.

## Caddy

Caddy завершает TLS и добавляет защиту периметра: лимит запросов на клиента, CrowdSec, HSTS и лимит тела запроса. Он закрывает `/metrics` и `/reindex` и отдаёт `/assets/*` из зеркала.

Соберите Caddy с двумя плагинами:

```sh
xcaddy build v2.11.7 \
  --with github.com/mholt/caddy-ratelimit@v0.1.0 \
  --with github.com/hslatman/caddy-crowdsec-bouncer/http@v0.14.1
```

Это [Caddyfile.example](Caddyfile.example). Замените `example.com` и `/srv/hyperhack`:

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

- `CROWDSEC_API_URL` и `CROWDSEC_API_KEY` берутся из окружения Caddy при загрузке конфига. LAPI CrowdSec по умолчанию тоже занимает порт `8080`, поэтому, если оба работают на хосте, перенесите один из них.
- Сервис сам ставит CSP и заголовки безопасности. Caddy добавляет только HSTS и убирает `Server`.
- Общий лимит — 1200 запросов в минуту на IP клиента. Он считает страницы и вызовы API; `/static/*` и `/assets/*` не учитываются, поэтому картинки и шрифты его не расходуют.
- Без плагинов удалите строки `order`, `crowdsec`, `@limited` и `rate_limit`. Сервис всё равно ограничивает свои эндпоинты записи.

### TRUSTED_PROXIES

За Caddy сервис видит адрес Caddy, а не читателя. Без `TRUSTED_PROXIES` у всех читателей общий лимит запросов и общий хеш посетителя. С ним сервис берёт IP клиента из `X-Forwarded-For`.

Если Caddy работает на хосте, а порт опубликован на `127.0.0.1:8080`, запросы приходят в контейнер со шлюза сети Docker. Узнать его можно так:

```sh
docker network inspect hyperhack_default -f '{{(index .IPAM.Config 0).Gateway}}'
```

Впишите этот адрес в `TRUSTED_PROXIES` (например, `172.18.0.1`) и перезапустите сервис. Если Caddy работает в той же сети Compose, укажите адрес контейнера Caddy или подсеть сети.

## Мониторинг

`/metrics` отдаёт текстовый формат Prometheus на основном порту. Он отвечает только адресам из `METRICS_ALLOW`, а на публичном домене его закрывает Caddy. Собирайте метрики напрямую, не через Caddy.

Конфиг сбора для VictoriaMetrics (`-promscrape.config`):

```yaml
scrape_configs:
  - job_name: hyperhack
    scrape_interval: 30s
    static_configs:
      - targets: ["127.0.0.1:8080"]
```

Если VictoriaMetrics работает в Docker, укажите адрес, до которого она достаёт: например, подключите её к той же сети и используйте `hyperhack:8080`.

### Grafana

Дашборд — [grafana/hyperhack.json](grafana/hyperhack.json). В Grafana: **Dashboards → New → Import**, загрузите файл и выберите источник данных. Нужен источник данных типа Prometheus, направленный на VictoriaMetrics. Панели:

- Overview: число постов, просмотры, среднее время на странице, лайки, дизлайки, время с последней синхронизации.
- Posts: просмотры по дням, таблица по постам (просмотры, время, дочитывания, копирования кода, голоса), глубина чтения, голоса по дням.
- Readers: источник трафика, устройство, язык интерфейса, тема, переходы по внешним ссылкам.
- Search: запросы и запросы без результатов по часам, задержка, топ запросов, топ запросов без результатов.
- HTTP: запросы по маршрутам, 5xx и 429, задержка p95.
- Sync: объекты в бакете, неудачные синхронизации, неудачные загрузки, время с последней попытки.

Переменная `Post` фильтрует панели постов.

### Метрики

| Метрика | Тип | Метки |
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
| `hyperhack_search_top_queries`, `hyperhack_search_top_zero_result_queries` | gauge | `query` (топ 20) |
| `hyperhack_http_requests_total` | counter | `route`, `code` |
| `hyperhack_http_request_duration_seconds` | histogram | `route` |

Счётчики постов хранятся в SQLite и переживают перезапуск. Счётчики поиска и HTTP живут в памяти и после перезапуска начинают с нуля. `increase()` и `rate()` справляются с обоими случаями. Среднее время на странице — `read_seconds / read_views`.

Просмотры и глубина чтения считаются один раз на посетителя, пост и день. Посетитель — дневной HMAC от IP (для IPv6 — `/64`) и user agent. Хеш живёт только в памяти, сырые IP никогда не хранятся и не пишутся в лог. Читатели без JS не учитываются.

## Логи

JSON в stdout, одна запись на строку, с полями `time`, `level`, `msg` и `component` (`main`, `sync`, `http`, `render`, `votes`, `metrics`, `store`). `LOG_LEVEL=debug` добавляет подробности синхронизации и каждый HTTP-запрос. Секреты, сырые IP и хеши посетителей никогда не пишутся в лог.

## Резервные копии

- В `data/` лежит база SQLite: просмотры, голоса и статистика поиска. Её нужно копировать. Для целостной копии остановите контейнер или копируйте `hyperhack.db` вместе с `hyperhack.db-wal` и `hyperhack.db-shm`.
- `vault/` — зеркало бакета, копировать его не нужно. Удалите его, чтобы запустить полную пересинхронизацию.
- Источник постов — бакет. Делайте его копии средствами Garage или держите хранилище Obsidian в git.

## Смена секретов

Поменяйте значение в `.env` и перезапустите сервис: `docker compose up -d`.

- `REINDEX_TOKEN`: одновременно обновите всех, кто вызывает `/reindex`.
- `ALTCHA_HMAC_KEY`: задачи, выданные до перезапуска, не пройдут, читателю придётся нажать ещё раз. Голоса сохраняются.
- `VISITOR_SECRET`: влияет только на отсев повторных просмотров, а он живёт в памяти и всё равно сбрасывается при перезапуске. Читатель, уже учтённый в этот день, может быть учтён ещё раз.
- Ключи S3: создайте в Garage новый ключ только на чтение, впишите его в `.env`, перезапустите, убедитесь по логам, что синхронизация прошла, и удалите старый ключ.

Cookie голосов (`vid`) случайные и не зависят ни от одного секрета.

## Обновление

Версии перечислены в [тегах](https://github.com/Mimic890/hyperhack/tags). Релизу `v0.2.0` соответствует образ `ghcr.io/mimic890/hyperhack:0.2.0`.

1. Сделайте резервную копию `data/` (см. [Резервные копии](#резервные-копии)).
2. В `docker-compose.yml` укажите новый тег в `image:`. Если вы не меняли файл, можно вместо этого скачать его заново из `main`: там закреплён последний релиз.
3. Проверьте новый образ, скачайте его и перезапустите сервис:

   ```sh
   gh attestation verify oci://ghcr.io/mimic890/hyperhack:<version> -R Mimic890/hyperhack
   docker compose pull
   docker compose up -d
   docker compose ps        # дождитесь "healthy"
   ```

Чтобы откатиться, верните старый тег и выполните `docker compose up -d`. Если старая версия не запускается, восстановите `data/` из резервной копии.

## Разработка

Go 1.27 (`toolchain go1.27.3`).

```sh
set -a; . ./.env; set +a
DATA_DIR=./data VAULT_DIR=./vault LOG_FORMAT=text go run .
# или в Docker: docker compose -f compose.dev.yml up --build
```

`compose.dev.yml` собирает образ из рабочей копии как `hyperhack:dev`, остальные настройки берёт из `docker-compose.yml`. Compose сам его не подключает, поэтому в продакшене остаётся выпущенный образ.

Проверки, перед каждым коммитом и в CI:

```sh
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
go test -race ./...
```

Тест ALTCHA запускает браузерный решатель в Node, если `node` установлен. Если задана переменная `CI`, отсутствие `node` роняет тест.

Изменения: одно изменение — одна ветка и один PR, Conventional Commits, каждое решение по устройству — в обоих файлах SPEC.

## Безопасность

Об уязвимостях сообщайте приватно, см. [SECURITY.ru.md](SECURITY.ru.md).

## Лицензия

[AGPL-3.0](LICENSE).
