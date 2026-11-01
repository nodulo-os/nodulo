// Package search provides a small, privacy-conscious web search client.
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const (
	defaultEndpoint = "https://html.duckduckgo.com/html/"
	maxHTMLBytes    = 4 << 20
)

// SearchResult is a title, canonical URL, and short search-engine snippet.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearXNGClient queries an administrator-controlled SearXNG instance.
type SearXNGClient struct {
	HTTPClient *http.Client
	Endpoint   string
}

func NewSearXNGClient(endpoint string) *SearXNGClient {
	return &SearXNGClient{HTTPClient: &http.Client{Timeout: 8 * time.Second}, Endpoint: strings.TrimSpace(endpoint)}
}

// SearchWeb requests JSON results from the configured SearXNG instance.
func (c *SearXNGClient) SearchWeb(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("web search query is empty")
	}
	if limit <= 0 {
		return []SearchResult{}, nil
	}
	if limit > 10 {
		limit = 10
	}
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(c.Endpoint), "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("SearXNG endpoint must be an HTTP(S) base URL without credentials, query, or fragment")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/search"
	values := base.Query()
	values.Set("q", query)
	values.Set("format", "json")
	base.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create SearXNG request: %w", err)
	}
	req.Header.Set("User-Agent", "Nodulo/1.0 (+https://github.com)")
	req.Header.Set("Accept", "application/json")
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request SearXNG search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("SearXNG search returned %s", resp.Status)
	}
	var payload struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxHTMLBytes)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode SearXNG response: %w", err)
	}
	results := make([]SearchResult, 0, min(limit, len(payload.Results)))
	for _, item := range payload.Results {
		if len(results) >= limit {
			break
		}
		canonical := canonicalURL(item.URL)
		title := strings.TrimSpace(item.Title)
		if canonical == "" || title == "" {
			continue
		}
		results = append(results, SearchResult{Title: title, URL: canonical, Snippet: truncateRunes(strings.TrimSpace(item.Content), 1200)})
	}
	return results, nil
}

// DuckDuckGoClient fetches only DuckDuckGo's HTML results page; it does not
// crawl result pages or submit the user's conversation and attachments.
type DuckDuckGoClient struct {
	HTTPClient *http.Client
	Endpoint   string
}

func NewDuckDuckGoClient() *DuckDuckGoClient {
	return &DuckDuckGoClient{
		HTTPClient: &http.Client{Timeout: 8 * time.Second},
		Endpoint:   defaultEndpoint,
	}
}

// SearchWeb is a convenience wrapper for callers that do not need to manage
// cancellation explicitly. Long-running server requests should use the client
// method so their request context can cancel the search.
func SearchWeb(query string, limit int) ([]SearchResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return NewDuckDuckGoClient().SearchWeb(ctx, query, limit)
}

// SearchWeb queries DuckDuckGo and returns up to limit organic results.
func (c *DuckDuckGoClient) SearchWeb(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("web search query is empty")
	}
	if limit <= 0 {
		return []SearchResult{}, nil
	}
	if limit > 10 {
		limit = 10
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse DuckDuckGo endpoint: %w", err)
	}
	values := parsed.Query()
	values.Set("q", query)
	parsed.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create DuckDuckGo request: %w", err)
	}
	req.Header.Set("User-Agent", "Nodulo/1.0 (+https://github.com)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request DuckDuckGo search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("DuckDuckGo search returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTMLBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read DuckDuckGo results: %w", err)
	}
	if len(body) > maxHTMLBytes {
		return nil, fmt.Errorf("DuckDuckGo response exceeds %d bytes", maxHTMLBytes)
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse DuckDuckGo results: %w", err)
	}
	results := make([]SearchResult, 0, limit)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil || len(results) >= limit {
			return
		}
		if node.Type == html.ElementNode && hasClass(node, "result") {
			result := parseResult(node)
			if result.URL != "" && result.Title != "" {
				results = append(results, result)
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
			if len(results) >= limit {
				return
			}
		}
	}
	walk(doc)
	return results, nil
}

func parseResult(root *html.Node) SearchResult {
	var result SearchResult
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			if hasClass(node, "result__a") && result.URL == "" {
				for _, attr := range node.Attr {
					if attr.Key == "href" {
						result.URL = canonicalURL(attr.Val)
						break
					}
				}
				result.Title = strings.TrimSpace(nodeText(node))
			} else if hasClass(node, "result__snippet") && result.Snippet == "" {
				result.Snippet = truncateRunes(strings.TrimSpace(nodeText(node)), 1200)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return result
}

func truncateRunes(value string, maxRunes int) string {
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}

func canonicalURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if redirected := parsed.Query().Get("uddg"); redirected != "" {
		parsed, err = url.Parse(redirected)
		if err != nil {
			return ""
		}
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return ""
	}
	if parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

func nodeText(node *html.Node) string {
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			text.WriteString(current.Data)
			text.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(text.String()), " ")
}

func hasClass(node *html.Node, class string) bool {
	for _, attr := range node.Attr {
		if attr.Key == "class" {
			for _, item := range strings.Fields(attr.Val) {
				if item == class {
					return true
				}
			}
		}
	}
	return false
}
