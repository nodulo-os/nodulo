package search

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"core-nodulo/pkg/extractor"
	"golang.org/x/net/html"
)

const (
	deepSearchEndpoint = "https://html.duckduckgo.com/html/"
	deepSearchLimit    = 5
	maxSearchHTMLBytes = 4 << 20
	maxPageHTMLBytes   = 8 << 20
	maxPageTextRunes   = 15_000
)

const browserUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// BlockedPageObservation is a recoverable observation for anti-bot responses
// and page-read timeouts. The agent should search for another source.
const BlockedPageObservation = "Erro: Acesso bloqueado por proteção anti-bot. Faça uma nova Action: search_web buscando outra fonte."

// DeepScraper searches DuckDuckGo and fetches public web pages for research.
// Its client blocks private-network destinations and validates redirects.
type DeepScraper struct {
	HTTPClient *http.Client
	UserAgent  string
	Endpoint   string
}

func NewDeepScraper() *DeepScraper {
	return &DeepScraper{
		HTTPClient: extractor.NewSafePublicHTTPClient(15 * time.Second),
		UserAgent:  browserUserAgent,
		Endpoint:   deepSearchEndpoint,
	}
}

// SearchWeb returns up to five organic DuckDuckGo results.
func (s *DeepScraper) SearchWeb(ctx context.Context, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("termos de pesquisa vazios")
	}
	if len([]rune(query)) > 500 {
		query = string([]rune(query)[:500])
	}
	endpoint := s.Endpoint
	if endpoint == "" {
		endpoint = deepSearchEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "html.duckduckgo.com" {
		return nil, fmt.Errorf("endpoint DuckDuckGo inválido")
	}
	values := parsed.Query()
	values.Set("q", query)
	parsed.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("criar requisição de pesquisa: %w", err)
	}
	request.Header.Set("User-Agent", s.userAgent())
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	response, err := s.client().Do(request)
	if err != nil {
		return nil, fmt.Errorf("consultar DuckDuckGo: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("DuckDuckGo respondeu %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSearchHTMLBytes+1))
	if err != nil {
		return nil, fmt.Errorf("ler resultados do DuckDuckGo: %w", err)
	}
	if len(body) > maxSearchHTMLBytes {
		return nil, fmt.Errorf("resposta de pesquisa excedeu o limite permitido")
	}
	document, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("analisar resultados do DuckDuckGo: %w", err)
	}
	results := make([]SearchResult, 0, deepSearchLimit)
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node == nil || len(results) >= deepSearchLimit {
			return
		}
		if node.Type == html.ElementNode && hasClass(node, "result") {
			result := parseResult(node)
			if result.Title != "" && result.URL != "" {
				results = append(results, result)
				return
			}
		}
		for child := node.FirstChild; child != nil && len(results) < deepSearchLimit; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	return results, nil
}

// ReadPage downloads a public HTML page, removes navigation and other
// non-content elements, then returns at most the first 15,000 Unicode runes.
func (s *DeepScraper) ReadPage(ctx context.Context, rawURL string) (string, error) {
	if err := extractor.ValidatePublicURL(ctx, rawURL); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", ctx.Err()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || isPageReadTimeout(err) {
			return BlockedPageObservation, nil
		}
		return "", fmt.Errorf("URL de pesquisa não permitida: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("criar requisição da página: %w", err)
	}
	request.Header.Set("User-Agent", s.userAgent())
	request.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9")
	response, err := s.client().Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", ctx.Err()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || isPageReadTimeout(err) {
			return BlockedPageObservation, nil
		}
		return "", fmt.Errorf("buscar página: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable:
			return BlockedPageObservation, nil
		}
		return "", fmt.Errorf("página respondeu %s", response.Status)
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "" && mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		return "", fmt.Errorf("tipo de conteúdo não é HTML")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPageHTMLBytes+1))
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", ctx.Err()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || isPageReadTimeout(err) {
			return BlockedPageObservation, nil
		}
		return "", fmt.Errorf("ler HTML da página: %w", err)
	}
	if len(body) > maxPageHTMLBytes {
		return "", fmt.Errorf("HTML da página excedeu o limite permitido")
	}
	text, err := extractSemanticText(strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", fmt.Errorf("a página não contém texto semântico extraível")
	}
	if utf8.RuneCountInString(text) > maxPageTextRunes {
		text = string([]rune(text)[:maxPageTextRunes])
	}
	return text, nil
}

func isPageReadTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

func (s *DeepScraper) client() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return extractor.NewSafePublicHTTPClient(15 * time.Second)
}

func (s *DeepScraper) userAgent() string {
	if value := strings.TrimSpace(s.UserAgent); value != "" {
		return value
	}
	return browserUserAgent
}

func extractSemanticText(reader io.Reader) (string, error) {
	document, err := html.Parse(reader)
	if err != nil {
		return "", fmt.Errorf("analisar HTML da página: %w", err)
	}
	main := findMainContent(document)
	if main == nil {
		main = findBody(document)
	}
	if main == nil {
		main = document
	}
	var output strings.Builder
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			tag := strings.ToLower(node.Data)
			if isRemovedElement(node) {
				return
			}
			if isTextBlock(tag) && output.Len() > 0 {
				output.WriteByte('\n')
			}
		}
		if node.Type == html.TextNode {
			value := strings.Join(strings.Fields(node.Data), " ")
			if value != "" {
				output.WriteString(value)
				output.WriteByte(' ')
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
		if node.Type == html.ElementNode && isTextBlock(strings.ToLower(node.Data)) {
			output.WriteByte('\n')
		}
	}
	visit(main)
	lines := strings.Split(output.String(), "\n")
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			clean = append(clean, line)
		}
	}
	return strings.TrimSpace(strings.Join(clean, "\n")), nil
}

func findMainContent(root *html.Node) *html.Node {
	var best *html.Node
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && (node.Data == "main" || node.Data == "article" || attributeValue(node, "role") == "main") {
			if best == nil || node.Data == "main" {
				best = node
			}
			if node.Data == "main" {
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	return best
}

func findBody(root *html.Node) *html.Node {
	if root.Type == html.ElementNode && root.Data == "body" {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if body := findBody(child); body != nil {
			return body
		}
	}
	return nil
}

func isRemovedElement(node *html.Node) bool {
	switch strings.ToLower(node.Data) {
	case "script", "style", "nav", "footer", "aside", "header", "form", "noscript", "svg", "iframe", "template", "button", "input", "select", "textarea":
		return true
	}
	for _, key := range []string{"class", "id"} {
		value := strings.ToLower(attributeValue(node, key))
		for _, marker := range []string{"cookie", "breadcrumb", "sidebar", "navigation", "social-share", "advert", "related-content", "subscribe", "paywall-overlay"} {
			if strings.Contains(value, marker) {
				return true
			}
		}
	}
	return false
}

func isTextBlock(tag string) bool {
	switch tag {
	case "article", "blockquote", "br", "dd", "div", "figcaption", "h1", "h2", "h3", "h4", "h5", "h6", "li", "main", "p", "pre", "section", "td", "th", "tr":
		return true
	default:
		return false
	}
}

func attributeValue(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}
