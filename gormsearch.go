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

	config.Dispatcher = NewDefaultDispatcher(
		client,
		config.MaxWorkers,
		config.MaxRetries,
		config.OnError,
	)

	return &GormSearch{
		db:       db,
		client:   client,
		config:   config,
		registry: make(map[string]*IndexConfig),
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

	// Apply index prefix if configured
	if gs.config.IndexPrefix != "" {
		config.IndexName = gs.config.IndexPrefix + config.IndexName
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

	settings := gs.buildSettings(config)

	// Update settings if any attribute is set
	if hasSettings(settings) {
		if _, err := index.UpdateSettings(settings); err != nil {
			return err
		}
	}

	return nil
}

// buildSettings constructs the final Meilisearch settings by merging custom provider settings and struct tags.
func (gs *GormSearch) buildSettings(config *IndexConfig) *meilisearch.Settings {
	var settings *meilisearch.Settings

	// 1. Get settings from provider if available
	if provider, ok := config.Model.(SettingProvider); ok {
		settings = provider.MeiliSettings()
	}

	if settings == nil {
		settings = &meilisearch.Settings{}
	}

	// 2. Merge/Fallback to struct tag configurations
	if len(settings.SearchableAttributes) == 0 && len(config.SearchableFields) > 0 {
		settings.SearchableAttributes = config.SearchableFields
	}
	if len(settings.FilterableAttributes) == 0 && len(config.FilterableFields) > 0 {
		settings.FilterableAttributes = config.FilterableFields
	}
	if len(settings.SortableAttributes) == 0 && len(config.SortableFields) > 0 {
		settings.SortableAttributes = config.SortableFields
	}

	return settings
}

// hasSettings checks if any setting attribute is set.
func hasSettings(s *meilisearch.Settings) bool {
	return len(s.SearchableAttributes) > 0 ||
		len(s.FilterableAttributes) > 0 ||
		len(s.SortableAttributes) > 0 ||
		len(s.RankingRules) > 0 ||
		len(s.StopWords) > 0 ||
		len(s.Synonyms) > 0 ||
		s.DistinctAttribute != nil ||
		s.TypoTolerance != nil ||
		s.Faceting != nil ||
		s.Pagination != nil
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

	pk := config.PrimaryKey
	opts := &meilisearch.DocumentOptions{PrimaryKey: &pk}

	// Use FindInBatches to avoid loading all records into memory
	var results []map[string]any

	err = gs.db.WithContext(ctx).Model(model).FindInBatches(&results, gs.config.BatchSize, func(tx *gorm.DB, batch int) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if len(results) == 0 {
			return nil
		}

		if _, err := index.AddDocumentsWithContext(ctx, results, opts); err != nil {
			return err
		}
		return nil
	}).Error

	return err
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
