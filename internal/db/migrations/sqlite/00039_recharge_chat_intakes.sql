-- +goose Up
CREATE TABLE recharge_chat_intakes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_key TEXT NOT NULL,
    run_id INTEGER NOT NULL DEFAULT 0,
    cookie_id TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    buyer_id TEXT NOT NULL,
    order_id TEXT NOT NULL,
    action_id INTEGER NOT NULL,
    field_key TEXT NOT NULL,
    field_name TEXT NOT NULL DEFAULT '',
    value_secret TEXT NOT NULL DEFAULT '',
    masked_value TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'awaiting_input',
    prompt_sent INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(task_key, action_id, field_key)
);
CREATE INDEX idx_recharge_chat_lookup ON recharge_chat_intakes(cookie_id, chat_id, buyer_id, status, updated_at);

-- +goose Down
DROP TABLE IF EXISTS recharge_chat_intakes;
