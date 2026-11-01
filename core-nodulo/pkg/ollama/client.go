package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client calls Ollama's HTTP API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// GenerateRequest is the request body accepted by /api/generate.
type GenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// GenerateResponse is one newline-delimited response from /api/generate.
type GenerateResponse struct {
	Model         string `json:"model,omitempty"`
	Response      string `json:"response,omitempty"`
	Done          bool   `json:"done,omitempty"`
	Error         string `json:"error,omitempty"`
	TotalDuration int64  `json:"total_duration,omitempty"`
}

// ChatMessage is one role-tagged message accepted by /api/chat.
type ChatMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Think    *bool         `json:"think,omitempty"`
}

type ChatResponse struct {
	Message ChatMessage `json:"message"`
	Done    bool        `json:"done"`
	Error   string      `json:"error,omitempty"`
}

// EmbeddingsRequest is the payload accepted by Ollama's /api/embeddings.
type EmbeddingsRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// EmbeddingsResponse contains the embedding generated for one prompt.
type EmbeddingsResponse struct {
	Embedding []float32 `json:"embedding"`
}

type ModelInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type PullProgress struct {
	Status    string `json:"status,omitempty"`
	Digest    string `json:"digest,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
	Error     string `json:"error,omitempty"`
}

type tagsResponse struct {
	Models []ModelInfo `json:"models"`
}

type pullRequest struct {
	Name   string `json:"name"`
	Stream bool   `json:"stream"`
}

type deleteModelRequest struct {
	Name string `json:"name"`
}

// NewClient creates a client that does not impose a timeout on long streams.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 0,
		},
	}
}

// CreateEmbedding generates one vector using Ollama's legacy embeddings API.
func (c *Client) CreateEmbedding(ctx context.Context, model, prompt string) ([]float32, error) {
	body, err := json.Marshal(EmbeddingsRequest{Model: model, Prompt: prompt})
	if err != nil {
		return nil, fmt.Errorf("encode Ollama embeddings request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Ollama embeddings request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request Ollama embeddings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("Ollama embeddings returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}

	var result EmbeddingsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Ollama embeddings response: %w", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("Ollama returned an empty embedding")
	}
	return result.Embedding, nil
}

// Chat performs one non-streaming completion through Ollama's /api/chat API.
func (c *Client) Chat(ctx context.Context, model string, messages []ChatMessage) (string, error) {
	body, err := json.Marshal(ChatRequest{Model: model, Messages: messages, Stream: false})
	if err != nil {
		return "", fmt.Errorf("encode Ollama chat request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create Ollama chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request Ollama chat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", fmt.Errorf("Ollama chat returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	var result ChatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result); err != nil {
		return "", fmt.Errorf("decode Ollama chat response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("Ollama chat error: %s", result.Error)
	}
	return result.Message.Content, nil
}

// ListLocalModels returns the model names and their on-disk sizes from Ollama.
func (c *Client) ListLocalModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("create Ollama tags request: %w", err)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request Ollama tags: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("Ollama tags returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}

	var result tagsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Ollama tags response: %w", err)
	}
	if result.Models == nil {
		result.Models = make([]ModelInfo, 0)
	}
	return result.Models, nil
}

// PullModelStream downloads a model and emits Ollama's newline-delimited
// progress events. Canceling ctx closes the HTTP stream and both channels.
func (c *Client) PullModelStream(ctx context.Context, modelName string) (<-chan PullProgress, <-chan error) {
	progress := make(chan PullProgress)
	errors := make(chan error, 1)

	go func() {
		defer close(progress)
		defer close(errors)

		body, err := json.Marshal(pullRequest{Name: modelName, Stream: true})
		if err != nil {
			sendError(ctx, errors, fmt.Errorf("encode Ollama pull request: %w", err))
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/pull", bytes.NewReader(body))
		if err != nil {
			sendError(ctx, errors, fmt.Errorf("create Ollama pull request: %w", err))
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			if ctx.Err() == nil {
				sendError(ctx, errors, fmt.Errorf("request Ollama model pull: %w", err))
			}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			message, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			sendError(ctx, errors, fmt.Errorf("Ollama pull returned %s: %s", resp.Status, strings.TrimSpace(string(message))))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			var item PullProgress
			if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
				sendError(ctx, errors, fmt.Errorf("decode Ollama pull progress: %w", err))
				return
			}
			if item.Error != "" {
				sendError(ctx, errors, fmt.Errorf("Ollama pull: %s", item.Error))
				return
			}
			select {
			case progress <- item:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			sendError(ctx, errors, fmt.Errorf("read Ollama pull progress: %w", err))
		}
	}()

	return progress, errors
}

// DeleteModel removes a locally installed Ollama model.
func (c *Client) DeleteModel(ctx context.Context, modelName string) error {
	body, err := json.Marshal(deleteModelRequest{Name: modelName})
	if err != nil {
		return fmt.Errorf("encode Ollama delete request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+"/api/delete", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Ollama delete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request Ollama model deletion: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("Ollama delete returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	return nil
}

// StreamGenerate streams response fragments from Ollama. The channels are
// closed when the request completes or its context is canceled.
func (c *Client) StreamGenerate(ctx context.Context, model, prompt string) (<-chan string, <-chan error) {
	tokens := make(chan string)
	errors := make(chan error, 1)

	go func() {
		defer close(tokens)
		defer close(errors)

		body, err := json.Marshal(GenerateRequest{Model: model, Prompt: prompt, Stream: true})
		if err != nil {
			sendError(ctx, errors, err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/generate", bytes.NewReader(body))
		if err != nil {
			sendError(ctx, errors, err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			if ctx.Err() == nil {
				sendError(ctx, errors, fmt.Errorf("request Ollama: %w", err))
			}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			message, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			sendError(ctx, errors, fmt.Errorf("Ollama returned %s: %s", resp.Status, strings.TrimSpace(string(message))))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		// Ollama normally emits short JSON lines; allow larger fragments as well.
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				return
			}
			var chunk GenerateResponse
			if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
				sendError(ctx, errors, fmt.Errorf("decode Ollama stream: %w", err))
				return
			}
			if chunk.Error != "" {
				sendError(ctx, errors, fmt.Errorf("Ollama: %s", chunk.Error))
				return
			}
			if chunk.Response != "" {
				select {
				case tokens <- chunk.Response:
				case <-ctx.Done():
					return
				}
			}
			if chunk.Done {
				return
			}
		}
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			sendError(ctx, errors, fmt.Errorf("read Ollama stream: %w", err))
		}
	}()

	return tokens, errors
}

// StreamChat sends role-separated messages to Ollama's /api/chat endpoint.
func (c *Client) StreamChat(ctx context.Context, model string, messages []ChatMessage) (<-chan string, <-chan error) {
	return c.StreamChatWithThinking(ctx, model, messages, false)
}

// StreamChatWithThinking asks compatible Ollama models to use their native
// reasoning mode. Models without that capability still receive the enhanced
// system instruction assembled by the RAG service.
func (c *Client) StreamChatWithThinking(ctx context.Context, model string, messages []ChatMessage, thinking bool) (<-chan string, <-chan error) {
	tokens := make(chan string)
	errors := make(chan error, 1)

	go func() {
		defer close(tokens)
		defer close(errors)

		request := ChatRequest{Model: model, Messages: messages, Stream: true}
		if thinking && supportsNativeThinking(model) {
			request.Think = &thinking
		}
		body, err := json.Marshal(request)
		if err != nil {
			sendError(ctx, errors, fmt.Errorf("encode Ollama chat request: %w", err))
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(body))
		if err != nil {
			sendError(ctx, errors, fmt.Errorf("create Ollama chat request: %w", err))
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			if ctx.Err() == nil {
				sendError(ctx, errors, fmt.Errorf("request Ollama chat: %w", err))
			}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			message, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			sendError(ctx, errors, fmt.Errorf("Ollama chat returned %s: %s", resp.Status, strings.TrimSpace(string(message))))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				return
			}
			var chunk ChatResponse
			if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
				sendError(ctx, errors, fmt.Errorf("decode Ollama chat stream: %w", err))
				return
			}
			if chunk.Error != "" {
				sendError(ctx, errors, fmt.Errorf("Ollama chat: %s", chunk.Error))
				return
			}
			if chunk.Message.Content != "" {
				select {
				case tokens <- chunk.Message.Content:
				case <-ctx.Done():
					return
				}
			}
			if chunk.Done {
				return
			}
		}
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			sendError(ctx, errors, fmt.Errorf("read Ollama chat stream: %w", err))
		}
	}()

	return tokens, errors
}

// supportsNativeThinking limits Ollama's `think` field to model families known
// to implement it. Other models still receive the RAG service's enhanced
// system instruction, without a parameter that can make their request fail.
func supportsNativeThinking(model string) bool {
	name := strings.ToLower(strings.TrimSpace(model))
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		name = name[slash+1:]
	}
	name, _, _ = strings.Cut(name, ":")
	for _, family := range []string{"deepseek-r1", "qwen3", "qwq", "phi4-reasoning"} {
		if name == family || strings.HasPrefix(name, family+"-") || strings.HasPrefix(name, family+".") {
			return true
		}
	}
	return false
}

func sendError(ctx context.Context, errs chan<- error, err error) {
	if err == nil || ctx.Err() != nil {
		return
	}
	errs <- err
}
