package extractor

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	pdf "github.com/ledongthuc/pdf"
	"golang.org/x/net/html"
)

const (
	maxArchiveEntries      = 500
	maxArchiveEntryBytes   = 200 << 20
	maxArchiveExpandedSize = 512 << 20
	maxWebResponseBytes    = 32 << 20
)

// TextExtractor converts a random-access document into plain text.
type TextExtractor interface {
	Extract(r io.ReaderAt, size int64) (string, error)
}

type PDFExtractor struct{}

func (PDFExtractor) Extract(r io.ReaderAt, size int64) (text string, err error) {
	// The PDF parser has malformed-file paths that panic instead of returning
	// an error. Keep that failure local to this document so a bad PDF cannot
	// terminate the HTTP request or an ingestion worker.
	defer func() {
		if recovered := recover(); recovered != nil {
			text = ""
			err = errors.New("estrutura PDF inválida ou não suportada pelo extrator")
		}
	}()
	if r == nil || size <= 0 {
		return "", errors.New("PDF vazio ou inválido")
	}
	document, err := pdf.NewReader(r, size)
	if err != nil {
		return "", fmt.Errorf("abrir PDF: %w", err)
	}
	var output strings.Builder
	for pageNumber := 1; pageNumber <= document.NumPage(); pageNumber++ {
		page := document.Page(pageNumber)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("extrair página %d do PDF: %w", pageNumber, err)
		}
		text = cleanText(text)
		if text != "" {
			if output.Len() > 0 {
				output.WriteString("\n\n")
			}
			output.WriteString(text)
		}
	}
	if output.Len() == 0 {
		return "", errors.New("PDF não contém texto extraível; documentos digitalizados precisam de OCR")
	}
	return output.String(), nil
}

// ExtractFile selects the parser from the filename and preserves that name in
// the returned document metadata.
func ExtractFile(filename string, r io.ReaderAt, size int64) ([]ExtractedDocument, error) {
	if r == nil || size <= 0 {
		return nil, errors.New("arquivo vazio ou inválido")
	}
	name := filepath.Base(filepath.Clean(filename))
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip":
		if size > maxArchiveEntryBytes {
			return nil, fmt.Errorf("arquivo ZIP excede o limite de leitura interna de %d MiB", maxArchiveEntryBytes>>20)
		}
		return (ZipExtractor{}).ExtractDocuments(r, size)
	case ".pdf":
		text, err := (PDFExtractor{}).Extract(r, size)
		if err != nil {
			return nil, err
		}
		return []ExtractedDocument{{Name: name, Content: text}}, nil
	case ".txt", ".md":
		data, err := io.ReadAll(io.NewSectionReader(r, 0, size))
		if err != nil {
			return nil, fmt.Errorf("ler arquivo de texto: %w", err)
		}
		if !utf8.Valid(data) {
			return nil, errors.New("arquivo de texto precisa estar codificado em UTF-8")
		}
		return []ExtractedDocument{{Name: name, Content: cleanText(string(data))}}, nil
	case ".docx", ".pptx":
		data, err := io.ReadAll(io.NewSectionReader(r, 0, size))
		if err != nil {
			return nil, fmt.Errorf("ler arquivo Office: %w", err)
		}
		var text string
		if strings.EqualFold(filepath.Ext(name), ".docx") {
			text, err = extractOfficeXML(data, "word/document.xml", "w:t", "w:p")
		} else {
			text, err = extractPresentationXML(data)
		}
		if err != nil {
			return nil, err
		}
		return []ExtractedDocument{{Name: name, Content: text}}, nil
	default:
		return nil, fmt.Errorf("tipo de arquivo não suportado: %s", filepath.Ext(name))
	}
}

type ExtractedDocument struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type ZipExtractor struct{}

// Extract combines readable archive entries and keeps each original filename
// as a visible heading. Call ExtractDocuments when individual entry metadata
// is needed by an ingestion job.
func (z ZipExtractor) Extract(r io.ReaderAt, size int64) (string, error) {
	documents, err := z.ExtractDocuments(r, size)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	for _, document := range documents {
		if output.Len() > 0 {
			output.WriteString("\n\n")
		}
		output.WriteString("Documento: ")
		output.WriteString(document.Name)
		output.WriteString("\n\n")
		output.WriteString(document.Content)
	}
	return output.String(), nil
}

func (ZipExtractor) ExtractDocuments(r io.ReaderAt, size int64) ([]ExtractedDocument, error) {
	if r == nil || size <= 0 {
		return nil, errors.New("arquivo ZIP vazio ou inválido")
	}
	archive, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("abrir arquivo ZIP: %w", err)
	}
	if len(archive.File) > maxArchiveEntries {
		return nil, fmt.Errorf("arquivo ZIP excede o limite de %d entradas", maxArchiveEntries)
	}
	var declaredExpandedTotal uint64
	var actualExpandedTotal uint64
	documents := make([]ExtractedDocument, 0)
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(filepath.Clean(entry.Name))
		extension := strings.ToLower(filepath.Ext(name))
		if !supportedArchiveExtension(extension) {
			continue
		}
		if entry.UncompressedSize64 > maxArchiveEntryBytes {
			return nil, fmt.Errorf("arquivo %q excede o limite descompactado de %d MiB", name, maxArchiveEntryBytes>>20)
		}
		declaredExpandedTotal += entry.UncompressedSize64
		if declaredExpandedTotal > maxArchiveExpandedSize {
			return nil, errors.New("conteúdo descompactado do ZIP excede o limite de 512 MiB")
		}
		content, err := readZipEntry(entry, maxArchiveEntryBytes)
		if err != nil {
			return nil, fmt.Errorf("ler arquivo %q do ZIP: %w", name, err)
		}
		actualExpandedTotal += uint64(len(content))
		if actualExpandedTotal > maxArchiveExpandedSize {
			return nil, errors.New("conteúdo descompactado do ZIP excede o limite de 512 MiB")
		}
		text, err := extractEntry(extension, content)
		if err != nil {
			return nil, fmt.Errorf("extrair arquivo %q do ZIP: %w", name, err)
		}
		text = cleanText(text)
		if text != "" {
			documents = append(documents, ExtractedDocument{Name: name, Content: text})
		}
	}
	if len(documents) == 0 {
		return nil, errors.New("o ZIP não contém arquivos suportados com texto extraível")
	}
	return documents, nil
}

func supportedArchiveExtension(extension string) bool {
	switch extension {
	case ".pdf", ".txt", ".md", ".docx", ".pptx":
		return true
	default:
		return false
	}
}

func extractEntry(extension string, content []byte) (string, error) {
	switch extension {
	case ".pdf":
		return (PDFExtractor{}).Extract(bytes.NewReader(content), int64(len(content)))
	case ".txt", ".md":
		if !utf8.Valid(content) {
			return "", errors.New("arquivo de texto precisa estar codificado em UTF-8")
		}
		return string(content), nil
	case ".docx":
		return extractOfficeXML(content, "word/document.xml", "w:t", "w:p")
	case ".pptx":
		return extractPresentationXML(content)
	default:
		return "", fmt.Errorf("extensão não suportada: %s", extension)
	}
}

func readZipEntry(entry *zip.File, maximum int64) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("entrada excede limite de %d bytes", maximum)
	}
	return data, nil
}

func extractOfficeXML(archiveBytes []byte, target, textTag, paragraphTag string) (string, error) {
	archive, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		return "", fmt.Errorf("abrir contêiner Office: %w", err)
	}
	for _, entry := range archive.File {
		if filepath.ToSlash(entry.Name) != target {
			continue
		}
		data, err := readZipEntry(entry, maxArchiveEntryBytes)
		if err != nil {
			return "", err
		}
		return extractXMLText(data, textTag, paragraphTag)
	}
	return "", fmt.Errorf("arquivo XML %s não encontrado", target)
}

func extractPresentationXML(archiveBytes []byte) (string, error) {
	archive, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		return "", fmt.Errorf("abrir apresentação PPTX: %w", err)
	}
	type slideFile struct {
		name  string
		index int
		file  *zip.File
	}
	slides := make([]slideFile, 0)
	for _, entry := range archive.File {
		name := filepath.ToSlash(entry.Name)
		if !strings.HasPrefix(name, "ppt/slides/slide") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		stem := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(name), "slide"), ".xml")
		index := 0
		if _, err := fmt.Sscanf(stem, "%d", &index); err != nil {
			continue
		}
		slides = append(slides, slideFile{name: name, index: index, file: entry})
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].index < slides[j].index })
	var output strings.Builder
	for _, slide := range slides {
		data, err := readZipEntry(slide.file, maxArchiveEntryBytes)
		if err != nil {
			return "", fmt.Errorf("ler slide %s: %w", slide.name, err)
		}
		text, err := extractXMLText(data, "a:t", "a:p")
		if err != nil {
			return "", fmt.Errorf("ler texto do slide %s: %w", slide.name, err)
		}
		if text != "" {
			if output.Len() > 0 {
				output.WriteString("\n\n")
			}
			output.WriteString("Slide ")
			output.WriteString(fmt.Sprint(slide.index))
			output.WriteString("\n")
			output.WriteString(text)
		}
	}
	if output.Len() == 0 {
		return "", errors.New("a apresentação não contém texto extraível")
	}
	return output.String(), nil
}

func extractXMLText(data []byte, textTag, paragraphTag string) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var output strings.Builder
	inText := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("decodificar XML: %w", err)
		}
		switch item := token.(type) {
		case xml.StartElement:
			if item.Name.Local == tagLocalName(paragraphTag) && output.Len() > 0 {
				output.WriteByte('\n')
			}
			if item.Name.Local == tagLocalName(textTag) {
				inText = true
			}
		case xml.EndElement:
			if item.Name.Local == tagLocalName(textTag) {
				inText = false
			}
		case xml.CharData:
			if inText {
				output.Write([]byte(item))
			}
		}
	}
	return cleanText(output.String()), nil
}

func tagLocalName(tag string) string {
	if index := strings.IndexByte(tag, ':'); index >= 0 {
		return tag[index+1:]
	}
	return tag
}

type WebExtractor struct {
	UserAgent string
}

func (w WebExtractor) Extract(r io.ReaderAt, size int64) (string, error) {
	if r == nil || size <= 0 || size > 4096 {
		return "", errors.New("URL vazia")
	}
	urlBytes := make([]byte, size)
	if _, err := r.ReadAt(urlBytes, 0); err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("ler URL: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return w.ExtractURL(ctx, string(urlBytes))
}

func (w WebExtractor) ExtractURL(ctx context.Context, rawURL string) (string, error) {
	parsed, err := validateWebURL(ctx, rawURL)
	if err != nil {
		return "", err
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           safePublicDialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("página excedeu o limite de redirecionamentos")
			}
			_, err := validateWebURL(request.Context(), request.URL.String())
			return err
		},
	}
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", fmt.Errorf("criar requisição web: %w", err)
	}
	userAgent := strings.TrimSpace(w.UserAgent)
	if userAgent == "" {
		userAgent = "NODULO-RAG/1.0"
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("buscar página web: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("página web respondeu %s", response.Status)
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "" && mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		return "", fmt.Errorf("tipo de conteúdo web não suportado: %s", mediaType)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxWebResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("ler página web: %w", err)
	}
	if len(body) > maxWebResponseBytes {
		return "", fmt.Errorf("página web excede o limite de %d MiB", maxWebResponseBytes>>20)
	}
	text, err := extractHTMLText(bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", errors.New("página web não contém texto extraível")
	}
	return text, nil
}

func ValidatePublicURL(ctx context.Context, rawURL string) error {
	_, err := validateWebURL(ctx, rawURL)
	return err
}

// NewSafePublicHTTPClient creates an HTTP client that only connects to public
// addresses and revalidates every redirect. It is intended for server-side
// retrieval of user/model-provided URLs.
func NewSafePublicHTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           safePublicDialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("página excedeu o limite de redirecionamentos")
			}
			return ValidatePublicURL(request.Context(), request.URL.String())
		},
	}
}

func validateWebURL(ctx context.Context, rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return nil, errors.New("URL deve ser HTTP(S) pública e não pode conter credenciais")
	}
	if err := rejectNonPublicHost(ctx, parsed.Hostname()); err != nil {
		return nil, err
	}
	return parsed, nil
}

func safePublicDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("endereço web inválido: %w", err)
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolver host web: %w", err)
	}
	if len(addresses) == 0 {
		return nil, errors.New("host web não possui endereços IP")
	}
	for _, address := range addresses {
		if !isPublicIP(address.IP) {
			return nil, errors.New("URLs para redes privadas ou locais não são permitidas")
		}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, address := range addresses {
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("conectar ao host público: %w", lastErr)
}

func rejectNonPublicHost(ctx context.Context, host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return errors.New("URLs para redes privadas ou locais não são permitidas")
		}
		return nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolver URL: %w", err)
	}
	if len(addresses) == 0 {
		return errors.New("host da URL não resolveu para um endereço IP")
	}
	for _, address := range addresses {
		if !isPublicIP(address.IP) {
			return errors.New("URLs para redes privadas ou locais não são permitidas")
		}
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}

func extractHTMLText(reader io.Reader) (string, error) {
	document, err := html.Parse(reader)
	if err != nil {
		return "", fmt.Errorf("analisar HTML: %w", err)
	}
	var output strings.Builder
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch strings.ToLower(node.Data) {
			case "script", "style", "noscript", "svg", "template", "head":
				return
			}
			if isBlockElement(strings.ToLower(node.Data)) && output.Len() > 0 {
				output.WriteByte('\n')
			}
		}
		if node.Type == html.TextNode {
			output.WriteString(node.Data)
			output.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
		if node.Type == html.ElementNode && isBlockElement(strings.ToLower(node.Data)) {
			output.WriteByte('\n')
		}
	}
	visit(document)
	return cleanText(output.String()), nil
}

func isBlockElement(tag string) bool {
	switch tag {
	case "address", "article", "aside", "blockquote", "br", "dd", "div", "dl", "dt", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "li", "main", "nav", "ol", "p", "pre", "section", "table", "td", "th", "tr", "ul":
		return true
	default:
		return false
	}
}

func cleanText(value string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return ' '
			}
			return r
		}, line)
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}
