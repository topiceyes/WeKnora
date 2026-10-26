-- Migration 000903: DingTalk 通讯录全员快照 —— 组织浏览/批量导入的数据源。
-- 每日通讯录同步时全量替换（见 dingtalk.Repository.ReplaceContacts）。
-- 与 user_dingtalk_identities 的区别：identity 表只覆盖已绑定本地账号的人
-- （同步刻意不为陌生人建号），本表落全员（含未注册者），供共享空间
-- 「从钉钉组织架构添加」浏览挑选；导入时才为选中者建号并回填 identity。
-- ding_user_id 只存真实企业内部 userid，永不存 "union:" 占位。

CREATE TABLE IF NOT EXISTS dingtalk_contacts (
    corp_id        VARCHAR(128) NOT NULL DEFAULT '',
    ding_user_id   VARCHAR(128) NOT NULL,
    union_id       VARCHAR(128) NOT NULL DEFAULT '',
    name           VARCHAR(255) NOT NULL DEFAULT '',
    mobile         VARCHAR(64)  NOT NULL DEFAULT '',
    email          VARCHAR(255) NOT NULL DEFAULT '',
    title          VARCHAR(255) NOT NULL DEFAULT '',
    job_number     VARCHAR(64)  NOT NULL DEFAULT '',
    dept_id_list   JSONB        NOT NULL DEFAULT '[]',        -- 所在部门 id 列表
    last_synced_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (corp_id, ding_user_id)
);
