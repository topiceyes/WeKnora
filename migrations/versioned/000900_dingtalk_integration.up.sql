-- Migration 000900: DingTalk integration — 扫码登录身份绑定 + 通讯录同步记录。
-- 不动 users 表（避免侵入上游），钉钉外部身份独立成表，通过 user_id 关联。

-- 钉钉身份绑定表：一条记录 = 一个本地账号绑定一个钉钉身份。
-- 「用户是否钉钉来源」由此表有无记录判断，天然排除本地/OIDC 账号。
CREATE TABLE IF NOT EXISTS user_dingtalk_identities (
    id               BIGSERIAL PRIMARY KEY,
    user_id          VARCHAR(36)  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    union_id         VARCHAR(128) NOT NULL DEFAULT '',          -- 钉钉开放平台统一身份（跨企业唯一）
    ding_user_id     VARCHAR(128) NOT NULL,                     -- 企业内部 userid，通讯录对账主键
    corp_id          VARCHAR(128) NOT NULL DEFAULT '',          -- 企业 id，预留多企业隔离
    nick             VARCHAR(255) NOT NULL DEFAULT '',
    mobile           VARCHAR(64)  NOT NULL DEFAULT '',
    email            VARCHAR(255) NOT NULL DEFAULT '',
    dept_id_list     JSONB        NOT NULL DEFAULT '[]',        -- 最近同步时所在部门 id 列表（备查）
    job_number       VARCHAR(64)  NOT NULL DEFAULT '',          -- 工号
    title            VARCHAR(255) NOT NULL DEFAULT '',          -- 职位
    user_access_token TEXT        NOT NULL DEFAULT '',          -- AES-GCM 加密的 userToken（v1 不持久化，字段预留）
    token_expires_at TIMESTAMPTZ,
    last_login_at    TIMESTAMPTZ,
    last_synced_at   TIMESTAMPTZ,                               -- 最近一次对账确认此人在职的时间
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deleted_at       TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_dingtalk_identity_union_corp
    ON user_dingtalk_identities (union_id, corp_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_dingtalk_identity_dinguser_corp
    ON user_dingtalk_identities (ding_user_id, corp_id) WHERE deleted_at IS NULL;
-- 一个本地账号只允许绑定一个钉钉身份
CREATE UNIQUE INDEX IF NOT EXISTS uniq_dingtalk_identity_user
    ON user_dingtalk_identities (user_id) WHERE deleted_at IS NULL;

-- 通讯录同步运行记录表：每次 cron/手动触发一行，供回溯审计。
CREATE TABLE IF NOT EXISTS dingtalk_sync_runs (
    id                BIGSERIAL PRIMARY KEY,
    trigger_type      VARCHAR(20) NOT NULL,                     -- 'cron' | 'manual'
    status            VARCHAR(20) NOT NULL,                     -- 'running' | 'success' | 'failed'
    started_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at       TIMESTAMPTZ,
    total_remote      INT NOT NULL DEFAULT 0,                   -- 钉钉侧拉到的在职人数
    created_count     INT NOT NULL DEFAULT 0,
    updated_count     INT NOT NULL DEFAULT 0,
    deactivated_count INT NOT NULL DEFAULT 0,                   -- 本次判离职并禁用的人数
    error_message     TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_dingtalk_sync_runs_started ON dingtalk_sync_runs (started_at DESC);
