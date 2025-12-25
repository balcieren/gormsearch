// Package gormsearch provides seamless integration between GORM and Meilisearch.
// It automatically syncs GORM models to Meilisearch using struct tags.
package gormsearch

import (
	"context"

	"github.com/meilisearch/meilisearch-go"
	"gorm.io/gorm"
)

// New creates a new GormSearch instance.
// Returns error if db or client is nil.
func New(db *gorm.DB, client meilisearch.ServiceManager, opts ...Option) (*GormSearch, error) {
	if db == nil {
		return nil, ErrNilDB
	}
	if client == nil {
		return nil, ErrNilClient
	}

	config := &Config{
		BatchSize:  DefaultBatch,
		Async:      true,
		MaxWorkers: DefaultWorkers,
	}

	for _, opt := range opts {
		opt(config)
	}

	return &GormSearch{
		db:       db,
		client:   client,
		config:   config,
		registry: make(map[string]*IndexConfig),
		pool:     newWorkerPool(config.MaxWorkers),
	}, nil
}

// MustNew creates a new GormSearch instance, panics on error.
func MustNew(db *gorm.DB, client meilisearch.ServiceManager, opts ...Option) *GormSearch {
	gs, err := New(db, client, opts...)
	if err != nil {
		panic(err)
	}
	return gs
}

// Register registers a model for automatic Meilisearch synchronization.
// The model's struct tags are parsed to configure searchable, filterable,
// and sortable fields.
func (gs *GormSearch) Register(model any) error {
	if model == nil {
		return ErrNilModel
	}

	config, err := parseModel(model)
	if err != nil {
		return err
	}

	// Store in registry (thread-safe)
	gs.mu.Lock()
	gs.registry[config.IndexName] = config
	gs.mu.Unlock()

	// Configure Meilisearch index settings
	if err := gs.configureIndex(config); err != nil {
		return err
	}

	// Register GORM callbacks
	gs.registerCallbacks(config)

	return nil
}

// configureIndex sets up the Meilisearch index with the parsed settings.
func (gs *GormSearch) configureIndex(config *IndexConfig) error {
	index := gs.client.Index(config.IndexName)

	// Create or update index
	_, err := gs.client.CreateIndex(&meilisearch.IndexConfig{
		Uid:        config.IndexName,
		PrimaryKey: config.PrimaryKey,
	})
	if err != nil {
		// Index might already exist, continue with settings update
	}

	// Update searchable attributes
	if len(config.SearchableFields) > 0 {
		if _, err := index.UpdateSearchableAttributes(&config.SearchableFields); err != nil {
			return err
		}
	}

	// Update filterable attributes
	if len(config.FilterableFields) > 0 {
		attrs := toInterfaceSlice(config.FilterableFields)
		if _, err := index.UpdateFilterableAttributes(&attrs); err != nil {
			return err
		}
	}

	// Update sortable attributes
	if len(config.SortableFields) > 0 {
		if _, err := index.UpdateSortableAttributes(&config.SortableFields); err != nil {
			return err
		}
	}

	return nil
}

// toInterfaceSlice converts a string slice to an interface slice.
func toInterfaceSlice(s []string) []any {
	result := make([]any, len(s))
	for i, v := range s {
		result[i] = v
	}
	return result
}

// Sync manually syncs all records of a model to Meilisearch.
func (gs *GormSearch) Sync(model any) error {
	return gs.SyncWithContext(context.Background(), model)
}

// SyncWithContext syncs all records with context for timeout/cancellation.
func (gs *GormSearch) SyncWithContext(ctx context.Context, model any) error {
	if model == nil {
		return ErrNilModel
	}

	config, err := parseModel(model)
	if err != nil {
		return err
	}

	index := gs.client.Index(config.IndexName)

	// Query all records
	var results []map[string]any
	if err := gs.db.WithContext(ctx).Model(model).Find(&results).Error; err != nil {
		return err
	}

	if len(results) == 0 {
		return nil
	}

	pk := config.PrimaryKey
	opts := &meilisearch.DocumentOptions{PrimaryKey: &pk}

	// Batch upload with context check
	for i := 0; i < len(results); i += gs.config.BatchSize {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := i + gs.config.BatchSize
		if end > len(results) {
			end = len(results)
		}

		batch := results[i:end]
		if _, err := index.AddDocumentsWithContext(ctx, batch, opts); err != nil {
			return err
		}
	}

	return nil
}

// Client returns the underlying Meilisearch client.
func (gs *GormSearch) Client() meilisearch.ServiceManager {
	return gs.client
}

// DB returns the underlying GORM database instance.
func (gs *GormSearch) DB() *gorm.DB {
	return gs.db
}

// getConfig returns the IndexConfig for an index name (thread-safe).
func (gs *GormSearch) getConfig(indexName string) (*IndexConfig, bool) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	config, exists := gs.registry[indexName]
	return config, exists
}
