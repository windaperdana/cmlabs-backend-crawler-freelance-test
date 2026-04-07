# CMLabs Web Crawler

A Go HTTP API that crawls websites — including **SPA**, **SSR**, and **PWA** — using a headless Chromium browser (via [chromedp](https://github.com/chromedp/chromedp)).  
Crawl results are saved as **HTML files** with a linked index page.

## Features

| Feature | Detail |
|---|---|
| SPA / PWA support | Uses headless Chrome; waits for `document.readyState == "complete"` plus a configurable settle delay so JS frameworks finish hydrating |
| SSR support | Standard HTML rendered server-side is captured identically |
| Configurable depth | `max_depth 0` = seed URL only; up to `5` levels deep |
| Same-domain filtering | Optionally restrict crawling to the seed hostname |
| HTML output | Each page is saved as a standalone HTML file with the rendered source and discovered links |
| Index page | An `index.html` summary listing all crawled pages |
| REST API | Simple JSON API — see endpoints below |
| SSRF protection | Blocks crawling of private / loopback addresses |

## Requirements

- Go 1.26+
- Google Chrome or Chromium installed in a standard path  
  (chromedp will find it automatically on macOS, Linux, and Windows)

## Quick start

```bash
# Clone & build
git clone <repo>
cd cmlabs-backend-crawler-freelance-test
make build

# Run (default port :9000)
./crawler

# Or with environment variables
CRAWLER_ADDR=:9000 CRAWLER_OUTPUT_DIR=/tmp/crawl-output ./crawler
```

## API Reference

### `GET /health`

Liveness probe.

**Response**
```json
{ "status": "ok" }
```

---

### `POST /crawl`

Crawl a website.  
Content-Type: `application/json`

**Request body**

| Field | Type | Required | Description |
|---|---|---|---|
| `url` | string | yes | Seed URL to crawl (http/https) |
| `max_depth` | int | no | Crawl depth (0–5, default 0) |
| `wait_after_load_ms` | int | no | Extra JS settle time in ms (default 2000) |
| `same_domain` | bool | no | Restrict to seed hostname (default true) |
| `output_dir` | string | no | Custom output directory path |

**Example request**
```bash
curl -X POST http://localhost:9000/crawl \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://example.com",
    "max_depth": 1,
    "wait_after_load_ms": 3000,
    "same_domain": true
  }'
```

**Example response**
```json
{
  "seed_url": "https://example.com",
  "output_dir": "output/example_com",
  "index_file": "output/example_com/index.html",
  "page_count": 3,
  "pages": [
    {
      "url": "https://example.com",
      "file": "example_com.html",
      "link_count": 2,
      "crawled_at": "2026-04-07T10:00:00Z"
    }
  ],
  "started_at": "2026-04-07T10:00:00Z",
  "finished_at": "2026-04-07T10:00:05Z"
}
```

---

### `GET /results/`

Serves the generated HTML output directory so you can browse crawl results in a browser:

```
http://localhost:9000/results/<output-subdir>/index.html
```

## Project structure

```
.
├── cmd/
│   └── crawler/
│       └── main.go          # Entry point / HTTP server
├── internal/
│   ├── api/
│   │   └── handler.go       # REST endpoints, request validation
│   ├── crawler/
│   │   └── crawler.go       # Headless browser crawling logic
│   └── storage/
│       └── storage.go       # HTML file / index generation
├── Makefile
├── go.mod
└── README.md
```

## How SPA/PWA/SSR rendering works

1. chromedp launches a real headless Chrome instance.
2. `chromedp.Navigate()` loads the URL.
3. The code polls `document.readyState` until `"complete"`.
4. An additional **wait_after_load** dwell period lets client-side frameworks (React, Vue, Angular, Next.js, Nuxt, etc.) finish their initial render and data-fetching.
5. The **full DOM** (not raw HTML source) is captured via `dom.GetOuterHTML` — this includes all dynamically injected content.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `CRAWLER_ADDR` | `:8080` | HTTP listen address |
| `CRAWLER_OUTPUT_DIR` | `output` | Root directory for saved HTML results |
