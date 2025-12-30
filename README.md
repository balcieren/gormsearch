# GormSearch

[![Go Reference](https://pkg.go.dev/badge/github.com/balcieren/gormsearch.svg)](https://pkg.go.dev/github.com/balcieren/gormsearch)
[![Go Report Card](https://goreportcard.com/badge/github.com/balcieren/gormsearch)](https://goreportcard.com/report/github.com/balcieren/gormsearch)

A minimalist Go library that provides seamless integration between GORM and Meilisearch. Automatically syncs GORM models to Meilisearch using struct tags.

## Features

- 🏷️ **Struct Tag Configuration** - Define searchable, filterable, sortable, primaryKey fields
- 🔄 **Auto Sync** - GORM hooks automatically sync Create/Update/Delete operations
- 🗑️ **Soft Delete Support** - Automatically removes from Meilisearch on soft delete
- ⚡ **Async Mode** - Non-blocking operations with worker pool
- 🔁 **Retry Logic** - Exponential backoff for failed operations
- 🔍 **Facets & Highlighting** - Built-in support for faceted search
- 🎯 **Generics** - Type-safe search results with `Of[T]()` and `SearchAs[T]()`
- ⏱️ **Context Support** - Timeout and cancellation via context
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
    results, _ := gormsearch.SearchFor[Product](gs, "macbook",
        gormsearch.WithLimit(10),
        gormsearch.WithFilter("category = 'electronics'"),
        gormsearch.WithSort("price:asc"),
    )

    for _, product := range results.Hits {
        fmt.Printf("Found: %s ($%.2f)\n", product.Name, product.Price)
    }
}
```

## Struct Tag Reference

| Tag | Description |
|-----|-------------|
| `searchable` | Field is indexed for full-text search |
| `filterable` | Field can be filtered (`filter: "price > 100"`) |
| `sortable` | Field can be sorted (`sort: ["price:asc"]`) |
| `primaryKey` | Field is the primary key for Meilisearch index |
| `-` | Field is excluded from Meilisearch |

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

## Typed Search

Get typed results directly using Go generics.

### Single Search

```go
// Auto-detect index name from type
results, _ := gormsearch.SearchFor[Product](gs, "macbook")

// Or with fluent API
products := gormsearch.Of[Product](gs)
results, _ := products.Search("macbook")
results, _ := products.WithContext(ctx).Search("macbook")

// Access typed results
for _, p := range results.Hits {
    fmt.Printf("Found: %s ($%.2f)\n", p.Name, p.Price)
}
```

### Multi-Type MultiSearch

Perform searches across multiple indexes with different types in a single HTTP request.

```go
var products []Product
var categories []Category
var users []User

// Single HTTP request, multiple types
results, err := gormsearch.MultiSearch(gs,
    gormsearch.Query(&products, "macbook"),
    gormsearch.Query(&categories, "electronics"),
    gormsearch.Query(&users, "john", gormsearch.WithLimit(5)),
)

if err == nil {
    fmt.Printf("Total products: %d\n", results[0].EstimatedTotal)
    // Access full result including raw hits if needed
    // fmt.Println(results[0].Hits[0]["name"])
}

// With context
results, err := gormsearch.MultiSearchWithContext(ctx, gs,
    gormsearch.Query(&products, "macbook"),
    gormsearch.Query(&categories, "electronics"),
)

// With explicit index name
results, err := gormsearch.MultiSearch(gs,
    gormsearch.QueryIndex(&products, "custom_index", "macbook"),
)
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
results, err := gs.SearchWithContext(ctx, "products", "query")

// Context-aware multi-search
results, err := gs.MultiSearchRawWithContext(ctx, queries...)

// Context-aware sync
err := gs.SyncWithContext(ctx, &Product{})
```

### Sync

```go
gs.Sync(&Product{})
```

## Advanced Features

### 1. Index Settings Sync

Implement `SettingProvider` interface to configure advanced settings like synonyms, stop words, and ranking rules.

```go
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

## Custom Serialization

You can use custom encoder/decoder functions to integrate external serialization libraries like `sonic`, `msgpack`, or `goccy/go-json` for better performance.

### Custom Encoder

```go
gs, err := gormsearch.New(db, meili,
    gormsearch.WithEncoder(func(model any) (map[string]any, error) {
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

### Custom Decoder

```go
gs, err := gormsearch.New(db, meili,
    gormsearch.WithDecoder(func(hits []map[string]any, dest any) error {
        // Use custom logic to decode hits into dest
        data, err := sonic.Marshal(hits)
        if err != nil {
            return err
        }
        return sonic.Unmarshal(data, dest)
    }),
)
```

## Pluggable Workers (Dispatcher)

By default, `gormsearch` uses an in-memory worker pool. You can implement the `Dispatcher` interface to offload syncing to external queues like NATS, Redis, or Kafka.

### Interface

```go
type Dispatcher interface {
    Dispatch(ctx context.Context, job Job) error
}

type Job struct {
    IndexName string
    Operation string         // "create", "update", "delete"
    Document  map[string]any // Encoded document
    ID        string         // Primary Key
}
```

### Example: NATS Dispatcher

```go
type NatsDispatcher struct {
    nc *nats.Conn
}

func (d *NatsDispatcher) Dispatch(ctx context.Context, job gormsearch.Job) error {
    data, _ := json.Marshal(job)
    return d.nc.Publish("meili.sync", data)
}

// Usage
gs, _ := gormsearch.New(db, meili,
    gormsearch.WithDispatcher(&NatsDispatcher{nc: natsConn}),
)
```

### Example: Functional Dispatcher (Concise)

Use `WithDispatcherFunc` to pass a closure directly:

```go
gs, _ := gormsearch.New(db, meili,
    gormsearch.WithDispatcherFunc(func(ctx context.Context, job gormsearch.Job) error {
        data, _ := json.Marshal(job)
        return nc.Publish("meili.sync", data)
    }),
)
```

### Example: NATS Consumer (Worker)

You must implement the worker that listens to the queue. Making it work is easy with `gormsearch.ExecuteJob`:

```go
// In your separate Worker service:
nc.Subscribe("meili.sync", func(m *nats.Msg) {
    var job gormsearch.Job
    if err := json.Unmarshal(m.Data, &job); err != nil {
        return
    }

    // One-line execution!
    err := gormsearch.ExecuteJob(meiliClient, job)
    if err != nil {
        log.Println("Sync fail:", err)
    }
})
```

### Example: Redis Dispatcher & Worker

Using Redis Lists (`LPUSH` / `BRPOP`) as a queue.

**Producer (App):**
```go
gs, _ := gormsearch.New(db, meili,
    gormsearch.WithDispatcherFunc(func(ctx context.Context, job gormsearch.Job) error {
        data, _ := json.Marshal(job)
        return rdb.LPush(ctx, "meili_queue", data).Err()
    }),
)
```

**Consumer (Worker):**
```go
for {
    // Block until a job is available
    result, err := rdb.BRPop(ctx, 0, "meili_queue").Result()
    if err != nil {
        continue
    }

    var job gormsearch.Job
    if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
        continue
    }

    // Process job
    gormsearch.ExecuteJob(meiliClient, job)
}
```

## Errors

```go
gormsearch.ErrNilDB              // Database connection is nil
gormsearch.ErrNilClient          // Meilisearch client is nil
gormsearch.ErrNilModel           // Model is nil
gormsearch.ErrIndexNotRegistered // Index not registered
gormsearch.ErrQueryTooLong       // Query exceeds 1000 characters
```

## License

MIT
