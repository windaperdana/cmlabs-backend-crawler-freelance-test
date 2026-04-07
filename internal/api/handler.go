package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cmlabs/crawler/internal/crawler"
	"github.com/cmlabs/crawler/internal/storage"
)

// CrawlRequest is the JSON body for POST /crawl.
type CrawlRequest struct {
	URL           string `json:"url"`
	MaxDepth      int    `json:"max_depth"`
	WaitAfterLoad int    `json:"wait_after_load_ms"`
	Timeout       int    `json:"timeout_ms"`
	SameDomain    *bool  `json:"same_domain"`
	OutputDir     string `json:"output_dir"`
}

// CrawlResponse is the JSON body returned by POST /crawl.
type CrawlResponse struct {
	SeedURL    string     `json:"seed_url"`
	OutputDir  string     `json:"output_dir"`
	IndexFile  string     `json:"index_file"`
	PageCount  int        `json:"page_count"`
	Pages      []PageInfo `json:"pages"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt time.Time  `json:"finished_at"`
}

// PageInfo summarises one crawled page.
type PageInfo struct {
	URL       string    `json:"url"`
	File      string    `json:"file"`
	LinkCount int       `json:"link_count"`
	Error     string    `json:"error,omitempty"`
	CrawledAt time.Time `json:"crawled_at"`
}

// ErrorResponse is used for all error payloads.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Handler returns an http.ServeMux wired with all routes.
func Handler() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/crawl", handleCrawl)
	mux.HandleFunc("/health", handleHealth)
	outDir := defaultOutputDir()
	mux.Handle("/results/", http.StripPrefix("/results/", http.FileServer(http.Dir(outDir))))
	return mux
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleCrawl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "only POST is accepted")
		return
	}

	var req CrawlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if err := validateRequest(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	opts := crawler.DefaultOptions()
	opts.MaxDepth = req.MaxDepth
	if req.WaitAfterLoad > 0 {
		opts.WaitAfterLoad = time.Duration(req.WaitAfterLoad) * time.Millisecond
	}
	if req.Timeout > 0 {
		opts.Timeout = time.Duration(req.Timeout) * time.Millisecond
	}
	if req.SameDomain != nil {
		opts.SameDomain = *req.SameDomain
	}

	outDir := req.OutputDir
	if outDir == "" {
		outDir = filepath.Join(defaultOutputDir(), sanitizeDirName(req.URL))
	}

	log.Printf("[crawl] start url=%s depth=%d outDir=%s", req.URL, req.MaxDepth, outDir)
	startedAt := time.Now()

	c, err := crawler.New(opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "crawler init: "+err.Error())
		return
	}
	defer c.Close()

	pages, err := c.CrawlSite(req.URL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "crawl: "+err.Error())
		return
	}

	finishedAt := time.Now()

	var pageInfos []PageInfo
	var indexRows []storage.IndexRow

	for i, p := range pages {
		log.Printf("[crawl] saving page %d/%d url=%s", i+1, len(pages), p.URL)
		filename, saveErr := storage.SavePage(outDir, p.URL, p.HTML, p.StatusCode, p.Links, p.CrawledAt, p.Error)
		if saveErr != nil {
			log.Printf("[crawl] save error %s: %v", p.URL, saveErr)
		}

		errStr := ""
		if p.Error != nil {
			errStr = p.Error.Error()
		}

		pageInfos = append(pageInfos, PageInfo{
			URL:       p.URL,
			File:      filename,
			LinkCount: len(p.Links),
			Error:     errStr,
			CrawledAt: p.CrawledAt,
		})
		indexRows = append(indexRows, storage.IndexRow{
			URL:       p.URL,
			ErrorMsg:  errStr,
			Links:     p.Links,
			CrawledAt: p.CrawledAt.Format(time.RFC1123),
			Filename:  filename,
		})
	}

	if err := storage.SaveIndex(outDir, req.URL, indexRows); err != nil {
		log.Printf("[crawl] index write error: %v", err)
	}

	resp := CrawlResponse{
		SeedURL:    req.URL,
		OutputDir:  outDir,
		IndexFile:  filepath.Join(outDir, "index.html"),
		PageCount:  len(pages),
		Pages:      pageInfos,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}

	log.Printf("[crawl] done pages=%d elapsed=%s", len(pages), finishedAt.Sub(startedAt))
	writeJSON(w, http.StatusOK, resp)
}

func validateRequest(req *CrawlRequest) error {
	if req.URL == "" {
		return errors.New("url is required")
	}
	parsed, err := url.ParseRequestURI(req.URL)
	if err != nil {
		return errors.New("url is not a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("url must use http or https scheme")
	}
	host := strings.ToLower(parsed.Hostname())
	blocked := []string{
		"localhost", "127.", "0.0.0.0", "::1",
		"169.254.", "10.", "192.168.",
		"172.16.", "172.17.", "172.18.", "172.19.", "172.20.",
		"172.21.", "172.22.", "172.23.", "172.24.", "172.25.",
		"172.26.", "172.27.", "172.28.", "172.29.", "172.30.", "172.31.",
	}
	for _, b := range blocked {
		if strings.HasPrefix(host, b) || host == b {
			return errors.New("crawling private/loopback addresses is not allowed")
		}
	}
	if req.MaxDepth < 0 {
		req.MaxDepth = 0
	}
	if req.MaxDepth > 5 {
		req.MaxDepth = 5
	}
	if req.WaitAfterLoad < 0 {
		req.WaitAfterLoad = 0
	}
	if req.WaitAfterLoad > 30000 {
		req.WaitAfterLoad = 30000
	}
	if req.OutputDir != "" {
		clean := filepath.Clean(req.OutputDir)
		if strings.Contains(clean, "..") {
			return errors.New("output_dir must not contain path traversal sequences")
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, ErrorResponse{Error: msg})
}

func defaultOutputDir() string {
	if dir := os.Getenv("CRAWLER_OUTPUT_DIR"); dir != "" {
		return dir
	}
	return "output"
}

var sanitizeReplacer = strings.NewReplacer(
	"https://", "",
	"http://", "",
	"/", "_",
	":", "_",
	"?", "_",
	"&", "_",
	"=", "_",
	".", "_",
)

func sanitizeDirName(rawURL string) string {
	s := sanitizeReplacer.Replace(rawURL)
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}
