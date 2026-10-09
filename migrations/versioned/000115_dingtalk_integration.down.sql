-- Migration 000115 down: drop DingTalk integration tables.
DROP TABLE IF EXISTS dingtalk_sync_runs;
DROP TABLE IF EXISTS user_dingtalk_identities;
