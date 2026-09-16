# External Fulfillment Catalog and Price Sync

## 1. Scope and trigger

This contract applies when a fulfillment provider exposes catalog browsing for the batch-listing flow, and when the background automation scheduler refreshes the price of an already-linked external product.

The current catalog-capable providers are `kasushou_v2` and `kayixin_v3`. One supplier SKU maps to one Xianyu item. The Kayixin catalog integration accepts only card products with a single supplier specification.

## 2. Interfaces and ownership

- Provider clients implement the consumer-owned `fulfillment.CatalogGateway` interface through `ListCategories` and `ListProductPage`.
- `fulfillment.ProductPage.TotalPages` carries the supplier's page count when available. It is optional at the HTTP boundary so older providers and callers remain compatible.
- The multi-provider adapter selects a provider by instance configuration. HTTP handlers do not contain supplier protocol rules.
- The frontend uses the shared fulfillment API adapter and never calls supplier endpoints directly.
- The Kasushou protocol client owns its upstream request pacing. Every catalog, detail, quote, purchase, and order-query call for the same normalized station URL and merchant ID shares one serial queue. Other providers keep their existing protocol-specific behavior.
- `fulfillment.ErrProductNotFound` is emitted by a provider product-detail path only after the instance has been resolved and the response explicitly reports that the product itself is missing, deleted, or delisted. The application service only propagates this typed result. The adapter maps it to consumer-owned `automation.ErrExternalProductNotFound`; generic instance, merchant, user, order, or ownership `ErrNotFound` must not cross this boundary as a deleted product.

## 3. Kayixin protocol mapping

| Operation | Request | Required mapping |
|---|---|---|
| Categories | `POST /api/v3/goods/getDirs` with `{}` | Recursively map supplier directory `id`, `name`, and `children`. |
| Product page | `POST /api/v3/goods/getList` | Send `goodsType: "1"`, `skuType: "0"`, `showDirId: "1"`, `dirId`, trimmed `keyword`, and `page`. |
| Pagination | Product-list response | Map `allCount` to `Total` and `allPage` to `TotalPages`. |
| Product detail | Existing detail endpoint | Map description, detail text, purchase notice, min/max quantity, current price, and stock. |

The existing Kayixin request signing algorithm, purchase semantics, and order-query semantics remain unchanged. List responses that omit stock use `-1` to mean “unknown until detail lookup”; detail responses must report positive stock before listing.

For a detail response whose minimum purchase quantity is greater than one, the batch row uses that minimum as the automation action's per-item delivery count. Initial listing price and later price sync both round one supplier unit up to cents after markup and then multiply by the delivery count. Available Xianyu quantity is capped by supplier stock and the supplier's per-order maximum after the same conversion. The static safe price is the supplier unit price multiplied by the delivery count.

## 4. Validation and error matrix

| Condition | Expected behavior |
|---|---|
| Provider is not catalog-capable | Return the existing unsupported-provider error. |
| Category or list API returns a supplier business error | Return a provider-scoped error; do not synthesize an empty successful page. |
| List stock is absent | Preserve the product as selectable with stock `-1`; fetch detail before publishing. |
| Detail stock is zero or negative | Reject that product before starting the listing batch. |
| Stock is below the supplier minimum, or minimum exceeds maximum | Reject that product before starting the listing batch. |
| Detail request fails | Stop batch preparation and show the product-specific failure. |
| One background quote fails | Record the rule failure and continue scanning later rules. |
| Background listing quote confirms the supplier product no longer exists | Reuse the normal Xianyu item-price update path to set `9999.00`, persist the local item price after remote success, and continue the scan. |
| Background listing quote returns the product with `CanBuy=false` | Treat the status or stock as explicitly unavailable, reuse the normal Xianyu item-price update path to set `9999.00`, persist the local item price after remote success, and continue the scan. |
| A product-bound external fulfillment rule enables dynamic price sync | New manual/API rules and batch-publish rules default both first-inquiry quote guidance and adjusted-price notice to enabled; an explicit `false` remains respected. |
| Existing product-bound external fulfillment rules already enable dynamic price sync | Migration `00052` sets both buyer-message switches to `true` once; local-card, account-wide, deleted, disabled-action, and non-dynamic rules remain unchanged. |
| Instance missing, rate limit, timeout, authentication, or supplier system failure | Log and skip the rule; never apply the `9999.00` product-review price. |
| Scheduler context is cancelled while pacing | Stop waiting and exit without starting another supplier quote. |

## 5. Examples

Good request body:

```json
{"goodsType":"1","skuType":"0","showDirId":"1","dirId":"12","keyword":"会员","page":2}
```

Compatible page response:

```json
{"items":[],"total":81,"page":2,"page_size":20,"total_pages":5}
```

Bad behavior: treating missing list stock as zero and hiding the product before its detail endpoint can provide stock.

Correct behavior: represent list stock as `-1`, allow selection, then require positive stock from the sequential detail check before publishing.

## 6. Background quote pacing

- The scan loop is sequential and must not add quote goroutines.
- The scheduler keeps at least 500 milliseconds between eligible background quote starts as supplemental protection across providers.
- Kasushou additionally permits only one in-flight request for the same station and merchant, and spaces actual HTTP request starts by at least 3.5 seconds. This conservative interval keeps the fourth request outside a rolling ten-second window.
- Kasushou catalog, detail, explicit quote, scheduled price sync, purchase, and order-query calls consume the same queue; the API key must not appear in the queue key or logs.
- A Kasushou card-product detail uses only `goods/info`; only direct-recharge products request `goods/attach`, avoiding an unnecessary second quota unit for card products.
- The first eligible quote starts immediately. Rules skipped before a supplier call do not consume a delay slot.
- Scheduler and Kasushou-client waits both use the caller context so shutdown or request cancellation interrupts the delay before HTTP I/O.
- The limiter coordinates one application process. Multiple replicas using the same supplier credentials require an external shared limiter or separate upstream quota.
- The missing-or-unavailable product fallback applies only to scheduled listing-price sync. Pending-order repricing and purchase flows keep returning the original error and must not use the `9999.00` value.

## 7. Verification requirements

- Provider tests use a local HTTP server and assert endpoint paths, signed payloads, request filters, recursive categories, pagination, and response normalization.
- Automation tests cover serial order, minimum quote-start spacing, continuation after one failure, context cancellation, and duplicate-item handling.
- Automation tests assert both a typed deleted-product error and a successful quote with `CanBuy=false` perform exactly one `999900`-cent item update and persist `9999.00`, while ordinary quote failures perform no update.
- Adapter and handler contract tests prove the provider route and optional `total_pages` field.
- Frontend behavior tests cover Kayixin instance selection, category/page parameters, sequential detail checks, invalid-detail rejection, and Kasushou regression.
- Run API generation/check, architecture checks, comment checks for every touched file, focused Go tests including race coverage, frontend type-check/tests/build, server build, and `git diff --check`.

## 8. Accepted-order recovery pacing

- A newly submitted supplier order that returns `waiting` or `processing` is not queried immediately. It is persisted as an external wait and becomes eligible for its first status query after five seconds.
- Recovery discovery runs in an independently owned five-second scheduler loop. The minute-level account/review scan must not be required for supplier-order progress.
- Retry delays after completed queries are 5 seconds, 10 seconds, 20 seconds, 30 seconds, 1 minute, 2 minutes, 3 minutes, 5 minutes, and then 10 minutes.
- Every recovery call queries the original persisted external order number. A temporary supplier `not found` response remains waiting and must never invoke `Buy` again, even with the same idempotency key.
- Database claim fencing remains authoritative when recovery and other scheduler work overlap. Cancellation stops the independent loop and its owner waits for the loop before shutdown completes.
- If a pending-price retry observes `FAIL_BIZ_BAD_REQUEST` with the confirmed reason `当前订单状态不支持改价`, the order has naturally left the repricing phase. Close the pending quote without failing automation; if the paid-order flow already replaced it with an adjusted snapshot, preserve that snapshot. Log the MTOP outcome and automation closure at `INFO`, while unrelated business failures remain errors.

## Scenario: buyer-message defaults for external dynamic pricing

### 1. Scope / Trigger

- Trigger: a product-bound `order_paid` rule has an enabled external `send_card` action whose `price_sync_enabled` or legacy `pending_price_enabled` value is true.

### 2. Signatures

- Rule JSON fields: `price_guidance_enabled: boolean` and `price_adjusted_notice_enabled: boolean`.
- Database upgrade: aligned SQLite, MySQL, and PostgreSQL migration `00052_external_price_message_defaults.sql`.

### 3. Contracts

- New manual/API and batch-publish rules write both fields as `true` when they are absent.
- An explicitly supplied `false` remains false for new rules and later edits.
- Migration `00052` intentionally overwrites both fields to `true` once for every eligible existing rule.

### 4. Validation & Error Matrix

- Missing product binding, non-`order_paid` trigger, local action, disabled action, or dynamic pricing disabled -> do not apply defaults.
- Invalid rule or action JSON in migration input -> do not broaden the target; valid eligible records continue to migrate.

### 5. Good/Base/Bad Cases

- Good: external product rule with price sync enabled receives both switches while unrelated JSON fields remain unchanged.
- Base: a new rule with `price_guidance_enabled:false` preserves that user choice and defaults only the missing adjusted-price switch.
- Bad: enabling buyer messages on account-wide or local-card rules would create a configuration rejected by application validation.

### 6. Tests Required

- Application tests assert creation defaults, explicit-false preservation, and batch-publish rule JSON.
- Frontend tests assert enabling external price sync updates the draft immediately.
- Migration tests assert eligible historical rules are enabled and ineligible rules remain byte-for-byte unchanged.

### 7. Wrong vs Correct

- Wrong: apply defaults during every startup or update, which would reopen switches that a user deliberately closed.
- Correct: default only absent fields during creation, and use one irreversible migration to satisfy the explicit existing-data rollout.

## Wrong and correct implementations

Wrong: use `Promise.all` for pre-publish detail calls or spawn one goroutine per background rule. Both can burst supplier APIs and trigger rate limits.

Correct: use an explicit sequential loop in the frontend, retain the scheduler's context-cancellable 500-millisecond supplemental pacing, and enforce Kasushou's shared serial 3.5-second request queue at the protocol-client boundary.

Wrong: infer Kayixin total pages using a locally assumed page size when the supplier returns `allPage`.

Correct: carry `allPage` through the domain, HTTP contract, generated schema, and feature adapter as optional `total_pages`, while retaining the old fallback only when the field is absent.
