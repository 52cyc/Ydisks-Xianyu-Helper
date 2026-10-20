-- +goose Up
-- 保留 fork 已发布的 00052–00056，新增上游能力从 00057 接续。
ALTER TABLE ai_reply_settings ADD COLUMN ai_mode VARCHAR(32) NOT NULL DEFAULT 'bargain_only',
    ADD CONSTRAINT chk_ai_reply_settings_mode CHECK (ai_mode IN ('bargain_only', 'full_service'));

-- +goose Down
ALTER TABLE ai_reply_settings DROP CHECK chk_ai_reply_settings_mode, DROP COLUMN ai_mode;
