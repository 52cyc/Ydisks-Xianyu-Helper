# External Fulfillment Catalog and Price Sync

## 1. Scope and trigger

This contract applies when a fulfillment provider exposes catalog browsing for the batch-listing flow, and when the background automation scheduler refreshes the price of an already-linked external product.

The current catalog-capable providers are `kasushou_v2` and `kayixin_v3`. One supplier SKU maps to one Xianyu item. The Kayixin catalog integration accepts only card products with a single supplier specification.

## 2. Interfaces and ownership

- Provider clients implement the consumer-owned `fulfillment.CatalogGateway` interface through `ListCategories` and `ListProductPage`.
- `fulfillment.ProductPage.TotalPages` carries the supplier's page count when available. It is optional at the HTTP boundary so older providers and callers remain compatible.
- The multi-provider adapter selects a provider by instance configuration. HTTP handlers do not contain supplier protocol rules.
- The frontend uses the shared fulfillment API adapter and never calls supplier endpoints directly.
- The automation scheduler owns background quote pacing. Provider clients remain safe for explicit user queries and purchase flows without an unconditional delay.
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
| Instance missing, rate limit, timeout, authentication, or supplier system failure | Log and skip the rule; never apply the `9999.00` deleted-product price. |
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
- Starts of actual supplier quote calls are spaced by at least 500 milliseconds in production.
- The first eligible quote starts immediately. Rules skipped before a supplier call do not consume a delay slot.
- Waiting uses the scheduler context so shutdown interrupts the delay.
- The pacing hook is limited to scheduled external-listing price scans; explicit quote, purchase, and order-query paths are unaffected.
- The deleted-product fallback applies only to scheduled listing-price sync. Pending-order repricing and purchase flows keep returning the original error and must not use the `9999.00` value.

## 7. Verification requirements

- Provider tests use a local HTTP server and assert endpoint paths, signed payloads, request filters, recursive categories, pagination, and response normalization.
- Automation tests cover serial order, minimum quote-start spacing, continuation after one failure, context cancellation, and duplicate-item handling.
- Automation tests also assert a typed deleted-product error performs exactly one `999900`-cent item update and persists `9999.00`, while ordinary quote failures perform no update.
- Adapter and handler contract tests prove the provider route and optional `total_pages` field.
- Frontend behavior tests cover Kayixin instance selection, category/page parameters, sequential detail checks, invalid-detail rejection, and Kasushou regression.
- Run API generation/check, architecture checks, comment checks for every touched file, focused Go tests including race coverage, frontend type-check/tests/build, server build, and `git diff --check`.

## Wrong and correct implementations

Wrong: use `Promise.all` for pre-publish detail calls or spawn one goroutine per background rule. Both can burst supplier APIs and trigger rate limits.

Correct: use an explicit sequential loop in the frontend and a context-cancellable 500-millisecond start interval in the scheduler.

Wrong: infer Kayixin total pages using a locally assumed page size when the supplier returns `allPage`.

Correct: carry `allPage` through the domain, HTTP contract, generated schema, and feature adapter as optional `total_pages`, while retaining the old fallback only when the field is absent.
