package storage

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var resultTmpl = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>Crawl Result - {{.URL}}</title>
  <style>
    body{font-family:system-ui,sans-serif;margin:0;background:#f5f5f5;color:#333}
    header{background:#1a73e8;color:#fff;padding:1rem 2rem}
    header h1{margin:0;font-size:1.2rem}
    .meta{background:#fff;border-bottom:1px solid #ddd;padding:.75rem 2rem;font-size:.9rem}
    .meta span{margin-right:2rem}
    .ok{color:#188038;font-weight:bold}
    .err{color:#c5221f;font-weight:bold}
    .content{padding:2rem}
    .frame{width:100%;height:70vh;border:1px solid #ddd;border-radius:4px;background:#fff;resize:vertical;overflow:auto}
    pre{margin:0;padding:1rem;white-space:pre-wrap;word-break:break-all;font-size:.85rem}
    .links{margin-top:1.5rem}
    .links h2{font-size:1rem;margin-bottom:.5rem}
    .links ul{list-style:disc;padding-left:1.5rem}
    .links a{color:#1a73e8}
    .errbox{background:#fce8e6;border:1px solid #f5c6cb;border-radius:4px;padding:1rem;color:#c5221f;margin-top:1rem}
  </style>
</head>
<body>
<header><h1>CMLabs Web Crawler - Result</h1></header>
<div class="meta">
  <span><strong>URL:</strong> <a href="{{.URL}}" target="_blank" style="color:inherit">{{.URL}}</a></span>
  <span><strong>Crawled at:</strong> {{.CrawledAt}}</span>
  {{if .Error}}<span class="err">ERROR</span>{{else}}<span class="ok">HTTP {{.StatusCode}}</span>{{end}}
</div>
<div class="content">
  {{if .Error}}<div class="errbox"><strong>Error:</strong> {{.Error}}</div>
  {{else}}<div class="frame"><pre>{{.HTMLEscaped}}</pre></div>{{end}}
  {{if .Links}}<div class="links"><h2>Discovered links ({{len .Links}})</h2><ul>
    {{range .Links}}<li><a href="{{.}}" target="_blank">{{.}}</a></li>{{end}}
  </ul></div>{{end}}
</div>
</body>
</html>`))

var indexTmpl *template.Template

func init() {
	indexTmpl = template.Must(template.New("index").Funcs(template.FuncMap{
		"inc": func(i int) int { return i + 1 },
	}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>Crawl Report - {{.SeedURL}}</title>
  <style>
    body{font-family:system-ui,sans-serif;margin:0;background:#f5f5f5;color:#333}
    header{background:#1a73e8;color:#fff;padding:1rem 2rem}
    header h1{margin:0}
    header p{margin:.25rem 0 0;font-size:.9rem;opacity:.85}
    table{width:100%;border-collapse:collapse;background:#fff}
    th{background:#e8f0fe;text-align:left;padding:.6rem 1rem;font-size:.85rem}
    td{padding:.6rem 1rem;border-bottom:1px solid #e0e0e0;font-size:.85rem}
    tr:hover td{background:#f0f4ff}
    a{color:#1a73e8}
    .ok{color:#188038;font-weight:bold}
    .err{color:#c5221f;font-weight:bold}
    .wrap{padding:2rem}
  </style>
</head>
<body>
<header>
  <h1>CMLabs Web Crawler - Report</h1>
  <p>Seed: {{.SeedURL}} | Pages: {{len .Pages}} | Generated: {{.GeneratedAt}}</p>
</header>
<div class="wrap">
<table>
  <thead><tr><th>#</th><th>URL</th><th>Status</th><th>Links</th><th>Crawled at</th><th>Detail</th></tr></thead>
  <tbody>
  {{range $i,$p := .Pages}}
  <tr>
    <td>{{inc $i}}</td>
    <td><a href="{{$p.URL}}" target="_blank">{{$p.URL}}</a></td>
    <td>{{if $p.ErrorMsg}}<span class="err">ERROR</span>{{else}}<span class="ok">200</span>{{end}}</td>
    <td>{{len $p.Links}}</td>
    <td>{{$p.CrawledAt}}</td>
    <td><a href="{{$p.Filename}}">view</a></td>
  </tr>
  {{end}}
  </tbody>
</table>
</div>
</body>
</html>`))
}

// PageData is passed to resultTmpl.
type PageData struct {
	URL         string
	StatusCode  int
	HTMLEscaped string
	Links       []string
	CrawledAt   string
	Error       string
}

// IndexRow is one row in the index table.
type IndexRow struct {
	URL       string
	ErrorMsg  string
	Links     []string
	CrawledAt string
	Filename  string
}

// IndexData is passed to indexTmpl.
type IndexData struct {
	SeedURL     string
	GeneratedAt string
	Pages       []IndexRow
}

// SavePage writes the HTML result for a single page to outDir.
func SavePage(outDir, pageURL, html string, statusCode int, links []string, crawledAt time.Time, crawlErr error) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	filename := urlToFilename(pageURL)
	dest := filepath.Join(outDir, filename)

	data := PageData{
		URL:        pageURL,
		StatusCode: statusCode,
		Links:      links,
		CrawledAt:  crawledAt.Format(time.RFC1123),
	}
	if crawlErr != nil {
		data.Error = crawlErr.Error()
	} else {
		data.HTMLEscaped = html
	}

	var buf bytes.Buffer
	if err := resultTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execute: %w", err)
	}
	if err := os.WriteFile(dest, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", dest, err)
	}
	return filename, nil
}

// SaveIndex writes an index.html summary to outDir.
func SaveIndex(outDir, seedURL string, rows []IndexRow) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	data := IndexData{
		SeedURL:     seedURL,
		GeneratedAt: time.Now().Format(time.RFC1123),
		Pages:       rows,
	}

	var buf bytes.Buffer
	if err := indexTmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("index template: %w", err)
	}
	dest := filepath.Join(outDir, "index.html")
	if err := os.WriteFile(dest, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}
	return nil
}

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func urlToFilename(rawURL string) string {
	s := rawURL
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimSuffix(s, "/")
	s = nonAlnum.ReplaceAllString(s, "_")
	if len(s) > 200 {
		s = s[:200]
	}
	return s + ".html"
}
