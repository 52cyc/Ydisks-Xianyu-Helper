# Keyword matching

## Scope

`internal/engine/reply.go` resolves keyword replies after API replies and before AI/default replies. Matching is local and does not request platform data.

## Signatures

`keywordMatches(messageLower, rawKeyword string) bool` accepts an already lowercased message and the stored rule string. `isKeywordTermSeparator(separator rune) bool` defines its separators.

## Contract

Split on `|`, ASCII comma, Chinese comma, ideographic comma, LF and CR. Trim each term and match any nonempty term as a case-insensitive substring. Spaces inside a term do not split it. Item-scoped rules retain priority over account-scoped rules; item scope and keyword alternatives are independent.

## Validation

Empty terms and separator-only rules never match. Existing rule text is not migrated: punctuation in old rules acquires separator semantics. The OpenAPI keyword descriptions and rule editor hints must agree with this contract.

## Cases

- Base: `在吗` matches `老板在吗`.
- Alternatives: `在吗 | 价格` matches either `在吗` or `价格多少`.
- Bad: `| , ， 、` must not match arbitrary messages.

## Tests

Keep `TestKeywordReply_AnyKeywordTermMatches`, `TestKeywordMatchesIgnoresEmptyTerms`, and existing item-priority tests. Verify each supported separator and the separator-only negative case using local SQLite fixtures.

## Wrong vs correct

Wrong: compare the entire stored `在吗 | 价格` string against the message. Correct: split once through the shared matching helper and apply OR matching without changing rule priority.
