# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v1.1.0] - 2026-08-04

### 🐛 Bug Fixes

- **Geo decode data corruption** — geo fields (`meili:"geo"`) were decoded using indices relative to the root struct instead of the geo sub-struct, silently dropping or corrupting data
- **Silent data loss in typed search results** — the fast decoder skipped slices, maps, pointers, and nested structs entirely (e.g. `Tags []string` always came back empty); they now decode correctly with a JSON fallback
- **Parser panics** — `Register()` no longer panics on models embedding `*gorm.Model` (pointer embeds) or non-struct embedded types; nil embedded pointers are also handled safely at runtime
- **`MultiSearch` ignored `WithIndexPrefix`** — `Query(...)`/`QueryIndex(...)` queries failed with `ErrIndexNotRegistered` when an index prefix was configured
- **`indexNameFor` panicked for pointer types** (`T = *Product`) and missed pointer-receiver `TableName()`/`IndexName()` methods
- **`Searcher.Search` could mutate the caller's options slice** backing array
- **`WithAsync(false)` was a no-op** — the flag was stored but never used

### ⚡ Performance & Reliability

- **Bounded dispatcher concurrency** — worker slots are now acquired *before* spawning goroutines, so in-flight jobs are capped at `WithMaxWorkers`. Under sustained bulk writes, `Dispatch` applies natural backpressure instead of piling up unbounded goroutines (prevents memory blow-ups)
- **Synchronous mode actually works** — `WithAsync(false)` executes jobs inline with zero goroutine overhead and returns execution errors to the caller
- Retry backoff now uses stoppable timers (no leaked `time.After` timers on context cancellation)

### ⚠️ Behavior Change

- Async dispatch can now **block briefly when all workers are busy** (backpressure). If you need strictly non-blocking behavior under heavy load, increase `WithMaxWorkers` or use `WithQueue` with an external queue (Redis/NATS/Kafka)

### 🧪 Testing & Housekeeping

- 11 new regression tests covering all fixes above (`bugfix_test.go`)
- Added `.gitignore`; removed dead code in `Sync`; README updated

**Full Changelog**: https://github.com/balcieren/gormsearch/compare/v1.0.1...v1.1.0
