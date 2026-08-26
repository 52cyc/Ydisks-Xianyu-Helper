-- +goose Up
CREATE TABLE external_price_message_records (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    dedupe_key VARCHAR(255) NOT NULL,
    cookie_id VARCHAR(191) NOT NULL,
    chat_id VARCHAR(191) NOT NULL,
    item_id VARCHAR(191) NOT NULL DEFAULT '',
    rule_id BIGINT NOT NULL,
    order_id VARCHAR(191) NOT NULL DEFAULT '',
    message_kind VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    last_error TEXT NOT NULL,
    lease_expires_at BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_external_price_message_dedupe(dedupe_key),
    KEY idx_external_price_messages_chat(cookie_id, chat_id, status)
);

-- +goose Down
DROP TABLE IF EXISTS external_price_message_records;
