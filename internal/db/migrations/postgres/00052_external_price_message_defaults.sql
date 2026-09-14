-- +goose Up
UPDATE automation_rules AS rule_row
SET config_json = (
  CASE
    WHEN BTRIM(rule_row.config_json) LIKE '{%' THEN rule_row.config_json::jsonb
    ELSE '{}'::jsonb
  END || '{"price_guidance_enabled":true,"price_adjusted_notice_enabled":true}'::jsonb
)::text,
updated_at = CURRENT_TIMESTAMP
WHERE rule_row.deleted_at IS NULL
  AND rule_row.trigger_type = 'order_paid'
  AND rule_row.item_id <> ''
  AND EXISTS (
    SELECT 1
    FROM automation_rule_actions AS action
    WHERE action.rule_id = rule_row.id
      AND action.enabled = TRUE
      AND action.action_type = 'send_card'
      AND CASE
        WHEN BTRIM(action.config_json) LIKE '{%'
        THEN action.config_json::jsonb ->> 'source_type' = 'external'
          AND (
            COALESCE((action.config_json::jsonb ->> 'price_sync_enabled')::boolean, FALSE)
            OR COALESCE((action.config_json::jsonb ->> 'pending_price_enabled')::boolean, FALSE)
          )
        ELSE FALSE
      END
  );

-- +goose Down
SELECT 1;
