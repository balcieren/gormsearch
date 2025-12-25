package gormsearch

import (
	"sync"

	"github.com/meilisearch/meilisearch-go"
	"gorm.io/gorm"
)

// Indexable is an optional interface for custom index names.
// If a model implements this interface, its IndexName() will be used.
// Otherwise, the table name from GORM will be used.
type Indexable interface {
	IndexName() string
}

// GormSearch is the main struct that bridges GORM and Meilisearch.
type GormSearch struct {
	db       *gorm.DB
	client   meilisearch.ServiceManager
	config   *Config
	registry map[string]*IndexConfig
	mu       sync.RWMutex // Protects registry
	pool     *workerPool  // Limits concurrent async operations
}

// Config holds the configuration options for GormSearch.
type Config struct {
	BatchSize  int
	Async      bool
	MaxWorkers int                        // Max concurrent async operations (default: 10)
	MaxRetries int                        // Max retries for failed operations (default: 3)
	OnError    func(op string, err error) // Error callback for async operations
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
	Facets    []string // Attributes for faceted search
	Highlight []string // Attributes to highlight
}

// workerPool limits concurrent async operations.
type workerPool struct {
	sem chan struct{}
}

func newWorkerPool(size int) *workerPool {
	if size <= 0 {
		size = 10
	}
	return &workerPool{
		sem: make(chan struct{}, size),
	}
}

func (p *workerPool) acquire() {
	p.sem <- struct{}{}
}

func (p *workerPool) release() {
	<-p.sem
}
