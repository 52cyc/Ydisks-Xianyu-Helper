-- +goose Up
UPDATE automation_rules
SET config_json = json_set(
  CASE
    WHEN json_valid(config_json) AND json_type(config_json) = 'object' THEN config_json
    ELSE '{}'
  END,
  '$.price_guidance_enabled', json('true'),
  '$.price_adjusted_notice_enabled', json('true')
),
updated_at = CURRENT_TIMESTAMP
WHERE deleted_at IS NULL
  AND trigger_type = 'order_paid'
  AND item_id <> ''
  AND EXISTS (
    SELECT 1
    FROM automation_rule_actions action
    WHERE action.rule_id = automation_rules.id
      AND action.enabled = 1
      AND action.action_type = 'send_card'
      AND json_valid(action.config_json)
      AND json_extract(action.config_json, '$.source_type') = 'external'
      AND (
        COALESCE(json_extract(action.config_json, '$.price_sync_enabled'), 0) = 1
        OR COALESCE(json_extract(action.config_json, '$.pending_price_enabled'), 0) = 1
      )
  );

-- +goose Down
SELECT 1;
