CREATE TABLE IF NOT EXISTS knowledge_navigation (
    tenant_id INTEGER NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 0,
    folders TEXT NOT NULL DEFAULT '[]',
    orders TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (tenant_id, knowledge_base_id)
);
