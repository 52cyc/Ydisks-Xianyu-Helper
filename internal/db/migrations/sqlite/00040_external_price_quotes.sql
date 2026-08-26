-- +goose Up
CREATE TABLE external_price_quotes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id TEXT NOT NULL,
    cookie_id TEXT NOT NULL,
    action_id INTEGER NOT NULL,
    unit_cost_cents INTEGER NOT NULL,
    fulfillment_quantity INTEGER NOT NULL,
    fixed_markup_cents INTEGER NOT NULL,
    minimum_profit_cents INTEGER NOT NULL,
    target_order_cents INTEGER NOT NULL,
    dynamic_safe_price TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(order_id, action_id)
);
CREATE INDEX idx_external_price_quotes_order ON external_price_quotes(order_id, status);

-- +goose Down
DROP TABLE IF EXISTS external_price_quotes;
