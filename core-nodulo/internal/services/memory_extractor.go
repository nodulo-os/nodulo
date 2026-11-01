package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"core-nodulo/internal/repository"
	"core-nodulo/pkg/ollama"
)

const (
	memoryExtractionTimeout = 90 * time.Second
	memoryFactMaxRunes      = 500
	memoryPromptMaxRunes    = 12_000
)

const memoryExtractionSystemPrompt = `Você é o Extrator de Perfil do NÓDULO. Analise a mensagem do usuário. Se ele revelar informações duradouras sobre si mesmo (ex: "estou no 1º ano", "tenho dificuldade com ponteiros", "uso Linux", "prefiro explicações curtas"), extraia o fato de forma concisa na terceira pessoa (ex: "O usuário tem dificuldade com ponteiros em C"). Se não houver informação pessoal ou duradoura, retorne EXATAMENTE a string "VAZIO".`

// MemoryService extracts durable user-provided profile facts and retrieves
// semantically related facts without putting user work on the chat request path.
type MemoryService struct {
	client      *ollama.Client
	repository  *repository.UserMemoryRepository
	generation  string
	embedding   string
	concurrency chan struct{}
}

func NewMemoryService(client *ollama.Client, repo *repository.UserMemoryRepository, generationModel, embeddingModel string) *MemoryService {
	return &MemoryService{
		client: client, repository: repo,
		generation: strings.TrimSpace(generationModel), embedding: strings.TrimSpace(embeddingModel),
		concurrency: make(chan struct{}, 4),
	}
}

// ExtractAndSaveFact is intended to run in a goroutine after the SSE response
// has been flushed. It uses an independent bounded context, so client disconnects
// do not cancel a save already scheduled for the user's profile.
func (s *MemoryService) ExtractAndSaveFact(userID string, userMessage string, aiResponse string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// Avoid writing model or user content that might be present in a panic value.
			slog.Error("semantic memory extraction panicked; request server remains healthy")
		}
	}()
	if s == nil || s.client == nil || s.repository == nil || strings.TrimSpace(userID) == "" || s.generation == "" || s.embedding == "" {
		return
	}
	select {
	case s.concurrency <- struct{}{}:
		defer func() { <-s.concurrency }()
	default:
		slog.Warn("semantic memory extraction skipped because the background limit is full")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), memoryExtractionTimeout)
	defer cancel()
	enabled, err := s.repository.MemoryEnabled(ctx, userID)
	if err != nil {
		slog.Warn("read semantic memory preference failed")
		return
	}
	if !enabled {
		return
	}
	userMessage = truncateMemoryText(strings.TrimSpace(userMessage), memoryPromptMaxRunes)
	aiResponse = truncateMemoryText(strings.TrimSpace(aiResponse), memoryPromptMaxRunes)
	if userMessage == "" {
		return
	}
	input := fmt.Sprintf("Mensagem do usuário (única fonte autorizada para fatos pessoais):\n%s\n\nResposta da IA (apenas contexto; não extraia fatos dela):\n%s", userMessage, aiResponse)
	messages := []ollama.ChatMessage{
		{Role: "system", Content: memoryExtractionSystemPrompt},
		{Role: "user", Content: input},
	}
	tokens, errs := s.client.StreamChat(ctx, s.generation, messages)
	var result strings.Builder
	for tokens != nil || errs != nil {
		select {
		case <-ctx.Done():
			slog.Warn("semantic memory extraction timed out")
			return
		case token, ok := <-tokens:
			if !ok {
				tokens = nil
				continue
			}
			if result.Len() < 4_000 {
				result.WriteString(token)
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				slog.Warn("semantic memory extraction failed")
				return
			}
		}
	}

	fact := strings.TrimSpace(result.String())
	fact = strings.Trim(fact, "`\"' \r\n\t")
	if fact == "" || strings.EqualFold(fact, "VAZIO") || strings.ContainsAny(fact, "\r\n") {
		return
	}
	fact = truncateMemoryText(fact, memoryFactMaxRunes)
	embedding, err := s.client.CreateEmbedding(ctx, s.embedding, fact)
	if err != nil {
		slog.Warn("create semantic memory embedding failed")
		return
	}
	if len(embedding) != repository.EmbeddingDimensions {
		slog.Warn("semantic memory embedding has unexpected dimensions", "dimensions", len(embedding), "expected", repository.EmbeddingDimensions)
		return
	}
	if err := s.repository.InsertFact(ctx, userID, fact, embedding); err != nil {
		if errors.Is(err, repository.ErrMemoryDisabled) {
			return
		}
		slog.Warn("persist semantic memory fact failed")
		return
	}
	slog.Info("semantic memory fact stored", "user_id", userID, "fact_characters", utf8.RuneCountInString(fact))
}

// RelevantFacts returns up to three profile facts related to the current
// question. Failures are returned to the caller, which can continue without memory.
func (s *MemoryService) RelevantFacts(ctx context.Context, userID, question string) ([]string, error) {
	if s == nil || s.client == nil || s.repository == nil || s.embedding == "" {
		return nil, nil
	}
	enabled, err := s.repository.MemoryEnabled(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read semantic memory preference: %w", err)
	}
	if !enabled {
		return nil, nil
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	embedding, err := s.client.CreateEmbedding(lookupCtx, s.embedding, truncateMemoryText(question, memoryPromptMaxRunes))
	if err != nil {
		return nil, fmt.Errorf("embed question for semantic memory: %w", err)
	}
	if len(embedding) != repository.EmbeddingDimensions {
		return nil, fmt.Errorf("semantic memory query embedding has %d dimensions; expected %d", len(embedding), repository.EmbeddingDimensions)
	}
	facts, err := s.repository.SearchRelevantFacts(lookupCtx, userID, embedding, 3)
	if err != nil {
		return nil, fmt.Errorf("retrieve semantic memory facts: %w", err)
	}
	return facts, nil
}

func truncateMemoryText(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}
