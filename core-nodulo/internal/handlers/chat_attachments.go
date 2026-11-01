package handlers

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"core-nodulo/internal/observability"
	"core-nodulo/internal/repository"
	"core-nodulo/internal/services"
	"core-nodulo/pkg/extractor"
)

const (
	maxChatPromptRunes     = 20_000
	chatMultipartMemory    = 8 << 20
	multipartOverheadBytes = 2 << 20
	defaultFileOnlyPrompt  = "Analise os arquivos anexados e apresente uma síntese dos pontos principais."
	defaultImageOnlyPrompt = "Analise a imagem anexada e descreva os elementos visuais relevantes com precisão didática."
)

var errChatUploadTooLarge = errors.New("O envio excede o limite de tamanho permitido.")

func parseChatInput(w http.ResponseWriter, r *http.Request, settings repository.ChatUploadSettings) (ChatPayload, []services.ChatAttachment, []repository.MessageAttachment, *multipart.Form, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return ChatPayload{}, nil, nil, nil, errors.New("Tipo de conteúdo inválido.")
	}
	if mediaType == "application/json" {
		var payload ChatPayload
		if err := decodeJSONBody(w, r, &payload, 1<<20); err != nil {
			return ChatPayload{}, nil, nil, nil, errors.New("Mensagem inválida ou maior que o limite permitido.")
		}
		payload.Prompt = strings.TrimSpace(payload.Prompt)
		if utf8.RuneCountInString(payload.Prompt) > maxChatPromptRunes {
			return ChatPayload{}, nil, nil, nil, errors.New("A mensagem excede o limite de 20 mil caracteres.")
		}
		if payload.Prompt == "" {
			return ChatPayload{}, nil, nil, nil, errors.New("Escreva uma mensagem antes de enviar.")
		}
		return payload, nil, nil, nil, nil
	}
	if mediaType != "multipart/form-data" {
		return ChatPayload{}, nil, nil, nil, errors.New("Envie uma mensagem JSON ou um formulário com arquivos anexados.")
	}

	maxRequestBytes := int64(0)
	if settings.MaxTotalSizeMiB > 0 {
		maxRequestBytes = settings.MaxTotalSizeMiB*(1<<20) + multipartOverheadBytes
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	}
	if err := r.ParseMultipartForm(chatMultipartMemory); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return ChatPayload{}, nil, nil, nil, fmt.Errorf("%w: o tamanho total ultrapassa o limite configurado de %d MiB.", errChatUploadTooLarge, settings.MaxTotalSizeMiB)
		}
		return ChatPayload{}, nil, nil, nil, errors.New("Não foi possível ler os arquivos enviados. Confira o formato do formulário.")
	}
	payload := ChatPayload{
		ConversationID: strings.TrimSpace(r.FormValue("conversation_id")),
		RetryMessageID: strings.TrimSpace(r.FormValue("retry_message_id")),
		Prompt:         strings.TrimSpace(r.FormValue("prompt")),
		Model:          strings.TrimSpace(r.FormValue("model")),
		Thinking:       strings.EqualFold(strings.TrimSpace(r.FormValue("thinking")), "true"),
	}
	if payload.Prompt == "" {
		payload.Prompt = strings.TrimSpace(r.FormValue("message"))
	}
	if utf8.RuneCountInString(payload.Prompt) > maxChatPromptRunes {
		r.MultipartForm.RemoveAll()
		return ChatPayload{}, nil, nil, nil, errors.New("A mensagem excede o limite de 20 mil caracteres.")
	}
	files := append(r.MultipartForm.File["files"], r.MultipartForm.File["file"]...)
	if settings.MaxFiles > 0 && len(files) > settings.MaxFiles {
		r.MultipartForm.RemoveAll()
		return ChatPayload{}, nil, nil, nil, fmt.Errorf("%w: esta instalação permite até %d arquivos por mensagem.", errChatUploadTooLarge, settings.MaxFiles)
	}
	if len(files) == 0 && payload.Prompt == "" {
		r.MultipartForm.RemoveAll()
		return ChatPayload{}, nil, nil, nil, errors.New("Escreva uma mensagem ou selecione pelo menos um arquivo.")
	}

	attachments := make([]services.ChatAttachment, 0, len(files))
	metadata := make([]repository.MessageAttachment, 0, len(files))
	totalBytes := int64(0)
	for _, header := range files {
		name := safeChatFilename(header.Filename)
		extension := strings.ToLower(filepath.Ext(name))
		if isSupportedChatImageExtension(extension) {
			if settings.MaxFileSizeMiB > 0 && header.Size > settings.MaxFileSizeMiB*(1<<20) {
				r.MultipartForm.RemoveAll()
				return ChatPayload{}, nil, nil, nil, fmt.Errorf("%w: %q tem %s; o limite por arquivo é %d MiB.", errChatUploadTooLarge, name, formatFileBytes(header.Size), settings.MaxFileSizeMiB)
			}
			totalBytes += header.Size
			if settings.MaxTotalSizeMiB > 0 && totalBytes > settings.MaxTotalSizeMiB*(1<<20) {
				r.MultipartForm.RemoveAll()
				return ChatPayload{}, nil, nil, nil, fmt.Errorf("%w: o tamanho combinado ultrapassa o limite de %d MiB.", errChatUploadTooLarge, settings.MaxTotalSizeMiB)
			}
			file, err := header.Open()
			if err != nil {
				r.MultipartForm.RemoveAll()
				return ChatPayload{}, nil, nil, nil, fmt.Errorf("Não foi possível abrir a imagem %q.", name)
			}
			encoded, imageErr := encodeChatImage(file, extension)
			closeErr := file.Close()
			if imageErr != nil || closeErr != nil {
				r.MultipartForm.RemoveAll()
				if imageErr != nil {
					return ChatPayload{}, nil, nil, nil, fmt.Errorf("A imagem %q é inválida. Use uma imagem PNG, JPEG ou WebP íntegra.", name)
				}
				return ChatPayload{}, nil, nil, nil, fmt.Errorf("Não foi possível concluir a leitura da imagem %q.", name)
			}
			attachments = append(attachments, services.ChatAttachment{Name: name, ImageBase64: encoded})
			metadata = append(metadata, repository.MessageAttachment{Name: name, Size: header.Size})
			continue
		}
		switch extension {
		case ".pdf", ".txt", ".md", ".docx", ".pptx":
		default:
			r.MultipartForm.RemoveAll()
			return ChatPayload{}, nil, nil, nil, fmt.Errorf("O arquivo %q tem formato não suportado. Use PDF, DOCX, PPTX, TXT, MD, PNG, JPEG ou WebP.", name)
		}
		if settings.MaxFileSizeMiB > 0 && header.Size > settings.MaxFileSizeMiB*(1<<20) {
			r.MultipartForm.RemoveAll()
			return ChatPayload{}, nil, nil, nil, fmt.Errorf("%w: %q tem %s; o limite por arquivo é %d MiB.", errChatUploadTooLarge, name, formatFileBytes(header.Size), settings.MaxFileSizeMiB)
		}
		totalBytes += header.Size
		if settings.MaxTotalSizeMiB > 0 && totalBytes > settings.MaxTotalSizeMiB*(1<<20) {
			r.MultipartForm.RemoveAll()
			return ChatPayload{}, nil, nil, nil, fmt.Errorf("%w: o tamanho combinado ultrapassa o limite de %d MiB.", errChatUploadTooLarge, settings.MaxTotalSizeMiB)
		}
		file, err := header.Open()
		if err != nil {
			r.MultipartForm.RemoveAll()
			return ChatPayload{}, nil, nil, nil, fmt.Errorf("Não foi possível abrir o arquivo %q.", name)
		}
		documents, extractErr := extractor.ExtractFile(name, file, header.Size)
		closeErr := file.Close()
		if extractErr != nil || closeErr != nil {
			r.MultipartForm.RemoveAll()
			if extractErr != nil {
				slog.WarnContext(r.Context(), "chat attachment extraction failed",
					"request_id", observability.RequestID(r.Context()),
					"file_extension", extension,
					"error", observability.SafeError(extractErr),
				)
				return ChatPayload{}, nil, nil, nil, fmt.Errorf("Não foi possível ler um arquivo %s. Confirme que está íntegro e legível; PDFs digitalizados precisam de OCR. Nenhum arquivo foi enviado à IA.", extension)
			}
			return ChatPayload{}, nil, nil, nil, fmt.Errorf("Não foi possível concluir a leitura de %q.", name)
		}
		var extracted strings.Builder
		for _, document := range documents {
			if extracted.Len() > 0 {
				extracted.WriteString("\n\n")
			}
			extracted.WriteString(document.Content)
		}
		content := strings.TrimSpace(extracted.String())
		if content == "" {
			r.MultipartForm.RemoveAll()
			return ChatPayload{}, nil, nil, nil, fmt.Errorf("O arquivo %q não contém texto que possa ser analisado.", name)
		}
		attachments = append(attachments, services.ChatAttachment{Name: name, Content: content})
		metadata = append(metadata, repository.MessageAttachment{Name: name, Size: header.Size})
	}
	return payload, attachments, metadata, r.MultipartForm, nil
}

func formatFileBytes(size int64) string {
	if size < 1<<20 {
		return fmt.Sprintf("%.1f KiB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))
}

func safeChatFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(filepath.Clean(name))
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name))
	if name == "" || name == "." {
		return "arquivo"
	}
	return name
}

func isSupportedChatImageExtension(extension string) bool {
	switch extension {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	default:
		return false
	}
}

func encodeChatImage(file multipart.File, extension string) (string, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", errors.New("imagem vazia")
	}

	actualType := ""
	switch {
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}):
		actualType = "image/png"
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		actualType = "image/jpeg"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		actualType = "image/webp"
	}
	expectedType := ""
	switch extension {
	case ".png":
		expectedType = "image/png"
	case ".jpg", ".jpeg":
		expectedType = "image/jpeg"
	case ".webp":
		expectedType = "image/webp"
	}
	if actualType == "" || actualType != expectedType {
		return "", errors.New("extensão e conteúdo da imagem não correspondem")
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
