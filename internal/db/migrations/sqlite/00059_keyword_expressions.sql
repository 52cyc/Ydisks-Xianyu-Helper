-- +goose Up
-- 保留 fork 已发布的 00052–00056，新增上游能力从 00057 接续。
ALTER TABLE keywords ADD COLUMN keyword_expressions TEXT;
ALTER TABLE keywords ADD COLUMN match_type TEXT NOT NULL DEFAULT 'contains';

-- +goose Down
ALTER TABLE keywords DROP COLUMN match_type;
ALTER TABLE keywords DROP COLUMN keyword_expressions;
