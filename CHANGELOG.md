# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v1.2.0] - 2026-09-18

### 🚨 Critical Bug Fixes

- **`db.Create()` inserted the row twice** — the post-write hook reloaded the record with `db.Session(&gorm.Session{})`, which inherits the in-flight statement *including its already-built SQL*. The "reload" therefore re-executed the `INSERT`. Every create wrote a duplicate row and indexed a document built from an empty struct. The reload now uses a fresh statement (`NewDB: true`) that still shares the connection pool, so it stays inside the caller's transaction.
- **Updates were never indexed** — the same bug replayed the `UPDATE` instead of selecting, so the reload reported `record not found` and the job was dropped. Updates now re-index the full reloaded row, including columns the statement did not write (`Select("name")` no longer truncates the document).
- **String and UUID primary keys were broken** — the key was stringified before being passed to `First(dest, id)`, and GORM interprets a non-numeric string condition as raw SQL. Conditions are now built from the key's raw value via `clause.PrimaryColumn`.
- **`db.Delete(&Product{}, id)` deleted the wrong document** — the destination struct is left zeroed by GORM, so the hook removed document `"0"` and left the real one in the index. Affected keys are now resolved from the table before the rows are deleted, which also makes conditional deletes (`db.Where(...).Delete(...)`) sync correctly.
- **Batch writes were silently skipped** — `db.Create(&[]Product{...})` hands GORM a `*[]Product`, whose type name never matched the registered model. Batch writes now fan out to one job per record.

### 🐛 Bug Fixes

- **Large integers were corrupted in typed search results** — hits were routed through `map[string]any`, turning every JSON number into a `float64` and rounding IDs beyond 2^53. Typed decoding now reads the raw response bytes.
- **`WithJSONDecoder` was never used** — the option was stored and ignored. It now drives typed hit decoding.
- **Index settings were silently dropped** — `hasSettings` enumerated ten fields by hand, so a model providing only `DisplayedAttributes`, `Embedders`, `SearchCutoffMs`, `Dictionary`, … never had its settings pushed. It now compares against the zero value.
- **`MeiliSettings()` values were mutated** — tag-derived attributes were merged into the pointer the provider returned, leaking one model's configuration into every other model sharing a package-level settings value.
- **Filter values were escaped incorrectly** — backslashes were not escaped, so `a\b` produced a dangling escape; floats were rendered with `%v`, emitting exponent form (`1e+07`) that Meilisearch rejects in filters.
- **`Not()` bound to only the first condition** — `Not(a = 1 AND b = 2)` produced `NOT a = 1 AND b = 2`. The sub-filter is now parenthesized, and repeated `And()`/`Or()` calls no longer emit dangling operators.
- **`MultiSearch()` returned `(nil, nil)`** when called with no queries, handing the caller a nil pointer to dereference. It returns `ErrNoQueries`, matching `MultiSearchRaw`.
- **Models with the same name in different packages collided** — the registry was keyed by bare type name, so `billing.Invoice` and `crm.Invoice` overwrote each other and synced to the wrong index. Keys are now package-qualified.
- **`Register()` accepted non-struct values** and produced an empty, useless config; it now returns `ErrInvalidModel`.
- **`meili:"-"` / `json:"-"` on an embedded struct was ignored** — embedded types were walked into before their own tags were read.
- Reflection helpers (`extractID`, `isSoftDeleted`, `toDocument`) panicked on nil pointers and slices instead of returning zero values.
- `QueueDispatcher` panicked on a nil encoder and now defaults to `json.Marshal`; a missing publisher returns `ErrNoPublisher`.

### ⚡ Performance

- **Typed search decodes ~1.7x faster with ~11x fewer allocations** (41 vs 461 allocs/op for 20 hits) by decoding from the response bytes instead of building an intermediate `[]map[string]any` and re-marshaling complex fields.
- **Write callbacks are now O(1) in the number of registered models.** They were registered once per model, so every insert ran a type check for each registered model; a single set of callbacks now resolves the config from the registry.
- **Retries no longer sleep after the final attempt** — an exhausted retry chain wasted a full backoff interval (400ms at the default of 3) before returning.
- `valToString` switches on `reflect.Kind`, keeping named key types (`type UserID uint64`) off the `fmt.Sprintf` path.

### ⚠️ Behavior Changes

- A write that identifies rows only by condition (`db.Model(&Product{}).Where(...).Update(...)`) reports `ErrUnresolvedPrimaryKey` through `WithOnError` instead of silently doing nothing. It previously could not be synced either; it is now visible.
- Conditional deletes issue one extra `SELECT` of the affected primary keys, capped at 10,000 rows (`ErrDeleteFanoutTooLarge` is reported past that). Deletes of a loaded struct are unaffected.
- `FilterBuilder.GeoRadius` emits full precision (`45.5`) instead of six fixed decimals (`45.500000`).
- `MultiSearchResult.ByKey` is nil rather than an empty map when no query set a key. Reads are unaffected.

### 🧹 Housekeeping

- Minimum Go version raised to **1.27**.
- `SearchQuery.IgnoreFields` marked deprecated — it was never read.
- Dead and misleading documentation cleaned up: `WithEncoder`/`WithDecoder` doc blocks were attached to the wrong functions, and `IndexRef.MultiSearch` claimed to scope queries to its own index.

### 🧪 Testing

- 30 new regression tests (`regression_test.go`) covering every fix above, plus benchmarks comparing the raw and map-based decode paths.

**Full Changelog**: https://github.com/balcieren/gormsearch/compare/v1.1.0...v1.2.0

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
