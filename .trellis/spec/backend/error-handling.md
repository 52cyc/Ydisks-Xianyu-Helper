# Batch Item Publish Failure Boundaries

## 1. Scope / Trigger

This contract applies after the final Xianyu item-publish request has started. It prevents both duplicate remote items and batches that continue publishing after account risk control is triggered.

## 2. Signatures

- `mtop.IsDefinitePublishRejection(error) bool`
- `adapter.ShouldStopBatchAfterPublishFailure(error) bool`
- `items.BatchRunOptions.ShouldStopAfterFailure func(error) bool`
- `items.UncertainRemotePublishError`

No HTTP API or database schema is changed by this contract.

## 3. Contracts

- A typed `MTopErrorBusiness`, a typed `MTopErrorRiskVerification`, or a legacy `PublishError` containing `FAIL_BIZ_*` is a definite remote rejection.
- Transport, timeout, cancellation, decode, unknown system, and failed remote-result checkpoint paths remain uncertain unless a typed platform result proves otherwise.
- A definite rejection is persisted with the normal `publish` failure kind and is eligible for user-triggered retry.
- `UncertainRemotePublishError` is persisted as `uncertain_remote` and must not be reset automatically.
- A risk-verification failure is persisted for the current row before the runner stops. Remaining rows are finalized as `interrupted`, so the user can retry them after completing verification.
- The runner must not start slider CAPTCHA or silently retry a rejected item.

## 4. Validation & Error Matrix

| Condition | Row classification | Continue batch | Retry behavior |
|---|---|---:|---|
| `FAIL_BIZ_*` | `publish` | yes | user-triggered |
| `FAIL_SYS_USER_VALIDATE` / risk type | `publish` | no | user-triggered after verification |
| session expired | existing session failure | no | after account recovery |
| network/timeout/decode after remote start | `uncertain_remote` | yes unless context stops | manual remote-state check first |
| remote success but local checkpoint failed | `uncertain_remote` | no replay | manual remote-state check first |

## 5. Good / Base / Bad Cases

- Good: a title rule rejection returns its original platform message, remains retryable, and does not claim that the remote result is unknown.
- Base: an ordinary definitive business rejection fails only its row and the batch continues.
- Bad: wrapping every post-request error in `UncertainRemotePublishError`; this blocks safe correction and hides platform semantics.
- Bad: continuing with the next row after risk verification; this increases account-control pressure.

## 6. Tests Required

- MTOP unit tests assert business and risk errors are definite, while system and transport errors are not.
- Adapter tests assert definite errors are returned directly and transport errors are wrapped as uncertain.
- Runner tests assert the current row is marked failed before stop, only one publish call occurs, and interrupted finalization runs.
- Database tests must continue proving `validation` and `uncertain_remote` rows are excluded from reset while `publish` and `interrupted` rows are retryable.

## 7. Wrong vs Correct

### Wrong

```go
return &items.UncertainRemotePublishError{Err: err}
```

### Correct

```go
if mtop.IsDefinitePublishRejection(err) {
    return err
}
return &items.UncertainRemotePublishError{Err: err}
```
