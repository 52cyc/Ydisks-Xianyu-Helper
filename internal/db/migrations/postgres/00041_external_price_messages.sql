-- +goose Up
CREATE TABLE external_price_message_records (
    id BIGSERIAL PRIMARY KEY,
    dedupe_key TEXT NOT NULL UNIQUE,
    cookie_id TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    item_id TEXT NOT NULL DEFAULT '',
    rule_id BIGINT NOT NULL,
    order_id TEXT NOT NULL DEFAULT '',
    message_kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    last_error TEXT NOT NULL DEFAULT '',
    lease_expires_at BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_external_price_messages_chat ON external_price_message_records(cookie_id, chat_id, status);

-- +goose Down
DROP TABLE IF EXISTS external_price_message_records;
