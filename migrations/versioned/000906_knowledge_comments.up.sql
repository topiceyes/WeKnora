CREATE TABLE IF NOT EXISTS knowledge_comments (
 id VARCHAR(36) PRIMARY KEY,
 knowledge_id VARCHAR(36) NOT NULL,
 parent_id VARCHAR(36) NOT NULL DEFAULT '',
 author_id VARCHAR(36) NOT NULL,
 author_name TEXT NOT NULL,
 body TEXT NOT NULL,
 quote TEXT NOT NULL DEFAULT '',
 prefix TEXT NOT NULL DEFAULT '',
 suffix TEXT NOT NULL DEFAULT '',
 content_version INTEGER NOT NULL DEFAULT 0,
 resolved BOOLEAN NOT NULL DEFAULT FALSE,
 deleted BOOLEAN NOT NULL DEFAULT FALSE,
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_knowledge_comments_thread ON knowledge_comments (knowledge_id, parent_id, created_at, id);
