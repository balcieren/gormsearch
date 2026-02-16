// Package gormsearch provides seamless integration between GORM and Meilisearch.
// It automatically syncs GORM models to Meilisearch using struct tags.
package gormsearch

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"

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

	// Initialize default dispatcher if none provided
	if config.Dispatcher == nil {
		config.Dispatcher = NewDefaultDispatcher(
			client,
			config.MaxWorkers,
			config.MaxRetries,
			config.OnError,
		)
	}

	// If using QueueDispatcher, inject the encoder
	if qd, ok := config.Dispatcher.(*QueueDispatcher); ok {
		if config.JSONEncoder != nil {
			qd.encoder = config.JSONEncoder
		} else {
			qd.encoder = json.Marshal
		}
	}

	return &GormSearch{
		db:             db,
		client:         client,
		config:         config,
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
		ctx:            context.Background(),
	}, nil
}

// WithContext returns a shallow copy of GormSearch with the given context.
// This allows chaining methods with a request-scoped context.
//
// Note: The returned copy shares the registry, config, and mutex with the original.
// It is safe for concurrent read operations (Search, Sync) but Register()
// should only be called on the original instance.
func (gs *GormSearch) WithContext(ctx context.Context) *GormSearch {
	newGS := *gs
	newGS.ctx = ctx
	return &newGS
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
	gs.registryByType[config.ModelType] = config
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
		// Only ignore "index already exists" errors
		var meiliErr *meilisearch.Error
		if !errors.As(err, &meiliErr) || meiliErr.MeilisearchApiError.Code != "index_already_exists" {
			return err
		}
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

// Sync manually syncs all records of a model to Meilisearch.
func (gs *GormSearch) Sync(model any) error {
	ctx := gs.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if model == nil {
		return ErrNilModel
	}

	// Try to reuse registered config for consistency with Register()
	var config *IndexConfig
	modelTypeName := reflect.TypeOf(model).String()
	if t := reflect.TypeOf(model); t.Kind() == reflect.Ptr {
		modelTypeName = t.Elem().Name()
	} else {
		modelTypeName = t.Name()
	}

	gs.mu.RLock()
	config = gs.registryByType[modelTypeName]
	gs.mu.RUnlock()

	if config == nil {
		var err error
		config, err = parseModel(model)
		if err != nil {
			return err
		}
		// Apply index prefix if configured
		if gs.config.IndexPrefix != "" {
			config.IndexName = gs.config.IndexPrefix + config.IndexName
		}
	}

	index := gs.client.Index(config.IndexName)

	pk := config.PrimaryKey
	opts := &meilisearch.DocumentOptions{PrimaryKey: &pk}

	// Create a slice of the model type to load data into
	// We need a pointer to a slice of structs
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	sliceType := reflect.SliceOf(modelType)
	slicePtr := reflect.New(sliceType)

	// Optimization: Allocate reusable document buffer outside the loop
	// We start with nil capacity and let append grow it as needed, or we could estimate.
	var documents []any

	var err error
	err = gs.db.WithContext(ctx).Model(model).FindInBatches(slicePtr.Interface(), gs.config.BatchSize, func(tx *gorm.DB, batch int) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Get the slice value
		sliceVal := slicePtr.Elem()
		if sliceVal.Len() == 0 {
			return nil
		}

		// Reuse buffer: Reset length to 0, keep capacity
		if cap(documents) < sliceVal.Len() {
			documents = make([]any, 0, sliceVal.Len())
		} else {
			documents = documents[:0]
		}

		for i := 0; i < sliceVal.Len(); i++ {
			item := sliceVal.Index(i).Interface()
			// Encode each document using our standard logic (JSONEncoder or Reflection)
			doc, err := gs.encodeDocument(item, config)
			if err != nil {
				// Log error and continue? Or fail batch?
				// Using OnError callback if available, but for Sync we might want to return error
				if gs.config.OnError != nil {
					gs.config.OnError("sync_encode", err)
				}
				continue
			}
			documents = append(documents, doc)
		}

		if len(documents) > 0 {
			if _, err := index.AddDocumentsWithContext(ctx, documents, opts); err != nil {
				return err
			}
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
