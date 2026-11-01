package services

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"core-nodulo/internal/repository"
	"core-nodulo/pkg/ollama"
)

const conversationTitleSystemPrompt = `Você é um assistente de estruturação. Leia a mensagem do usuário e gere um título descritivo e direto para este chat de no MÁXIMO 4 palavras.
Regras INEGOCIÁVEIS: Retorne APENAS o título. NÃO use aspas, NÃO use pontos finais, NÃO use Markdown. Exemplo: "Comparação REST e gRPC"`

const titleGenerationTimeout = 8 * time.Second

type TitleGenerator struct {
	client     *ollama.Client
	repository *repository.ChatRepository
	model      string
}

func NewTitleGenerator(client *ollama.Client, repo *repository.ChatRepository, model string) *TitleGenerator {
	return &TitleGenerator{client: client, repository: repo, model: strings.TrimSpace(model)}
}

// GenerateAndUpdateTitle creates a short title without blocking the chat stream.
// Call it in a goroutine after the first assistant response is flushed to the user.
func (g *TitleGenerator) GenerateAndUpdateTitle(conversationID string, userMessage string) {
	defer func() {
		if recover() != nil {
			slog.Error("automatic conversation title generation panicked")
		}
	}()
	if g == nil || g.client == nil || g.repository == nil || g.model == "" || strings.TrimSpace(conversationID) == "" {
		return
	}
	userMessage = strings.TrimSpace(userMessage)
	if userMessage == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), titleGenerationTimeout)
	defer cancel()
	title, err := g.client.Chat(ctx, g.model, []ollama.ChatMessage{
		{Role: "system", Content: conversationTitleSystemPrompt},
		{Role: "user", Content: userMessage},
	})
	if err != nil {
		slog.WarnContext(ctx, "automatic conversation title generation failed")
		return
	}
	title = normalizeConversationTitle(title)
	if title == "" {
		return
	}
	if err := g.repository.UpdateConversationTitle(ctx, conversationID, title); err != nil {
		slog.WarnContext(ctx, "automatic conversation title persistence failed")
		return
	}
	slog.InfoContext(ctx, "conversation title generated", "conversation_id", conversationID)
}

func normalizeConversationTitle(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.Trim(value, "\"'`*_# ")
	value = strings.TrimRight(value, ".!?;:, ")
	words := strings.Fields(value)
	if len(words) > 4 {
		words = words[:4]
	}
	if len(words) == 0 {
		return ""
	}
	return strings.Join(words, " ")
}
