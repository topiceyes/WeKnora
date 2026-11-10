CREATE TABLE IF NOT EXISTS knowledge_navigation (
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    revision BIGINT NOT NULL DEFAULT 0,
    folders JSONB NOT NULL DEFAULT '[]',
    orders JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (tenant_id, knowledge_base_id)
);
