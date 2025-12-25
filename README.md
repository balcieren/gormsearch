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

    results, _ := gs.Search("products", "macbook",
        gormsearch.WithLimit(10),
        gormsearch.WithFilter("category = 'electronics'"),
        gormsearch.WithSort("price:asc"),
    )

    for _, hit := range results.Hits {
        fmt.Println(hit["name"])
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

## Generics Support

Get typed results instead of `map[string]any`:

```go
// Auto-detect index name from type (recommended)
results, _ := gormsearch.SearchFor[Product](gs, "macbook")

// Or with wrapper
products := gormsearch.Of[Product](gs)
results, _ := products.Search("macbook")

// With context (fluent API)
results, _ := products.WithContext(ctx).Search("macbook")

// Access typed results
for _, p := range results.Hits {
    fmt.Println(p.Name, p.Price)  // Typed!
}
```

### Multi-Type MultiSearch

Search multiple indexes with different types in a single request:

```go
var products []Product
var categories []Category
var users []User

// Single HTTP request, multiple types
err := gormsearch.MultiSearch(gs,
    gormsearch.Query(&products, "macbook"),
    gormsearch.Query(&categories, "electronics"),
    gormsearch.Query(&users, "john", gormsearch.WithLimit(5)),
)

// With context
err := gormsearch.MultiSearchWithContext(ctx, gs,
    gormsearch.Query(&products, "macbook"),
    gormsearch.Query(&categories, "electronics"),
)

// With explicit index name
err := gormsearch.MultiSearch(gs,
    gormsearch.QueryIndex(&products, "custom_index", "macbook"),
)
```

## API

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

### Search

```go
results, err := gs.Search("products", "query",
    gormsearch.WithLimit(20),    // Max: 1000
    gormsearch.WithOffset(0),
    gormsearch.WithFilter("category = 'electronics' AND price < 1000"),
    gormsearch.WithSort("price:asc", "created_at:desc"),
)

// results.Hits - Search results
// results.EstimatedTotal - Estimated total count
// results.ProcessingTimeMs - Processing time in milliseconds
```

### MultiSearch

Search across multiple indexes in a single request (more efficient than multiple Search calls):

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
