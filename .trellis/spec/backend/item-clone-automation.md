# Cross-account Item Clone Automation

## Scope

When the item-list clone flow publishes a selected source item to another account owned by the same user, the successful target item inherits every automation rule explicitly bound to the source account and source item.

## Contracts

- The frontend clone snapshot carries the source account ID and source item ID through the existing batch-preview pipeline.
- Rules are copied only after the remote publish returns the target item ID. The target account ID and item ID replace the source binding.
- Preserve rule enabled state, priority, configuration, SKU migration state, action order, card references, delivery-template bindings, custom variables, delays, and action enabled state.
- Do not copy account-wide rules, automation runs, orders, quotes, deferred tasks, or issue history.
- Every cloned target rule stores `clone_source_rule_id` in its rule configuration. Batch recovery uses this value together with the target account and target item to avoid duplicate rule creation.
- A post-publish rule-copy failure remains a local post-publish error. Recovery may retry local closure but must never publish the remote item again.

## Multi-card message formatting

- A single supplier delivery value remains unchanged.
- Two or more supplier card values are rendered as separate `卡密 1：...`, `卡密 2：...` lines before replacing `{delivery_content}`.
- Empty supplier values do not consume a display sequence number. Embedded newlines inside one card value remain part of that numbered card.

## Required tests

- Frontend clone CSV tests assert both source identifiers are present.
- Batch preview tests assert the source identifiers survive normalization and persistence configuration.
- Application tests assert all source item rules are copied without old rule/action IDs and preserve disabled rules.
- Adapter tests assert account-wide rules are excluded and repeated clone closure is idempotent.
- Automation tests assert single-card compatibility and numbered multi-card output.
