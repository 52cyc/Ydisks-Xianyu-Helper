-- +goose Up
CREATE TABLE fulfillment_instances (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  public_id TEXT NOT NULL UNIQUE,
  user_id INTEGER NOT NULL,
  name TEXT NOT NULL,
  provider TEXT NOT NULL DEFAULT 'kasushou_v2',
  base_url TEXT NOT NULL,
  merchant_user_id TEXT NOT NULL,
  api_key TEXT NOT NULL,
  capabilities_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(user_id, name),
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX idx_fulfillment_instances_user ON fulfillment_instances(user_id, enabled);

CREATE TABLE fulfillment_mappings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  account_id TEXT NOT NULL,
  item_id TEXT NOT NULL,
  spec_name TEXT NOT NULL DEFAULT '',
  spec_value TEXT NOT NULL DEFAULT '',
  instance_id INTEGER NOT NULL,
  remote_goods_id INTEGER NOT NULL,
  goods_type INTEGER NOT NULL,
  safe_price TEXT NOT NULL DEFAULT '',
  quantity_mode TEXT NOT NULL DEFAULT 'order_quantity',
  fixed_quantity INTEGER NOT NULL DEFAULT 1,
  attach_mapping_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(user_id, account_id, item_id, spec_name, spec_value),
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(instance_id) REFERENCES fulfillment_instances(id) ON DELETE RESTRICT
);
CREATE INDEX idx_fulfillment_mappings_match ON fulfillment_mappings(user_id, account_id, item_id, enabled);

CREATE TABLE fulfillment_orders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  instance_id INTEGER NOT NULL,
  external_order_no TEXT NOT NULL,
  remote_order_no TEXT NOT NULL DEFAULT '',
  xianyu_order_id TEXT NOT NULL DEFAULT '',
  remote_goods_id INTEGER NOT NULL,
  quantity INTEGER NOT NULL,
  status INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT 'created',
  total_price TEXT NOT NULL DEFAULT '',
  result_secret TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(user_id, external_order_no),
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(instance_id) REFERENCES fulfillment_instances(id) ON DELETE RESTRICT
);
CREATE INDEX idx_fulfillment_orders_user_updated ON fulfillment_orders(user_id, updated_at DESC);
CREATE INDEX idx_fulfillment_orders_remote ON fulfillment_orders(instance_id, remote_order_no);

-- +goose Down
DROP TABLE IF EXISTS fulfillment_orders;
DROP TABLE IF EXISTS fulfillment_mappings;
DROP TABLE IF EXISTS fulfillment_instances;
