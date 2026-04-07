package crawler

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/chromedp"
)

// Options holds crawler configuration.
type Options struct {
	Timeout       time.Duration
	WaitAfterLoad time.Duration
	UserAgent     string
	MaxDepth      int
	SameDomain    bool
}

// DefaultOptions returns sensible defaults.
func DefaultOptions() Options {
	return Options{
		Timeout:       60 * time.Second,
		WaitAfterLoad: 2 * time.Second,
		UserAgent:     "Mozilla/5.0 (compatible; CMLabs-Crawler/1.0)",
		MaxDepth:      0,
		SameDomain:    true,
	}
}

// Page holds the result for a single crawled URL.
type Page struct {
	URL        string
	StatusCode int
	HTML       string
	Links      []string
	CrawledAt  time.Time
	Error      error
}

// Crawler performs headless-browser based crawling.
type Crawler struct {
	opts        Options
	allocCtx    context.Context
	allocCancel context.CancelFunc
}

// New creates a Crawler. Call Close() when done.
func New(opts Options) (*Crawler, error) {
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.UserAgent(opts.UserAgent),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)

	return &Crawler{
		opts:        opts,
		allocCtx:    allocCtx,
		allocCancel: allocCancel,
	}, nil
}

// Close releases browser resources.
func (c *Crawler) Close() {
	c.allocCancel()
}

// CrawlSite crawls the seed URL and discovered links up to MaxDepth.
func (c *Crawler) CrawlSite(seedURL string) ([]Page, error) {
	parsed, err := url.ParseRequestURI(seedURL)
	if err != nil {
		return nil, fmt.Errorf("invalid seed URL: %w", err)
	}

	visited := make(map[string]bool)
	var results []Page

	type entry struct {
		u     string
		depth int
	}
	queue := []entry{{u: seedURL, depth: 0}}
	log.Printf("[crawler] starting site crawl seed=%s maxDepth=%d", seedURL, c.opts.MaxDepth)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if visited[cur.u] {
			continue
		}
		visited[cur.u] = true

		log.Printf("[crawler] crawling url=%s depth=%d queued=%d visited=%d", cur.u, cur.depth, len(queue), len(visited))
		page := c.CrawlPage(cur.u)
		results = append(results, page)

		if page.Error != nil {
			log.Printf("[crawler] error url=%s err=%v", cur.u, page.Error)
		} else {
			log.Printf("[crawler] done url=%s links=%d htmlBytes=%d", cur.u, len(page.Links), len(page.HTML))
		}

		if page.Error != nil || cur.depth >= c.opts.MaxDepth {
			continue
		}

		for _, link := range page.Links {
			if visited[link] {
				continue
			}
			if c.opts.SameDomain && !sameDomain(parsed, link) {
				continue
			}
			queue = append(queue, entry{u: link, depth: cur.depth + 1})
		}
	}

	log.Printf("[crawler] site crawl complete seed=%s totalPages=%d", seedURL, len(results))
	return results, nil
}

// CrawlPage fetches and renders a single URL using a headless browser.
func (c *Crawler) CrawlPage(targetURL string) Page {
	start := time.Now()
	result := Page{
		URL:       targetURL,
		CrawledAt: start,
	}

	ctx, cancel := chromedp.NewContext(c.allocCtx)
	defer cancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer timeoutCancel()

	var outerHTML string

	err := chromedp.Run(timeoutCtx,
		chromedp.Navigate(targetURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForNetworkIdle(ctx, c.opts.WaitAfterLoad)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			node, err := dom.GetDocument().Do(ctx)
			if err != nil {
				return err
			}
			outerHTML, err = dom.GetOuterHTML().WithNodeID(node.NodeID).Do(ctx)
			return err
		}),
	)

	if err != nil {
		result.Error = fmt.Errorf("chromedp: %w", err)
		log.Printf("[crawler] page failed url=%s elapsed=%s err=%v", targetURL, time.Since(start).Round(time.Millisecond), err)
		return result
	}

	result.StatusCode = 200
	result.HTML = outerHTML
	result.Links = extractLinks(outerHTML, targetURL)
	log.Printf("[crawler] page ok url=%s elapsed=%s links=%d htmlBytes=%d", targetURL, time.Since(start).Round(time.Millisecond), len(result.Links), len(outerHTML))
	return result
}

// waitForNetworkIdle polls document.readyState and then waits extra settle time.
func waitForNetworkIdle(ctx context.Context, extra time.Duration) error {
	var readyState string
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := chromedp.Evaluate(`document.readyState`, &readyState).Do(ctx); err != nil {
			return err
		}
		if readyState == "complete" {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	timer := time.NewTimer(extra)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// extractLinks parses href attributes from raw HTML.
func extractLinks(html, baseURL string) []string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var links []string

	remaining := html
	for {
		idx := strings.Index(remaining, `href="`)
		if idx == -1 {
			break
		}
		remaining = remaining[idx+6:]
		end := strings.IndexByte(remaining, '"')
		if end == -1 {
			break
		}
		raw := remaining[:end]
		remaining = remaining[end+1:]

		if strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "mailto:") ||
			strings.HasPrefix(raw, "javascript:") || raw == "" {
			continue
		}

		ref, err := url.Parse(raw)
		if err != nil {
			continue
		}
		abs := base.ResolveReference(ref).String()
		if i := strings.IndexByte(abs, '#'); i != -1 {
			abs = abs[:i]
		}
		if !seen[abs] {
			seen[abs] = true
			links = append(links, abs)
		}
	}
	return links
}

// sameDomain returns true if rawURL belongs to the same host as base.
func sameDomain(base *url.URL, rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), base.Hostname())
}
