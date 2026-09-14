-- +goose Up
UPDATE automation_rules rule_row
SET config_json = JSON_SET(
  IF(JSON_VALID(rule_row.config_json), rule_row.config_json, '{}'),
  '$.price_guidance_enabled', JSON_EXTRACT('true', '$'),
  '$.price_adjusted_notice_enabled', JSON_EXTRACT('true', '$')
),
updated_at = CURRENT_TIMESTAMP
WHERE rule_row.deleted_at IS NULL
  AND rule_row.trigger_type = 'order_paid'
  AND rule_row.item_id <> ''
  AND EXISTS (
    SELECT 1
    FROM automation_rule_actions action
    WHERE action.rule_id = rule_row.id
      AND action.enabled = TRUE
      AND action.action_type = 'send_card'
      AND JSON_VALID(action.config_json)
      AND CASE
        WHEN JSON_VALID(action.config_json)
        THEN JSON_UNQUOTE(JSON_EXTRACT(action.config_json, '$.source_type')) = 'external'
          AND (
            JSON_UNQUOTE(JSON_EXTRACT(action.config_json, '$.price_sync_enabled')) IN ('true', '1')
            OR JSON_UNQUOTE(JSON_EXTRACT(action.config_json, '$.pending_price_enabled')) IN ('true', '1')
          )
        ELSE FALSE
      END
  );

-- +goose Down
SELECT 1;
