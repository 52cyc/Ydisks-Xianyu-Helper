-- +goose Up
CREATE TABLE external_price_quotes (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    order_id VARCHAR(191) NOT NULL,
    cookie_id VARCHAR(191) NOT NULL,
    action_id BIGINT NOT NULL,
    unit_cost_cents BIGINT NOT NULL,
    fulfillment_quantity INTEGER NOT NULL,
    fixed_markup_cents BIGINT NOT NULL,
    minimum_profit_cents BIGINT NOT NULL,
    target_order_cents BIGINT NOT NULL,
    dynamic_safe_price VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_external_price_quote_order_action(order_id, action_id),
    KEY idx_external_price_quotes_order(order_id, status)
);

-- +goose Down
DROP TABLE IF EXISTS external_price_quotes;
