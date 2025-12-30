package gormsearch

import (
	"context"
	"errors"
	"sync"

	"github.com/meilisearch/meilisearch-go"
	"gorm.io/gorm"
)

// ============================================================================
// Constants
// ============================================================================

const (
	DefaultLimit   int64 = 20
	MaxLimit       int64 = 1000
	DefaultBatch   int   = 100
	DefaultWorkers int   = 10
	MaxQueryLength int   = 1000
)

// ============================================================================
// Errors
// ============================================================================

var (
	ErrNilDB              = errors.New("gormsearch: database connection is nil")
	ErrNilClient          = errors.New("gormsearch: meilisearch client is nil")
	ErrNilModel           = errors.New("gormsearch: model is nil")
	ErrIndexNotRegistered = errors.New("gormsearch: index not registered")
	ErrQueryTooLong       = errors.New("gormsearch: query exceeds maximum length")
	ErrNoQueries          = errors.New("gormsearch: no queries provided")
)

// ============================================================================
// Interfaces
// ============================================================================

// Indexable is an optional interface for custom index names.
type Indexable interface {
	IndexName() string
}

// Tabler is the GORM interface for custom table names.
type Tabler interface {
	TableName() string
}

// Encoder encodes a model to a Meilisearch document.
// Use this to integrate external serialization packages like sonic or msgpack.
type Encoder func(model any) (map[string]any, error)

// Decoder decodes raw hits into a typed slice destination.
// The dest parameter should be a pointer to a slice (e.g., *[]Product).
type Decoder func(hits []map[string]any, dest any) error

// Dispatcher abstracts the execution of a search sync operation.
// Implement this interface to support external queues (NATS, Redis, Kafka).
type Dispatcher interface {
	Dispatch(ctx context.Context, job Job) error
}

// DispatcherFunc allows using a function as a Dispatcher.
type DispatcherFunc func(ctx context.Context, job Job) error

// Dispatch implements the Dispatcher interface.
func (f DispatcherFunc) Dispatch(ctx context.Context, job Job) error {
	return f(ctx, job)
}

// Job represents a unit of work to be synced to Meilisearch.
type Job struct {
	IndexName string
	Operation string         // "create", "update", "delete"
	Document  map[string]any // Encoded document (nil for delete)
	ID        string         // Primary Key value (required for delete)
}

// ============================================================================
// Core Types
// ============================================================================

// GormSearch is the main struct that bridges GORM and Meilisearch.
type GormSearch struct {
	db       *gorm.DB
	client   meilisearch.ServiceManager
	config   *Config
	registry map[string]*IndexConfig
	mu       sync.RWMutex
	pool     *workerPool
}

// Config holds the configuration options for GormSearch.
type Config struct {
	BatchSize  int
	Async      bool
	MaxWorkers int
	MaxRetries int
	OnError    func(op string, err error)
	Encoder    Encoder
	Decoder    Decoder
	Dispatcher Dispatcher
}

// IndexConfig holds the parsed configuration for a registered model.
type IndexConfig struct {
	IndexName        string
	PrimaryKey       string
	ModelType        string
	SearchableFields []string
	FilterableFields []string
	SortableFields   []string
	FieldMapping     map[string]fieldInfo
}

// fieldInfo holds information about a struct field.
type fieldInfo struct {
	JSONName   string
	Searchable bool
	Filterable bool
	Sortable   bool
	PrimaryKey bool
	Skip       bool
}

// ============================================================================
// Search Types
// ============================================================================

// SearchResult wraps the search response from Meilisearch.
type SearchResult struct {
	Hits              []map[string]any
	Query             string
	ProcessingTimeMs  int64
	Limit             int64
	Offset            int64
	EstimatedTotal    int64
	FacetDistribution map[string]map[string]int64
}

// SearchOptions holds options for search queries.
type SearchOptions struct {
	Limit     int64
	Offset    int64
	Filter    string
	Sort      []string
	Facets    []string
	Highlight []string
}

// SearchQuery represents a single query for MultiSearch.
type SearchQuery struct {
	IndexName string
	Query     string
	Limit     int64
	Offset    int64
	Filter    string
	Sort      []string
}

// MultiSearchResult wraps multiple search results.
type MultiSearchResult struct {
	Results          []SearchResult
	ProcessingTimeMs int64
}

// ============================================================================
// Internal Types
// ============================================================================

// workerPool limits concurrent async operations.
type workerPool struct {
	sem chan struct{}
}

func newWorkerPool(size int) *workerPool {
	if size <= 0 {
		size = DefaultWorkers
	}
	return &workerPool{sem: make(chan struct{}, size)}
}

func (p *workerPool) acquire() { p.sem <- struct{}{} }
func (p *workerPool) release() { <-p.sem }
