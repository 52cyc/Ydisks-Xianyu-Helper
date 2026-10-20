-- +goose Up
-- 保留 fork 已发布的 00052–00056，新增上游能力从 00057 接续。
CREATE TABLE item_ai_settings (
    cookie_id VARCHAR(255) NOT NULL,
    item_id VARCHAR(255) NOT NULL,
    ai_override VARCHAR(16) NOT NULL DEFAULT 'inherit',
    item_context TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (cookie_id, item_id),
    CONSTRAINT chk_item_ai_settings_override CHECK (ai_override IN ('inherit', 'enabled', 'disabled')),
    CONSTRAINT chk_item_ai_settings_context CHECK (CHAR_LENGTH(item_context) <= 12000),
    CONSTRAINT fk_item_ai_settings_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE item_ai_settings;
