# Logging Guidelines

> How logging is done in this project.

---

## Overview

The Go service uses `log/slog` with structured key/value fields. Business
components derive subsystem loggers, for example `subsys=automation`, so JSON
and text output expose the same diagnostic dimensions.

---

## Log Levels

- `DEBUG`: an event entered a boundary, or a normal duplicate/ignored message
  was observed. Receipt does not imply that a rule matched or an action ran.
- `INFO`: a user-visible state transition or external action definitely
  succeeded, or a configured gate intentionally deferred work.
- `WARN`: work was skipped or degraded and an operator can act on the reason.
- `ERROR`: a requested business action failed after entering execution.

---

## Structured Logging

Automation event logs must include `account`, `source`, `trigger`, `order_id`,
`item_id`, and `chat_id` when those facts are available. Missing identifiers
remain explicit empty values. Boolean fields such as `has_buyer_id` and
`has_update_key` report presence without copying opaque identifiers.

A rule miss caused by an incomplete platform event must also include:

- `rule_match_scope`: `item_then_account` or `account_only`;
- `failure_reason`: a stable machine-readable reason code;
- `paid_order_resolution`: whether local pending-order recovery was not
  applicable, impossible, missed, or succeeded.

---

## What to Log

For system-card automation, log both sides of the boundary:

1. The adapter logs the normalized facts at `DEBUG` immediately after protocol
   extraction.
2. The automation center logs a rule miss at `WARN` with the actual matching
   scope and any paid-order recovery result.

This makes it possible to distinguish a configured-rule problem from an
upstream payload that never supplied an item or order identifier.

---

## What NOT to Log

Never log Cookie strings, Tokens, passwords, SMTP credentials, supplier
secrets, encrypted metadata, raw WebSocket payloads, delivery card content, or
full `updateKey` values. Prefer presence flags, stable reason codes, and already
approved non-secret business identifiers.
