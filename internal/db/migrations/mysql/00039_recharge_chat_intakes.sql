-- +goose Up
CREATE TABLE recharge_chat_intakes (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    task_key VARCHAR(512) NOT NULL,
    run_id BIGINT NOT NULL DEFAULT 0,
    cookie_id VARCHAR(191) NOT NULL,
    chat_id VARCHAR(191) NOT NULL,
    buyer_id VARCHAR(191) NOT NULL,
    order_id VARCHAR(191) NOT NULL,
    action_id BIGINT NOT NULL,
    field_key VARCHAR(191) NOT NULL,
    field_name VARCHAR(191) NOT NULL DEFAULT '',
    value_secret TEXT NOT NULL,
    masked_value VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'awaiting_input',
    prompt_sent TINYINT(1) NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_recharge_chat_task_field(task_key(255), action_id, field_key),
    KEY idx_recharge_chat_lookup(cookie_id, chat_id, buyer_id, status, updated_at)
);

-- +goose Down
DROP TABLE IF EXISTS recharge_chat_intakes;
