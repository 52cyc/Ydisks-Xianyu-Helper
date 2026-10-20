# Keyword matching

## Scope

`internal/engine/reply.go` resolves keyword replies after API replies and before AI/default replies. Matching is local and does not request platform data.

## Signatures

`ReplyService.keywordMatches(kw db.Keyword, text string) bool` matches stored `Expressions` using `MatchType`. `keywordMatches(messageLower, rawKeyword string) bool` retains the fork's contains-mode separators. `keywordmatch.CompileExpression` centralizes Go/RE2 compilation for validation and execution.

## Contract

Expressions have OR semantics. In `contains` mode, split each expression on `|`, ASCII comma, Chinese comma, ideographic comma, LF and CR; trim and match nonempty terms as case-insensitive substrings. In `regexp` mode, pass complete expressions to Go/RE2 without splitting punctuation; default matching ignores case, with explicit inline flags supported. Spaces inside a contains term do not split it. Item-scoped rules retain priority over account-scoped rules.

## Validation

Empty contains terms never match. Invalid legacy regex expressions are logged by index/type without expression contents and skipped; unknown modes fail closed. Migration 59 adds expression storage and contains-mode defaults without deleting old keyword text. The API, UI and matcher must agree; regex punctuation must never pass through the legacy splitter.

## Cases

- Base: `在吗` matches `老板在吗`.
- Alternatives: `在吗 | 价格` matches either `在吗` or `价格多少`.
- Bad: `| , ， 、` must not match arbitrary messages.

## Tests

Keep `TestKeywordReply_AnyKeywordTermMatches`, `TestKeywordMatchesIgnoresEmptyTerms`, and existing item-priority tests. Verify each supported separator and the separator-only negative case using local SQLite fixtures.

## Wrong vs correct

Wrong: compare the entire stored `在吗 | 价格` string against the message. Correct: split once through the shared matching helper and apply OR matching without changing rule priority.
