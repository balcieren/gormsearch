# GormSearch

[![Go Reference](https://pkg.go.dev/badge/github.com/balcieren/gormsearch.svg)](https://pkg.go.dev/github.com/balcieren/gormsearch)
[![Go Report Card](https://goreportcard.com/badge/github.com/balcieren/gormsearch)](https://goreportcard.com/report/github.com/balcieren/gormsearch)

A minimalist Go library that provides seamless integration between GORM and Meilisearch. Automatically syncs GORM models to Meilisearch using struct tags.

## Features

- 🏷️ **Struct Tag Configuration** - Define searchable, filterable, sortable, primaryKey fields
- 🔄 **Auto Sync** - GORM hooks automatically sync Create/Update/Delete operations
- 🗑️ **Soft Delete Support** - Automatically removes from Meilisearch on soft delete
- ⚡ **Async Mode** - Non-blocking operations with worker pool
- 🔁 **Retry Logic** - Exponential backoff for failed operations with context-aware cancellation
- 🔍 **Facets & Highlighting** - Built-in support for faceted search
- 🎯 **Generics** - Type-safe search results with `SearchFor[T]()` and `Of[T]()`
- ⏱️ **Context Support** - Full context propagation for timeout and cancellation
- 🔒 **Safe by Default** - Input validation, panic recovery, limit enforcement

## Installation

```bash
go get github.com/balcieren/gormsearch
```

## Quick Start

```go
package main

import (
    "fmt"
    "log"

    "github.com/balcieren/gormsearch"
    "github.com/meilisearch/meilisearch-go"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
)

type Product struct {
    gorm.Model
    Name        string  `json:"name" meili:"searchable,filterable"`
    Description string  `json:"description" meili:"searchable"`
    Price       float64 `json:"price" meili:"filterable,sortable"`
    Category    string  `json:"category" meili:"filterable"`
    InternalSKU string  `json:"-" meili:"-"`
}

func (p Product) IndexName() string {
    return "products"
}

func main() {
    db, _ := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
    db.AutoMigrate(&Product{})

    meili := meilisearch.New("http://localhost:7700",
        meilisearch.WithAPIKey("masterKey"),
    )

    gs, err := gormsearch.New(db, meili,
        gormsearch.WithBatchSize(50),
        gormsearch.WithMaxWorkers(20),
        gormsearch.WithOnError(func(op string, err error) {
            log.Printf("meilisearch %s error: %v", op, err)
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    gs.Register(&Product{})

    db.Create(&Product{
        Name:     "MacBook Pro",
        Price:    2499.99,
        Category: "electronics",
    })

    // Search with Typed API
    results, err := gormsearch.SearchFor[Product](gs, "macbook",
        gormsearch.WithLimit(10),
        gormsearch.WithFilter("category = 'electronics'"),
        gormsearch.WithSort("price:asc"),
    )
    if err != nil {
        log.Fatal(err)
    }

    for _, product := range results.Hits {
        fmt.Printf("Found: %s ($%.2f)\n", product.Name, product.Price)
    }
}
```

## Struct Tag Reference

| Tag          | Description                                     |
| ------------ | ----------------------------------------------- |
| `searchable` | Field is indexed for full-text search           |
| `filterable` | Field can be filtered (`filter: "price > 100"`) |
| `sortable`   | Field can be sorted (`sort: ["price:asc"]`)     |
| `primaryKey` | Field is the primary key for Meilisearch index  |
| `-`          | Field is excluded from Meilisearch              |

```go
type Article struct {
    gorm.Model
    Title   string `meili:"searchable,filterable"`
    Content string `meili:"searchable"`
    Author  string `meili:"filterable"`
    Views   int    `meili:"sortable"`
    Draft   bool   `meili:"-"`
}
```

## Model Interface

Models can optionally implement the `Indexable` interface to customize the Meilisearch index name:

```go
// Indexable is an optional interface for custom index names.
type Indexable interface {
    IndexName() string
}
```

If not implemented, the table name (from GORM's `Tabler` interface or struct name) is used as the index name.

## Typed Search

Get typed results directly using Go generics.

### Single Search

```go
// Auto-detect index name from type
results, err := gormsearch.SearchFor[Product](gs, "macbook")
if err != nil {
    log.Fatal(err)
}

// With options
results, err = gormsearch.SearchFor[Product](gs, "macbook",
    gormsearch.WithIndexName("custom_products_index"), // Optional: Custom index name
    gormsearch.WithLimit(10),
    gormsearch.WithFilter("price < 1000"),
)

// Access typed results
for _, p := range results.Hits {
    fmt.Printf("Found: %s ($%.2f)\n", p.Name, p.Price)
}
```


### Fluent API

You can also use the `Of[T]` helper for a more fluent style:

```go
// Create a typed searcher
products := gormsearch.Of[Product](gs)

// Search
results, err := products.Search("macbook")
if err != nil {
    log.Fatal(err)
}

// With context
results, err = products.WithContext(ctx).Search("macbook",
    gormsearch.WithLimit(10),
)

// Override index name
results, err = products.Index("archived_products").Search("macbook")

// Chain everything
results, err = gormsearch.Of[Product](gs).
    WithContext(ctx).
    Index("v2_products").
    Search("macbook", gormsearch.WithLimit(10))
```

### Raw Index Search

Use `gs.Index()` for raw search when you don't need typed results:

```go
// Raw search (returns map[string]any)
results, err := gs.Index("products").Search("macbook")
for _, hit := range results.Hits {
    name := hit["name"].(string) // manual type assertion
}

// With context
results, err = gs.Index("products").WithContext(ctx).Search("macbook")

// With options
results, err = gs.Index("products").Search("macbook",
    gormsearch.WithLimit(10),
    gormsearch.WithFilter("price < 1000"),
)
```

### Multi-Type MultiSearch

Perform searches across multiple indexes with different types in a single HTTP request.

```go
var products []Product
var categories []Category

// Single HTTP request, multiple types - results decode into destination slices
metadata, err := gs.MultiSearch(
    gormsearch.Query(&products, "macbook").As("products"),
    gormsearch.Query(&categories, "electronics").As("categories"),
)

if err == nil {
    // Access decoded results directly
    for _, p := range products {
        fmt.Printf("Product: %s\n", p.Name)
    }

    // metadata contains EstimatedTotal, ProcessingTime, etc. for each query
    // Use ByKey map for named access
    fmt.Printf("Total products: %d\n", metadata.ByKey["products"].EstimatedTotal)
    fmt.Printf("Total categories: %d\n", metadata.ByKey["categories"].EstimatedTotal)

    // Or access via index (order preserved)
    // fmt.Printf("Total products: %d\n", metadata.Results[0].EstimatedTotal)
}
```

## Raw Search

If you need `map[string]any` results or dynamic index names.

### New / MustNew

```go
// Returns error if db or client is nil
gs, err := gormsearch.New(db, meiliClient,
    gormsearch.WithBatchSize(100),
    gormsearch.WithAsync(true),
    gormsearch.WithMaxWorkers(10),
    gormsearch.WithOnError(func(op string, err error) {
        log.Printf("error: %v", err)
    }),
)
// Or panic on error
gs := gormsearch.MustNew(db, meiliClient)
```

### Register

```go
gs.Register(&Product{})
```

### Search (Raw)

```go
results, err := gs.Search("products", "query",
    gormsearch.WithLimit(20),    // Max: 1000
    gormsearch.WithOffset(0),
    gormsearch.WithFilter("category = 'electronics' AND price < 1000"),
    gormsearch.WithSort("price:asc", "created_at:desc"),
)
```

### MultiSearch (Raw)

Search across multiple indexes in a single request returning raw maps:

```go
results, err := gs.MultiSearchRaw(
    gormsearch.SearchQuery{
        IndexName: "products",
        Query:     "macbook",
        Limit:     10,
        Filter:    "category = 'electronics'",
    },
    gormsearch.SearchQuery{
        IndexName: "articles",
        Query:     "macbook",
        Limit:     5,
    },
)

// Access individual results
for i, result := range results.Results {
    fmt.Printf("Index %d: %d hits\n", i, len(result.Hits))
}
```

### Context Support

All methods have context-aware versions for timeout and cancellation:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

// Context-aware search
results, err := gs.WithContext(ctx).Search("products", "query")

// Context-aware multi-search
results, err := gs.WithContext(ctx).MultiSearchRaw(queries...)

// Context-aware sync
err := gs.WithContext(ctx).Sync(&Product{})
```

### Sync

```go
gs.Sync(&Product{})
```

### What gets synced automatically

| Statement | Synced |
| --- | --- |
| `db.Create(&product)` | ✅ |
| `db.Create(&[]Product{...})` | ✅ one job per record |
| `db.Save(&product)` / `db.Model(&product).Update(...)` | ✅ full row is reloaded and re-indexed |
| `db.Delete(&product)` | ✅ |
| `db.Delete(&Product{}, id)` | ✅ |
| `db.Where("price > ?", 100).Delete(&Product{})` | ✅ affected keys are resolved before the delete |
| `db.Model(&Product{}).Where(...).Update(...)` | ❌ no primary key to work from |

The last row is the one exception: a conditional update names no record, so
there is nothing to re-index. It is reported through `WithOnError` as
`ErrUnresolvedPrimaryKey`; load the rows and save them, or call `Sync`, if you
need those changes indexed.

## Performance

GormSearch is built for high speed and efficient memory usage:

- **Zero-Allocation**: reflection hot paths are cached.
- **Direct Decoding**: typed search decodes Meilisearch's response bytes straight into your struct, with no intermediate `map[string]any` (~1.7x faster and ~11x fewer allocations than decoding via maps).
- **Buffer Reuse**: syncing reuses memory buffers to minimize GC pressure.
- **O(1) Lookups**: internal registries use optimized maps for instant access, and write callbacks cost the same whether you register 1 model or 50.
- **Async**: all updates are non-blocking by default, with a bounded worker pool that applies backpressure under heavy load (configurable via `WithMaxWorkers` and `WithAsync`).

## Advanced Features

### 1. Index Settings Sync

Implement `SettingProvider` interface to configure advanced settings like synonyms, stop words, and ranking rules.

```go
// SettingProvider interface definition
type SettingProvider interface {
    MeiliSettings() *meilisearch.Settings
}

// Example implementation
func (p Product) MeiliSettings() *meilisearch.Settings {
    return &meilisearch.Settings{
        Synonyms: map[string][]string{
            "phone": {"iphone", "galaxy"},
        },
        StopWords: []string{"the", "a", "an"},
        RankingRules: []string{
            "words",
            "typo",
            "proximity",
            "attribute",
            "sort",
            "exactness",
        },
    }
}
```

### 2. Geo-Search Support

Tag your struct field with `meili:"geo"`. GormSearch handles the extraction of coordinates.
Supported fields: `Lat`, `Latitude`, `Lng`, `Lon`, `Longitude`.

```go
type Place struct {
    gorm.Model
    Name     string `meili:"searchable"`
    Location Location `meili:"geo"`
}

type Location struct {
    Lat float64
    Lng float64
}

// Search with GeoRadius
builder := gormsearch.NewFilter().GeoRadius(48.8566, 2.3522, 1000)
results, _ := gormsearch.SearchFor[Place](gs, "coffee",
    gormsearch.WithFilter(builder.Build()),
)
```

### 3. Complex Filter Builder

Use the fluent `FilterBuilder` to construct type-safe filters.

```go
f := gormsearch.NewFilter()
filter := f.Where("category").Eq("electronics").
    And().
    Group(func(sub *gormsearch.FilterBuilder) {
        sub.Where("price").Lt(1000).
            Or().
            Where("on_sale").Eq(true)
    }).
    Build()

// Result: category = 'electronics' AND (price < 1000 OR on_sale = true)
```

### 4. Multi-Tenancy

Use `WithIndexPrefix` to isolate indexes for different tenants or environments.

```go
// Prefix all indexes with "tenant_1_"
gs, _ := gormsearch.New(db, meili,
    gormsearch.WithIndexPrefix("tenant_1_"),
)

gs.Register(&Product{}) // Index Name: "tenant_1_products"
```

### 5. Bulk Re-Indexer

The `Sync` method is optimized for bulk operations. It uses `FindInBatches` internally to re-index large tables efficiently without memory issues.

```go
// Efficiently re-index millions of records
err := gs.Sync(&Product{})
```

### 6. Retry Logic

Configure automatic retries with exponential backoff for failed Meilisearch operations:

```go
gs, _ := gormsearch.New(db, meili,
    gormsearch.WithMaxRetries(3), // Retry up to 3 times on failure
    gormsearch.WithOnError(func(op string, err error) {
        log.Printf("operation %s failed: %v", op, err)
    }),
)
```

## Custom Serialization

You can use custom encoder/decoder functions to integrate external serialization libraries like `sonic`, `msgpack`, or `goccy/go-json` for better performance.

### Custom Map Encoder (Legacy Style)

```go
gs, err := gormsearch.New(db, meili,
    gormsearch.WithMapEncoder(func(model any) (map[string]any, error) {
        // Use custom logic to convert model to map
        // e.g., using sonic
        data, err := sonic.Marshal(model)
        if err != nil {
            return nil, err
        }
        var doc map[string]any
        err = sonic.Unmarshal(data, &doc)
        return doc, err
    }),
)
```

### Custom Map Decoder (Legacy Style)

```go
gs, err := gormsearch.New(db, meili,
    gormsearch.WithMapDecoder(func(hits []map[string]any, dest any) error {
        // Use custom logic to decode hits into dest
        data, err := sonic.Marshal(hits)
        if err != nil {
            return err
        }
        return sonic.Unmarshal(data, dest)
    }),
)
```

## High Performance JSON

You can easily integrate high-performance JSON libraries like [`sonic`](https://github.com/bytedance/sonic) or [`go-json`](https://github.com/goccy/go-json) using the `WithJSONEncoder` and `WithJSONDecoder` options. This allows you to bypass the standard `encoding/json` library and map conversions for maximum speed.

```go
import "github.com/bytedance/sonic"

gs, err := gormsearch.New(db, meili,
    // Use sonic for encoding/decoding
    gormsearch.WithJSONEncoder(sonic.Marshal),
    gormsearch.WithJSONDecoder(sonic.Unmarshal),
)
```

`WithJSONEncoder` passes `json.RawMessage` straight to Meilisearch-go when syncing; `WithJSONDecoder` is used to decode search hits back into your structs.

> Note: `WithMapDecoder` opts out of this path. Hits handed to a `MapDecoder`
> have already passed through `map[string]any`, where every JSON number becomes
> a `float64` — so integers beyond 2^53 (Snowflake IDs and the like) lose
> precision. The default decoder reads the raw bytes and keeps them exact.

## Async Workers & Queues

Offload synchronization to external queues like Redis, NATS, or Kafka to ensure your application stays fast.

### 1. Easy Integration (Recommended)

Use `WithQueue` to integrate any queue system in a single line. GormSearch handles the serialization (JSON) automatically.

#### Redis Example

```go
// Producer
gs, _ := gormsearch.New(db, meili,
    gormsearch.WithQueue(func(ctx context.Context, data []byte) error {
        return rdb.RPush(ctx, "meili_queue", data).Err()
    }),
)
```

#### NATS Example

```go
// Producer
gs, _ := gormsearch.New(db, meili,
    gormsearch.WithQueue(func(ctx context.Context, data []byte) error {
        return nc.Publish("meili.sync", data)
    }),
)
```

#### Consumer (Worker)

In your worker service, simply pass the received data to `Consume`:

```go
// Can be used with any queue (Redis, NATS, Kafka, etc.)
err := gormsearch.Consume(meiliClient, payload)

// Or with context support for timeout/cancellation
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

var job gormsearch.Job
json.Unmarshal(payload, &job)
err := gormsearch.ExecuteWithContext(ctx, meiliClient, job)
```

### 2. Advanced Customization

If you need full control over the job creation logic (e.g., adding custom metadata, changing operation types), use the options ending in `Func`.

#### Custom Dispatcher

Use `WithDispatcherFunc` to bypass default serialization and handle the raw job struct directly.

```go
gs, _ := gormsearch.New(db, meili,
    gormsearch.WithDispatcherFunc(func(ctx context.Context, job gormsearch.Job) error {
        // Add custom headers or transformation logic
        return myQueue.Publish(job.IndexName, job)
    }),
)
```

### 3. Interface Implementation (Legacy)

You can also implement the `Dispatcher` interface for complex stateful dispatchers.

```go
type Dispatcher interface {
    Dispatch(ctx context.Context, job Job) error
}

gs, _ := gormsearch.New(db, meili,
    gormsearch.WithDispatcher(&MyDispatcher{}),
)
```

## Errors

```go
gormsearch.ErrNilDB              // Database connection is nil
gormsearch.ErrNilClient          // Meilisearch client is nil
gormsearch.ErrNilModel           // Model is nil
gormsearch.ErrInvalidModel       // Model is not a struct
gormsearch.ErrIndexNotRegistered // Index not registered
gormsearch.ErrQueryTooLong       // Query exceeds 1000 characters
gormsearch.ErrNoQueries          // MultiSearch called without queries
```

These are reported through `WithOnError` rather than returned, because the
write they relate to has already succeeded:

```go
gormsearch.ErrUnresolvedPrimaryKey // Write targeted rows by condition; nothing to sync
gormsearch.ErrDeleteFanoutTooLarge // Conditional delete matched more rows than can be tracked
```

## License

MIT
