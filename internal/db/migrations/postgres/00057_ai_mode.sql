-- +goose Up
-- 保留 fork 已发布的 00052–00056，新增上游能力从 00057 接续。
ALTER TABLE ai_reply_settings ADD COLUMN ai_mode VARCHAR(32) NOT NULL DEFAULT 'bargain_only'
    CHECK (ai_mode IN ('bargain_only', 'full_service'));

-- +goose Down
ALTER TABLE ai_reply_settings DROP COLUMN ai_mode;
