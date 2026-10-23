CREATE TABLE IF NOT EXISTS knowledge_content_versions (
    id                VARCHAR(36)  PRIMARY KEY,
    tenant_id         BIGINT       NOT NULL,
    knowledge_base_id VARCHAR(36)  NOT NULL,
    knowledge_id      VARCHAR(36)  NOT NULL,
    version           INTEGER      NOT NULL,
    title             VARCHAR(512) NOT NULL DEFAULT '',
    content           TEXT         NOT NULL DEFAULT '',
    created_by        VARCHAR(64)  NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_kcv_knowledge_version
    ON knowledge_content_versions (knowledge_id, version);
CREATE INDEX IF NOT EXISTS idx_kcv_knowledge_base_id
    ON knowledge_content_versions (knowledge_base_id);
CREATE INDEX IF NOT EXISTS idx_kcv_tenant_id
    ON knowledge_content_versions (tenant_id);
