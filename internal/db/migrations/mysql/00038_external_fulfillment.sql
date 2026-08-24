-- +goose Up
CREATE TABLE fulfillment_instances (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  public_id VARCHAR(64) NOT NULL UNIQUE,
  user_id BIGINT NOT NULL,
  name VARCHAR(191) NOT NULL,
  provider VARCHAR(64) NOT NULL DEFAULT 'kasushou_v2',
  base_url TEXT NOT NULL,
  merchant_user_id VARCHAR(191) NOT NULL,
  api_key TEXT NOT NULL,
  capabilities_json TEXT NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_fulfillment_instances_user_name(user_id, name),
  KEY idx_fulfillment_instances_user(user_id, enabled),
  CONSTRAINT fk_fulfillment_instances_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE fulfillment_mappings (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT NOT NULL,
  account_id VARCHAR(191) NOT NULL,
  item_id VARCHAR(191) NOT NULL,
  spec_name VARCHAR(191) NOT NULL DEFAULT '',
  spec_value VARCHAR(191) NOT NULL DEFAULT '',
  instance_id BIGINT NOT NULL,
  remote_goods_id BIGINT NOT NULL,
  goods_type INT NOT NULL,
  safe_price VARCHAR(64) NOT NULL DEFAULT '',
  quantity_mode VARCHAR(32) NOT NULL DEFAULT 'order_quantity',
  fixed_quantity INT NOT NULL DEFAULT 1,
  attach_mapping_json TEXT NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_fulfillment_mapping(user_id, account_id, item_id, spec_name, spec_value),
  KEY idx_fulfillment_mappings_match(user_id, account_id, item_id, enabled),
  CONSTRAINT fk_fulfillment_mappings_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_fulfillment_mappings_instance FOREIGN KEY(instance_id) REFERENCES fulfillment_instances(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE fulfillment_orders (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT NOT NULL,
  instance_id BIGINT NOT NULL,
  external_order_no VARCHAR(191) NOT NULL,
  remote_order_no VARCHAR(191) NOT NULL DEFAULT '',
  xianyu_order_id VARCHAR(191) NOT NULL DEFAULT '',
  remote_goods_id BIGINT NOT NULL,
  quantity INT NOT NULL,
  status INT NOT NULL DEFAULT 0,
  state VARCHAR(32) NOT NULL DEFAULT 'created',
  total_price VARCHAR(64) NOT NULL DEFAULT '',
  result_secret MEDIUMTEXT NOT NULL,
  error_message TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_fulfillment_order_external(user_id, external_order_no),
  KEY idx_fulfillment_orders_user_updated(user_id, updated_at),
  KEY idx_fulfillment_orders_remote(instance_id, remote_order_no),
  CONSTRAINT fk_fulfillment_orders_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_fulfillment_orders_instance FOREIGN KEY(instance_id) REFERENCES fulfillment_instances(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS fulfillment_orders;
DROP TABLE IF EXISTS fulfillment_mappings;
DROP TABLE IF EXISTS fulfillment_instances;
