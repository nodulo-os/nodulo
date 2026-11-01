package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound      = errors.New("registro não encontrado")
	ErrConflict      = errors.New("registro já existe")
	ErrInvalidFolder = errors.New("pasta inválida")
)

const EmbeddingDimensions = 768

type ChatRepository struct {
	pool *pgxpool.Pool
}

type Conversation struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	FolderID   *string    `json:"folder_id"`
	Title      string     `json:"title"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type ConversationFolder struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID             string              `json:"id"`
	ConversationID string              `json:"conversation_id"`
	Role           string              `json:"role"`
	Content        string              `json:"content"`
	Attachments    []MessageAttachment `json:"attachments,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
}

type MessageAttachment struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	DataURL   string `json:"data_url,omitempty"`
	MimeType  string `json:"mime_type,omitempty"`
	Generated bool   `json:"generated,omitempty"`
}

type DocumentChunk struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	SourceReference string  `json:"source_reference"`
	Content         string  `json:"content"`
	Score           float64 `json:"score"`
}

type DocumentChunkInput struct {
	Title           string
	SourceReference string
	Content         string
	Embedding       []float32
}

func NewChatRepository(pool *pgxpool.Pool) *ChatRepository {
	return &ChatRepository{pool: pool}
}

func (r *ChatRepository) InsertDocumentChunk(ctx context.Context, title, sourceRef, content string, embedding []float32) error {
	vector, err := formatEmbedding(embedding)
	if err != nil {
		return err
	}
	const query = `
		INSERT INTO document_chunks
			(id, title, source_reference, content, embedding, metadata, created_at)
		VALUES
			(uuid_generate_v4(), $1, $2, $3, $4::vector, '{}'::jsonb, NOW())`
	if _, err := r.pool.Exec(ctx, query, title, sourceRef, content, vector); err != nil {
		return fmt.Errorf("insert document chunk: %w", err)
	}
	return nil
}

func (r *ChatRepository) InsertDocumentChunks(ctx context.Context, chunks []DocumentChunkInput) error {
	if len(chunks) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin document chunk batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	const query = `
		INSERT INTO document_chunks
			(id, title, source_reference, content, embedding, metadata, created_at)
		VALUES
			(uuid_generate_v4(), $1, $2, $3, $4::vector, '{}'::jsonb, NOW())`
	for index, chunk := range chunks {
		vector, err := formatEmbedding(chunk.Embedding)
		if err != nil {
			return fmt.Errorf("format embedding for batch item %d: %w", index+1, err)
		}
		if _, err := tx.Exec(ctx, query, chunk.Title, chunk.SourceReference, chunk.Content, vector); err != nil {
			return fmt.Errorf("insert document chunk batch item %d: %w", index+1, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit document chunk batch: %w", err)
	}
	return nil
}

// ValidateDocumentChunkPrefix confirms that the already-counted prefix of a
// retried file is present in the index with the same title, source and content.
func (r *ChatRepository) ValidateDocumentChunkPrefix(ctx context.Context, sourceRef string, titles, contents []string) error {
	existing, err := r.ExistingDocumentChunks(ctx, sourceRef, titles, contents)
	if err != nil {
		return err
	}
	for index, found := range existing {
		if !found {
			return fmt.Errorf("persisted chunk %d does not match the uploaded file", index+1)
		}
	}
	return nil
}

func (r *ChatRepository) ExistingDocumentChunks(ctx context.Context, sourceRef string, titles, contents []string) ([]bool, error) {
	if len(titles) != len(contents) {
		return nil, fmt.Errorf("chunk title/content counts do not match")
	}
	if len(titles) == 0 {
		return []bool{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM document_chunks AS stored
			WHERE stored.source_reference = $1
			  AND stored.title = expected.title
			  AND stored.content = expected.content
		)
		FROM unnest($2::text[], $3::text[]) WITH ORDINALITY
			AS expected(title, content, chunk_order)
		ORDER BY expected.chunk_order`, sourceRef, titles, contents)
	if err != nil {
		return nil, fmt.Errorf("check existing document chunks: %w", err)
	}
	defer rows.Close()
	existing := make([]bool, 0, len(titles))
	for rows.Next() {
		var found bool
		if err := rows.Scan(&found); err != nil {
			return nil, fmt.Errorf("scan existing document chunk: %w", err)
		}
		existing = append(existing, found)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing document chunks: %w", err)
	}
	if len(existing) != len(titles) {
		return nil, fmt.Errorf("existing chunk lookup returned %d rows; expected %d", len(existing), len(titles))
	}
	return existing, nil
}

func (r *ChatRepository) SearchSimilarChunks(ctx context.Context, queryEmbedding []float32, limit int) ([]DocumentChunk, error) {
	vector, err := formatEmbedding(queryEmbedding)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		return []DocumentChunk{}, nil
	}
	const query = `
		SELECT id::text, title, source_reference, content,
			1 - (embedding <=> $1::vector) AS score
		FROM document_chunks
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $2`
	rows, err := r.pool.Query(ctx, query, vector, limit)
	if err != nil {
		return nil, fmt.Errorf("search similar document chunks: %w", err)
	}
	defer rows.Close()

	chunks := make([]DocumentChunk, 0, limit)
	for rows.Next() {
		var chunk DocumentChunk
		if err := rows.Scan(&chunk.ID, &chunk.Title, &chunk.SourceReference, &chunk.Content, &chunk.Score); err != nil {
			return nil, fmt.Errorf("scan similar document chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate similar document chunks: %w", err)
	}
	return chunks, nil
}

func formatEmbedding(embedding []float32) (string, error) {
	if len(embedding) != EmbeddingDimensions {
		return "", fmt.Errorf("embedding must have %d dimensions, got %d", EmbeddingDimensions, len(embedding))
	}
	var builder strings.Builder
	builder.Grow(len(embedding) * 12)
	builder.WriteByte('[')
	for index, value := range embedding {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return "", fmt.Errorf("embedding contains a non-finite value at index %d", index)
		}
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.Write(strconv.AppendFloat(nil, float64(value), 'f', -1, 32))
	}
	builder.WriteByte(']')
	return builder.String(), nil
}

func (r *ChatRepository) CreateConversation(ctx context.Context, userID, title string) (string, error) {
	const query = `
		INSERT INTO conversations (id, user_id, title, created_at, updated_at)
		VALUES (uuid_generate_v4(), $1, $2, NOW(), NOW())
		RETURNING id::text`
	var id string
	if err := r.pool.QueryRow(ctx, query, userID, title).Scan(&id); err != nil {
		return "", fmt.Errorf("create conversation: %w", err)
	}
	return id, nil
}

func (r *ChatRepository) ListConversations(ctx context.Context, userID string) ([]Conversation, error) {
	const query = `
		SELECT id::text, user_id::text, folder_id::text, title, archived_at, created_at, updated_at
		FROM conversations
		WHERE user_id = $1 AND archived_at IS NULL
		ORDER BY updated_at DESC, id DESC`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	conversations := make([]Conversation, 0)
	for rows.Next() {
		var conversation Conversation
		if err := rows.Scan(&conversation.ID, &conversation.UserID, &conversation.FolderID, &conversation.Title, &conversation.ArchivedAt, &conversation.CreatedAt, &conversation.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		conversations = append(conversations, conversation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversations: %w", err)
	}
	return conversations, nil
}

func (r *ChatRepository) GetMessages(ctx context.Context, conversationID string) ([]Message, error) {
	const query = `
		SELECT id::text, conversation_id::text, role, content, attachments, created_at
		FROM messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC, id ASC`
	return r.getMessages(ctx, query, conversationID)
}

func (r *ChatRepository) GetMessagesByConversationID(ctx context.Context, conversationID string) ([]Message, error) {
	return r.GetMessages(ctx, conversationID)
}

// GetMessagesByConversationIDForUser limits message reads to the requested owner.
func (r *ChatRepository) GetMessagesByConversationIDForUser(ctx context.Context, conversationID, userID string) ([]Message, error) {
	const query = `
		SELECT m.id::text, m.conversation_id::text, m.role, m.content, m.attachments, m.created_at
		FROM messages AS m
		JOIN conversations AS c ON c.id = m.conversation_id
		WHERE m.conversation_id = $1 AND c.user_id = $2
		ORDER BY m.created_at ASC, m.id ASC`
	return r.getMessages(ctx, query, conversationID, userID)
}

// GetRecentMessagesForContext returns at most the latest ten messages in
// chronological order, restricted to the conversation owner. When beforeID is
// set (message retry/edit), only earlier messages are returned.
func (r *ChatRepository) GetRecentMessagesForContext(ctx context.Context, conversationID, userID, beforeID string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 10 {
		limit = 10
	}
	const query = `
		SELECT id, conversation_id, role, content, created_at
		FROM (
			SELECT m.id::text AS id, m.conversation_id::text AS conversation_id,
				m.role, m.content, m.created_at, m.id AS sort_id
			FROM messages AS m
			JOIN conversations AS c ON c.id = m.conversation_id
			WHERE m.conversation_id = $1 AND c.user_id = $2
				AND ($4 = '' OR (m.created_at, m.id) < (
					SELECT target.created_at, target.id
					FROM messages AS target
					WHERE target.id = NULLIF($4, '')::uuid
						AND target.conversation_id = $1 AND target.role = 'user'
				))
			ORDER BY m.created_at DESC, m.id DESC
			LIMIT $3
		) AS recent
		ORDER BY created_at ASC, sort_id ASC`
	rows, err := r.pool.Query(ctx, query, conversationID, userID, limit, beforeID)
	if err != nil {
		return nil, fmt.Errorf("get recent conversation context: %w", err)
	}
	defer rows.Close()
	messages := make([]Message, 0, limit)
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.ConversationID, &message.Role, &message.Content, &message.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan recent conversation context: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent conversation context: %w", err)
	}
	return messages, nil
}

func (r *ChatRepository) getMessages(ctx context.Context, query string, args ...any) ([]Message, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get conversation messages: %w", err)
	}
	defer rows.Close()

	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		var attachmentData []byte
		if err := rows.Scan(&message.ID, &message.ConversationID, &message.Role, &message.Content, &attachmentData, &message.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if err := json.Unmarshal(attachmentData, &message.Attachments); err != nil {
			return nil, fmt.Errorf("decode message attachment metadata: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return messages, nil
}

func (r *ChatRepository) SaveMessage(ctx context.Context, conversationID, role, content string) error {
	return r.SaveMessageWithAttachments(ctx, conversationID, role, content, nil)
}

func (r *ChatRepository) SaveMessageWithAttachments(ctx context.Context, conversationID, role, content string, attachments []MessageAttachment) error {
	_, err := r.SaveMessageWithAttachmentsAndID(ctx, conversationID, role, content, attachments)
	return err
}

func (r *ChatRepository) SaveMessageWithAttachmentsAndID(ctx context.Context, conversationID, role, content string, attachments []MessageAttachment) (string, error) {
	if role != "user" && role != "assistant" {
		return "", fmt.Errorf("invalid message role %q", role)
	}
	if attachments == nil {
		attachments = []MessageAttachment{}
	}
	attachmentData, err := json.Marshal(attachments)
	if err != nil {
		return "", fmt.Errorf("encode message attachment metadata: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin save message transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var messageID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO messages (id, conversation_id, role, content, attachments, created_at)
		VALUES (uuid_generate_v4(), $1, $2, $3, $4::jsonb, NOW())
		RETURNING id::text`, conversationID, role, content, string(attachmentData)).Scan(&messageID); err != nil {
		return "", fmt.Errorf("insert message: %w", err)
	}
	result, err := tx.Exec(ctx, `UPDATE conversations SET updated_at = NOW() WHERE id = $1`, conversationID)
	if err != nil {
		return "", fmt.Errorf("update conversation timestamp: %w", err)
	}
	if result.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit save message transaction: %w", err)
	}
	return messageID, nil
}

// ReplaceUserMessageForRetry updates a user's earlier message and removes the
// later turns so the next generation uses the edited conversation history.
func (r *ChatRepository) ReplaceUserMessageForRetry(ctx context.Context, userID, conversationID, messageID, content string, attachments []MessageAttachment) error {
	if attachments == nil {
		attachments = []MessageAttachment{}
	}
	attachmentData, err := json.Marshal(attachments)
	if err != nil {
		return fmt.Errorf("encode retry attachment metadata: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin retry message transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var targetCreatedAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT m.created_at
		FROM messages AS m
		JOIN conversations AS c ON c.id = m.conversation_id
		WHERE m.id = $1 AND m.conversation_id = $2 AND c.user_id = $3 AND m.role = 'user'
		FOR UPDATE OF m`, messageID, conversationID, userID).Scan(&targetCreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("find message to retry: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM messages
		WHERE conversation_id = $1 AND (created_at, id) > ($2, $3::uuid)`, conversationID, targetCreatedAt, messageID); err != nil {
		return fmt.Errorf("remove later conversation messages: %w", err)
	}
	result, err := tx.Exec(ctx, `
		UPDATE messages SET content = $3, attachments = $4::jsonb
		WHERE id = $1 AND conversation_id = $2 AND role = 'user'`, messageID, conversationID, content, string(attachmentData))
	if err != nil {
		return fmt.Errorf("update retried user message: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at = NOW() WHERE id = $1 AND user_id = $2`, conversationID, userID); err != nil {
		return fmt.Errorf("update retried conversation timestamp: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit retry message transaction: %w", err)
	}
	return nil
}

func (r *ChatRepository) UpdateConversationTitle(ctx context.Context, conversationID, title string) error {
	result, err := r.pool.Exec(ctx, `UPDATE conversations SET title = $2, updated_at = NOW() WHERE id = $1`, conversationID, title)
	if err != nil {
		return fmt.Errorf("update conversation title: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ChatRepository) GetConversationForUser(ctx context.Context, conversationID, userID string) (Conversation, error) {
	const query = `
		SELECT id::text, user_id::text, folder_id::text, title, archived_at, created_at, updated_at
		FROM conversations WHERE id = $1 AND user_id = $2`
	var conversation Conversation
	if err := r.pool.QueryRow(ctx, query, conversationID, userID).Scan(
		&conversation.ID, &conversation.UserID, &conversation.FolderID, &conversation.Title, &conversation.ArchivedAt, &conversation.CreatedAt, &conversation.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Conversation{}, ErrNotFound
		}
		return Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	return conversation, nil
}

func (r *ChatRepository) ListArchivedConversations(ctx context.Context, userID string) ([]Conversation, error) {
	const query = `
		SELECT id::text, user_id::text, folder_id::text, title, archived_at, created_at, updated_at
		FROM conversations
		WHERE user_id = $1 AND archived_at IS NOT NULL
		ORDER BY archived_at DESC, updated_at DESC, id DESC`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list archived conversations: %w", err)
	}
	defer rows.Close()
	conversations := make([]Conversation, 0)
	for rows.Next() {
		var conversation Conversation
		if err := rows.Scan(&conversation.ID, &conversation.UserID, &conversation.FolderID, &conversation.Title, &conversation.ArchivedAt, &conversation.CreatedAt, &conversation.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan archived conversation: %w", err)
		}
		conversations = append(conversations, conversation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate archived conversations: %w", err)
	}
	return conversations, nil
}

func (r *ChatRepository) SetConversationArchived(ctx context.Context, userID, conversationID string, archived bool) error {
	const query = `
		UPDATE conversations
		SET archived_at = CASE
			WHEN $3 THEN COALESCE(archived_at, NOW())
			ELSE NULL
		END,
		updated_at = NOW()
		WHERE id = $1 AND user_id = $2`
	result, err := r.pool.Exec(ctx, query, conversationID, userID, archived)
	if err != nil {
		return fmt.Errorf("update conversation archive state: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ChatRepository) DeleteConversation(ctx context.Context, userID, conversationID string) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM conversations WHERE id = $1 AND user_id = $2`, conversationID, userID)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ChatRepository) ListConversationFolders(ctx context.Context, userID string) ([]ConversationFolder, error) {
	const query = `
		SELECT id::text, user_id::text, name, created_at, updated_at
		FROM conversation_folders
		WHERE user_id = $1
		ORDER BY lower(name), created_at ASC`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list conversation folders: %w", err)
	}
	defer rows.Close()
	folders := make([]ConversationFolder, 0)
	for rows.Next() {
		var folder ConversationFolder
		if err := rows.Scan(&folder.ID, &folder.UserID, &folder.Name, &folder.CreatedAt, &folder.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation folder: %w", err)
		}
		folders = append(folders, folder)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation folders: %w", err)
	}
	return folders, nil
}

func (r *ChatRepository) CreateConversationFolder(ctx context.Context, userID, name string) (ConversationFolder, error) {
	const query = `
		INSERT INTO conversation_folders (user_id, name)
		VALUES ($1, $2)
		RETURNING id::text, user_id::text, name, created_at, updated_at`
	var folder ConversationFolder
	if err := r.pool.QueryRow(ctx, query, userID, name).Scan(
		&folder.ID, &folder.UserID, &folder.Name, &folder.CreatedAt, &folder.UpdatedAt,
	); err != nil {
		if isUniqueViolation(err) {
			return ConversationFolder{}, ErrConflict
		}
		return ConversationFolder{}, fmt.Errorf("create conversation folder: %w", err)
	}
	return folder, nil
}

func (r *ChatRepository) RenameConversationFolder(ctx context.Context, userID, folderID, name string) (ConversationFolder, error) {
	const query = `
		UPDATE conversation_folders SET name = $3, updated_at = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING id::text, user_id::text, name, created_at, updated_at`
	var folder ConversationFolder
	if err := r.pool.QueryRow(ctx, query, folderID, userID, name).Scan(
		&folder.ID, &folder.UserID, &folder.Name, &folder.CreatedAt, &folder.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ConversationFolder{}, ErrNotFound
		}
		if isUniqueViolation(err) {
			return ConversationFolder{}, ErrConflict
		}
		return ConversationFolder{}, fmt.Errorf("rename conversation folder: %w", err)
	}
	return folder, nil
}

func (r *ChatRepository) DeleteConversationFolder(ctx context.Context, userID, folderID string) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM conversation_folders WHERE id = $1 AND user_id = $2`, folderID, userID)
	if err != nil {
		return fmt.Errorf("delete conversation folder: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ChatRepository) MoveConversationToFolder(ctx context.Context, userID, conversationID string, folderID *string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin move conversation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var ownedConversationID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM conversations WHERE id = $1 AND user_id = $2 FOR UPDATE`, conversationID, userID).Scan(&ownedConversationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lock conversation for folder move: %w", err)
	}
	if folderID != nil {
		var ownedFolderID string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM conversation_folders WHERE id = $1 AND user_id = $2 FOR KEY SHARE`, *folderID, userID).Scan(&ownedFolderID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrInvalidFolder
			}
			return fmt.Errorf("verify conversation folder ownership: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET folder_id = $3, updated_at = NOW() WHERE id = $1 AND user_id = $2`, conversationID, userID, folderID); err != nil {
		return fmt.Errorf("move conversation to folder: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit conversation folder move: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
