package db

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationLockID int64 = 0x4e4f44554c4f

type migration struct {
	version int64
	name    string
	up      func(context.Context, pgx.Tx) error
}

var migrations = []migration{
	{version: 1, name: "users", up: migrateUsers},
	{version: 2, name: "conversations_and_messages", up: migrateConversations},
	{version: 3, name: "academic_document_chunks", up: migrateDocumentChunks},
	{version: 4, name: "user_credentials_and_roles", up: migrateUserCredentialsAndRoles},
	{version: 5, name: "unique_user_email", up: migrateUniqueUserEmail},
	{version: 6, name: "ingestion_jobs", up: migrateIngestionJobs},
	{version: 7, name: "ingestion_job_office_types", up: migrateIngestionJobOfficeTypes},
	{version: 8, name: "registration_access_control", up: migrateRegistrationAccessControl},
	{version: 9, name: "optional_institution_identity", up: migrateOptionalInstitutionIdentity},
	{version: 10, name: "user_conversation_folders", up: migrateUserConversationFolders},
	{version: 11, name: "conversation_archive", up: migrateConversationArchive},
	{version: 12, name: "ingestion_job_title", up: migrateIngestionJobTitle},
	{version: 13, name: "message_attachment_metadata", up: migrateMessageAttachmentMetadata},
	{version: 14, name: "chat_upload_settings", up: migrateChatUploadSettings},
	{version: 15, name: "system_error_logs", up: migrateSystemErrorLogs},
	{version: 16, name: "web_search_settings", up: migrateWebSearchSettings},
	{version: 17, name: "user_semantic_memory", up: migrateUserSemanticMemory},
	{version: 18, name: "user_semantic_memory_controls", up: migrateUserSemanticMemoryControls},
	{version: 19, name: "durable_ingestion_queue", up: migrateDurableIngestionQueue},
}

// Migrate serializes schema changes and applies each unapplied migration in a
// transaction. The migration ledger is the only schema state initialized by
// the runner itself; application DDL lives in the versioned Go migrations below.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("initialize migration ledger: %w", err)
	}

	applied, err := loadAppliedMigrations(ctx, conn)
	if err != nil {
		return err
	}

	knownVersions := make(map[int64]string, len(migrations))
	for _, item := range migrations {
		knownVersions[item.version] = item.name
	}
	for version, name := range applied {
		knownName, exists := knownVersions[version]
		if !exists {
			return fmt.Errorf("database contains unknown migration version %d (%s)", version, name)
		}
		if knownName != name {
			return fmt.Errorf("migration %d name mismatch: database has %q, code has %q", version, name, knownName)
		}
	}

	pending := append([]migration(nil), migrations...)
	sort.Slice(pending, func(i, j int) bool { return pending[i].version < pending[j].version })
	for _, item := range pending {
		if _, exists := applied[item.version]; exists {
			continue
		}
		if err := applyMigration(ctx, conn, item); err != nil {
			return err
		}
	}
	return nil
}

func loadAppliedMigrations(ctx context.Context, conn *pgxpool.Conn) (map[int64]string, error) {
	rows, err := conn.Query(ctx, "SELECT version, name FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("read migration ledger: %w", err)
	}
	defer rows.Close()

	applied := make(map[int64]string)
	for rows.Next() {
		var version int64
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			return nil, fmt.Errorf("scan migration ledger: %w", err)
		}
		applied[version] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migration ledger: %w", err)
	}
	return applied, nil
}

func applyMigration(ctx context.Context, conn *pgxpool.Conn, item migration) error {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin migration %d (%s): %w", item.version, item.name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := item.up(ctx, tx); err != nil {
		return fmt.Errorf("apply migration %d (%s): %w", item.version, item.name, err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", item.version, item.name,
	); err != nil {
		return fmt.Errorf("record migration %d (%s): %w", item.version, item.name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %d (%s): %w", item.version, item.name, err)
	}
	return nil
}

func migrateUsers(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
		`CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO users (id, email, name, created_at)
		 VALUES ('00000000-0000-0000-0000-000000000001', 'academico@uesb.br', 'Utilizador Académico', CURRENT_TIMESTAMP)
		 ON CONFLICT (id) DO NOTHING`,
	}
	return executeMigrationStatements(ctx, tx, statements)
}

func migrateConversations(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS conversations (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			role TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
			content TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_user_updated_at ON conversations (user_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_conversation_created_at ON messages (conversation_id, created_at ASC)`,
	}
	return executeMigrationStatements(ctx, tx, statements)
}

func migrateDocumentChunks(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		`CREATE TABLE IF NOT EXISTS document_chunks (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			title TEXT NOT NULL,
			source_reference TEXT NOT NULL,
			content TEXT NOT NULL,
			embedding vector(768) NOT NULL,
			metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_document_chunks_embedding_cosine
			ON document_chunks USING hnsw (embedding vector_cosine_ops)`,
		`CREATE INDEX IF NOT EXISTS idx_document_chunks_source_reference ON document_chunks (source_reference)`,
	}
	return executeMigrationStatements(ctx, tx, statements)
}

func migrateUserCredentialsAndRoles(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT`,
		`UPDATE users SET role = 'user' WHERE role IS NULL`,
		`ALTER TABLE users ALTER COLUMN role SET DEFAULT 'user'`,
		`ALTER TABLE users ALTER COLUMN role SET NOT NULL`,
		`DO $migration$
		 BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conname = 'users_role_check'
				  AND conrelid = 'users'::regclass
			) THEN
				ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('user', 'admin'));
			END IF;
		 END
		 $migration$`,
		`CREATE INDEX IF NOT EXISTS idx_users_role ON users (role)`,
	}
	return executeMigrationStatements(ctx, tx, statements)
}

func migrateUniqueUserEmail(ctx context.Context, tx pgx.Tx) error {
	var hasDuplicateEmails bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			GROUP BY email
			HAVING COUNT(*) > 1
		)`,
	).Scan(&hasDuplicateEmails); err != nil {
		return fmt.Errorf("check for duplicate user e-mails: %w", err)
	}
	if hasDuplicateEmails {
		return fmt.Errorf("cannot enforce unique users.email: duplicate e-mail values exist; resolve duplicates and rerun migrations")
	}

	var hasUniqueEmailConstraint bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_constraint AS constraint_info
			JOIN pg_attribute AS column_info
			  ON column_info.attrelid = constraint_info.conrelid
			 AND column_info.attnum = constraint_info.conkey[1]
			WHERE constraint_info.conrelid = 'users'::regclass
			  AND constraint_info.contype = 'u'
			  AND array_length(constraint_info.conkey, 1) = 1
			  AND column_info.attname = 'email'
		)`).Scan(&hasUniqueEmailConstraint); err != nil {
		return fmt.Errorf("check unique constraint on users.email: %w", err)
	}
	if hasUniqueEmailConstraint {
		return nil
	}

	if _, err := tx.Exec(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_unique ON users (email)`); err != nil {
		return fmt.Errorf("create unique index on users.email: %w", err)
	}
	return nil
}

func migrateIngestionJobs(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS ingestion_jobs (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			filename TEXT NOT NULL,
			source_reference TEXT NOT NULL,
			file_type TEXT NOT NULL CHECK (file_type IN ('pdf', 'zip', 'url', 'text')),
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
			total_chunks INTEGER NOT NULL DEFAULT 0 CHECK (total_chunks >= 0),
			processed_chunks INTEGER NOT NULL DEFAULT 0 CHECK (processed_chunks >= 0),
			error_message TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ingestion_jobs_status ON ingestion_jobs (status, created_at DESC)`,
	}
	return executeMigrationStatements(ctx, tx, statements)
}

func migrateIngestionJobOfficeTypes(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		`ALTER TABLE ingestion_jobs DROP CONSTRAINT IF EXISTS ingestion_jobs_file_type_check`,
		`ALTER TABLE ingestion_jobs ADD CONSTRAINT ingestion_jobs_file_type_check
			CHECK (file_type IN ('pdf', 'zip', 'url', 'text', 'docx', 'pptx'))`,
	}
	return executeMigrationStatements(ctx, tx, statements)
}

func migrateIngestionJobTitle(ctx context.Context, tx pgx.Tx) error {
	return executeMigrationStatements(ctx, tx, []string{
		`ALTER TABLE ingestion_jobs ADD COLUMN IF NOT EXISTS title TEXT NOT NULL DEFAULT ''`,
	})
}

func migrateMessageAttachmentMetadata(ctx context.Context, tx pgx.Tx) error {
	return executeMigrationStatements(ctx, tx, []string{
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS attachments JSONB NOT NULL DEFAULT '[]'::jsonb`,
	})
}

func migrateChatUploadSettings(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS chat_upload_settings (
			id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
			max_files INTEGER NOT NULL DEFAULT 30 CHECK (max_files >= 0),
			max_file_size_mib BIGINT NOT NULL DEFAULT 50 CHECK (max_file_size_mib >= 0),
			max_total_size_mib BIGINT NOT NULL DEFAULT 0 CHECK (max_total_size_mib >= 0),
			updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO chat_upload_settings (id) VALUES (TRUE) ON CONFLICT (id) DO NOTHING`,
	}
	return executeMigrationStatements(ctx, tx, statements)
}

func migrateRegistrationAccessControl(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS registration_status TEXT NOT NULL DEFAULT 'approved'`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS rejection_reason TEXT`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMPTZ`,
		`DO $migration$ BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conname = 'users_registration_status_check'
				  AND conrelid = 'users'::regclass
			) THEN
				ALTER TABLE users ADD CONSTRAINT users_registration_status_check
				CHECK (registration_status IN ('pending', 'approved', 'rejected'));
			END IF;
		END $migration$`,
		`CREATE INDEX IF NOT EXISTS idx_users_registration_status_created
			ON users (registration_status, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS registration_settings (
			id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
			mode TEXT NOT NULL DEFAULT 'approval' CHECK (mode IN ('automatic', 'approval')),
			updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO registration_settings (id, mode) VALUES (TRUE, 'approval')
			ON CONFLICT (id) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS registration_allowed_domains (
			domain TEXT PRIMARY KEY,
			created_by UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS registration_allowlist (
			email TEXT PRIMARY KEY,
			note TEXT NOT NULL DEFAULT '',
			created_by UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	return executeMigrationStatements(ctx, tx, statements)
}

func migrateOptionalInstitutionIdentity(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS institutional_settings (
			id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
			show_institution BOOLEAN NOT NULL DEFAULT FALSE,
			institution_name TEXT NOT NULL DEFAULT '',
			institution_acronym TEXT NOT NULL DEFAULT '',
			institution_unit TEXT NOT NULL DEFAULT '',
			updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO institutional_settings (id) VALUES (TRUE) ON CONFLICT (id) DO NOTHING`,
		`UPDATE users SET email = 'academic@nodulo.local', name = 'Utilizador de demonstração'
			WHERE id = '00000000-0000-0000-0000-000000000001'
			  AND email = 'academico@uesb.br'
			  AND NOT EXISTS (SELECT 1 FROM users WHERE email = 'academic@nodulo.local' AND id <> '00000000-0000-0000-0000-000000000001')`,
	}
	return executeMigrationStatements(ctx, tx, statements)
}

func migrateUserConversationFolders(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS conversation_folders (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_folders_user_name
			ON conversation_folders (user_id, lower(name))`,
		`ALTER TABLE conversations
			ADD COLUMN IF NOT EXISTS folder_id UUID REFERENCES conversation_folders(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_user_folder_updated
			ON conversations (user_id, folder_id, updated_at DESC)`,
	}
	return executeMigrationStatements(ctx, tx, statements)
}

func migrateConversationArchive(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_user_archived_updated
			ON conversations (user_id, archived_at, updated_at DESC)`,
	}
	return executeMigrationStatements(ctx, tx, statements)
}

func migrateSystemErrorLogs(ctx context.Context, tx pgx.Tx) error {
	return executeMigrationStatements(ctx, tx, []string{
		`CREATE TABLE IF NOT EXISTS system_error_logs (
			id BIGSERIAL PRIMARY KEY,
			occurred_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			level TEXT NOT NULL CHECK (level IN ('WARN', 'ERROR')),
			source TEXT NOT NULL DEFAULT 'application',
			message TEXT NOT NULL,
			details TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			actor_user_id UUID,
			method TEXT NOT NULL DEFAULT '',
			route TEXT NOT NULL DEFAULT '',
			status_code INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_system_error_logs_occurred
			ON system_error_logs (occurred_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_system_error_logs_request
			ON system_error_logs (request_id) WHERE request_id <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_system_error_logs_actor
			ON system_error_logs (actor_user_id, occurred_at DESC) WHERE actor_user_id IS NOT NULL`,
	})
}

func migrateWebSearchSettings(ctx context.Context, tx pgx.Tx) error {
	return executeMigrationStatements(ctx, tx, []string{
		`CREATE TABLE IF NOT EXISTS web_search_settings (
			id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			provider TEXT NOT NULL DEFAULT 'duckduckgo' CHECK (provider IN ('duckduckgo', 'searxng')),
			endpoint TEXT NOT NULL DEFAULT '',
			result_limit INTEGER NOT NULL DEFAULT 5 CHECK (result_limit BETWEEN 1 AND 10),
			updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO web_search_settings (id, enabled, provider, endpoint, result_limit)
			VALUES (TRUE, TRUE, 'duckduckgo', '', 5) ON CONFLICT (id) DO NOTHING`,
	})
}

func migrateUserSemanticMemory(ctx context.Context, tx pgx.Tx) error {
	return executeMigrationStatements(ctx, tx, []string{
		`CREATE TABLE IF NOT EXISTS user_memory (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			fact TEXT NOT NULL CHECK (length(btrim(fact)) > 0),
			embedding vector(768) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_memory_user_created_at
			ON user_memory (user_id, created_at DESC)`,
	})
}

func migrateUserSemanticMemoryControls(ctx context.Context, tx pgx.Tx) error {
	return executeMigrationStatements(ctx, tx, []string{
		`CREATE TABLE IF NOT EXISTS user_memory_settings (
			user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			enabled BOOLEAN NOT NULL DEFAULT FALSE,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	})
}

func migrateDurableIngestionQueue(ctx context.Context, tx pgx.Tx) error {
	return executeMigrationStatements(ctx, tx, []string{
		`ALTER TABLE ingestion_jobs ADD COLUMN IF NOT EXISTS payload_path TEXT`,
		`ALTER TABLE ingestion_jobs ADD COLUMN IF NOT EXISTS source_url TEXT`,
		`ALTER TABLE ingestion_jobs ADD COLUMN IF NOT EXISTS lease_owner TEXT`,
		`ALTER TABLE ingestion_jobs ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ`,
		`CREATE INDEX IF NOT EXISTS idx_ingestion_jobs_claimable ON ingestion_jobs (status, lease_expires_at, created_at)`,
		`UPDATE ingestion_jobs SET status = 'failed',
			error_message = 'Este job foi criado antes do armazenamento persistente da fila. Envie novamente o arquivo ou a URL.',
			updated_at = NOW()
			WHERE status IN ('pending', 'processing') AND payload_path IS NULL AND source_url IS NULL`,
	})
}

func executeMigrationStatements(ctx context.Context, tx pgx.Tx, statements []string) error {
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
