# Changelog

All notable changes to danmuji are documented in this file.

## [Unreleased]

- No unreleased changes yet.

## [v0.3.1] - 2026-03-23

### Changed

- Danmuji test blocks now run in parallel by default.
- Generated `each/do`, `matrix`, and `each row` subtests now follow the enclosing test's parallel policy instead of hardcoding parallel execution.
- `@parallel` remains accepted for readability, but is now redundant in normal tests.

### Added

- `@serial` as an explicit opt-out for tests that must stay sequential.

### Fixed

- `process`-backed tests stay sequential automatically instead of inheriting the new default parallel behavior.
- Added and backfilled a checked-in `CHANGELOG.md` so release history lives in the repo as well as GitHub releases.

## [v0.3.0] - 2026-03-23

### Added

- `factory` declarations with `defaults` and `trait` support for reusable test data setup.
- `build` expressions with trait composition and field overrides.
- Rich assertions: `matches`, `unordered_equal`, `expect err is ...`, and `expect err message contains ...`.
- `await <-ch within ... as name` for typed channel receives with timeout.
- `needs tempdir` and `needs http` fixtures.
- Built-in `danmujiWS` helper set for WebSocket dial/read/write/close testing.
- Built-in `danmujiGRPC` helper set for bufconn-backed gRPC tests.

### Changed

- Lifted `needs` fixture bindings to the test-function scope so values like `dbEndpoint` remain available across nested blocks.
- Added postgres fixture env support for common config like password, database, and user.
- Improved matrix scoping so dimension aliases are usable inside nested `given` / `when` / `then` blocks.
- Reduced unused import injection in generated code.
- Expanded README coverage for factories, rich assertions, await, fixtures, WebSocket helpers, and gRPC helpers.

### Fixed

- Struct literal `Field: value` parsing inside danmuji blocks no longer collides with danmuji scenario-field parsing in the tested regression paths.
- Snapshot and highlight metadata refreshed for the expanded grammar.

## [v0.2.0] - 2026-03-23

### Added

- `fuzz` blocks that transpile to native Go fuzz targets.
- `danmujiHTTP` convenience helpers around `net/http/httptest`.

### Changed

- Allowed `exec`, `profile`, and `args` to remain usable as ordinary Go identifiers inside tests.
- Improved parse errors to point at the intended DSL construct and include enclosing test context.

### Included

- The numeric literal assertion fix from `v0.1.1` on `main`.

## [v0.1.1] - 2026-03-23

### Fixed

- Numeric literal assertions now handle cross-type numeric comparisons correctly.
- Mixed numeric cases like `expect value == 1000` no longer fail just because the literal defaults to Go `int` while the expression is `int64`, `uint32`, `uint8`, or similar.

## [v0.1.0] - 2026-03-19

### Added

- Initial public danmuji release with a `.dmj` BDD-style DSL that transpiles to normal `go test` code.
- Recursive CLI `build` / `test` support with source-mapped errors back to `.dmj` lines.
- Self-hosted meta specs covering the core DSL end to end.
- Runtime coverage for `process`, `process run`, `load`, and Docker-gated `needs`.
- Highlight-query coverage for danmuji-specific syntax.
- CI workflows for fast verification plus scheduled/manual race coverage.

### Included Surface

- Test categories: `unit`, `integration`, `e2e`.
- BDD blocks: `given`, `when`, `then`.
- Assertions: equality, inequality, relational operators, `contains`, `is_nil`, `not_nil`, `reject`.
- Doubles: `mock`, `fake`, `spy`, `verify ... called`, `not_called`, and `called with (...)`.
- Data-driven specs: `table`, `each row`, `each/do`, `matrix`, `property`.
- Timing and harness features: `eventually`, `consistently`, `fake_clock`, `process`, `stop`, `exec`, `snapshot`, `no_leaks`, `profile`, `load`, and `needs`.
