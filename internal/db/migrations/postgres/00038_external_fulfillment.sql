-- +goose Up
CREATE TABLE fulfillment_instances (
  id BIGSERIAL PRIMARY KEY,
  public_id VARCHAR(64) NOT NULL UNIQUE,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name VARCHAR(191) NOT NULL,
  provider VARCHAR(64) NOT NULL DEFAULT 'kasushou_v2',
  base_url TEXT NOT NULL,
  merchant_user_id VARCHAR(191) NOT NULL,
  api_key TEXT NOT NULL,
  capabilities_json TEXT NOT NULL DEFAULT '{}',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(user_id, name)
);
CREATE INDEX idx_fulfillment_instances_user ON fulfillment_instances(user_id, enabled);

CREATE TABLE fulfillment_mappings (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id VARCHAR(191) NOT NULL,
  item_id VARCHAR(191) NOT NULL,
  spec_name VARCHAR(191) NOT NULL DEFAULT '',
  spec_value VARCHAR(191) NOT NULL DEFAULT '',
  instance_id BIGINT NOT NULL REFERENCES fulfillment_instances(id) ON DELETE RESTRICT,
  remote_goods_id BIGINT NOT NULL,
  goods_type INTEGER NOT NULL,
  safe_price VARCHAR(64) NOT NULL DEFAULT '',
  quantity_mode VARCHAR(32) NOT NULL DEFAULT 'order_quantity',
  fixed_quantity INTEGER NOT NULL DEFAULT 1,
  attach_mapping_json TEXT NOT NULL DEFAULT '{}',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(user_id, account_id, item_id, spec_name, spec_value)
);
CREATE INDEX idx_fulfillment_mappings_match ON fulfillment_mappings(user_id, account_id, item_id, enabled);

CREATE TABLE fulfillment_orders (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  instance_id BIGINT NOT NULL REFERENCES fulfillment_instances(id) ON DELETE RESTRICT,
  external_order_no VARCHAR(191) NOT NULL,
  remote_order_no VARCHAR(191) NOT NULL DEFAULT '',
  xianyu_order_id VARCHAR(191) NOT NULL DEFAULT '',
  remote_goods_id BIGINT NOT NULL,
  quantity INTEGER NOT NULL,
  status INTEGER NOT NULL DEFAULT 0,
  state VARCHAR(32) NOT NULL DEFAULT 'created',
  total_price VARCHAR(64) NOT NULL DEFAULT '',
  result_secret TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(user_id, external_order_no)
);
CREATE INDEX idx_fulfillment_orders_user_updated ON fulfillment_orders(user_id, updated_at DESC);
CREATE INDEX idx_fulfillment_orders_remote ON fulfillment_orders(instance_id, remote_order_no);

-- +goose Down
DROP TABLE IF EXISTS fulfillment_orders;
DROP TABLE IF EXISTS fulfillment_mappings;
DROP TABLE IF EXISTS fulfillment_instances;
