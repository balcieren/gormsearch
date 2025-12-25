package gormsearch

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"

	"github.com/meilisearch/meilisearch-go"
)

var indexNameCache sync.Map

// TypedSearchResult wraps search results with typed hits.
type TypedSearchResult[T any] struct {
	Hits              []T
	Query             string
	ProcessingTimeMs  int64
	Limit             int64
	Offset            int64
	EstimatedTotal    int64
	FacetDistribution map[string]map[string]int64
}

// TypedMultiSearchResult wraps multi-search results with typed hits.
type TypedMultiSearchResult[T any] struct {
	Results          []TypedSearchResult[T]
	ProcessingTimeMs int64
}

// ============================================================================
// Searcher (Fluent API)
// ============================================================================

// Searcher provides typed search operations with fluent API.
type Searcher[T any] struct {
	gs        *GormSearch
	indexName string
	ctx       context.Context
}

// Of creates a typed searcher for the given GormSearch instance.
//
//	products := gormsearch.Of[Product](gs)
//	results, _ := products.Search("macbook")
func Of[T any](gs *GormSearch) *Searcher[T] {
	return &Searcher[T]{
		gs:        gs,
		indexName: indexNameFor[T](),
		ctx:       context.Background(),
	}
}

// WithContext returns a new Searcher with the given context.
//
//	results, _ := products.WithContext(ctx).Search("macbook")
func (s *Searcher[T]) WithContext(ctx context.Context) *Searcher[T] {
	return &Searcher[T]{
		gs:        s.gs,
		indexName: s.indexName,
		ctx:       ctx,
	}
}

// Search performs a typed search with auto-detected index name.
func (s *Searcher[T]) Search(query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	return searchAs[T](s.ctx, s.gs, s.indexName, query, opts...)
}

// SearchIndex performs a typed search on a specific index.
func (s *Searcher[T]) SearchIndex(indexName, query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	return searchAs[T](s.ctx, s.gs, indexName, query, opts...)
}

// MultiSearch performs a typed multi-search.
func (s *Searcher[T]) MultiSearch(queries ...SearchQuery) (*TypedMultiSearchResult[T], error) {
	return multiSearchAs[T](s.ctx, s.gs, queries...)
}

// ============================================================================
// Direct Functions
// ============================================================================

// SearchFor performs a typed search with auto-detected index name.
//
//	results, _ := gormsearch.SearchFor[Product](gs, "macbook")
func SearchFor[T any](gs *GormSearch, query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	return searchAs[T](context.Background(), gs, indexNameFor[T](), query, opts...)
}

// SearchForWithContext performs a typed search with context.
func SearchForWithContext[T any](ctx context.Context, gs *GormSearch, query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	return searchAs[T](ctx, gs, indexNameFor[T](), query, opts...)
}

// SearchAs performs a typed search with explicit index name.
func SearchAs[T any](gs *GormSearch, indexName, query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	return searchAs[T](context.Background(), gs, indexName, query, opts...)
}

// SearchAsWithContext performs a typed search with context and explicit index name.
func SearchAsWithContext[T any](ctx context.Context, gs *GormSearch, indexName, query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	return searchAs[T](ctx, gs, indexName, query, opts...)
}

// MultiSearchAs performs a typed multi-search.
func MultiSearchAs[T any](gs *GormSearch, queries ...SearchQuery) (*TypedMultiSearchResult[T], error) {
	return multiSearchAs[T](context.Background(), gs, queries...)
}

// MultiSearchAsWithContext performs a typed multi-search with context.
func MultiSearchAsWithContext[T any](ctx context.Context, gs *GormSearch, queries ...SearchQuery) (*TypedMultiSearchResult[T], error) {
	return multiSearchAs[T](ctx, gs, queries...)
}

// ============================================================================
// Internal
// ============================================================================

func searchAs[T any](ctx context.Context, gs *GormSearch, indexName, query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	result, err := gs.SearchWithContext(ctx, indexName, query, opts...)
	if err != nil {
		return nil, err
	}

	hits, err := decodeHits[T](result.Hits)
	if err != nil {
		return nil, err
	}

	return &TypedSearchResult[T]{
		Hits:              hits,
		Query:             result.Query,
		ProcessingTimeMs:  result.ProcessingTimeMs,
		Limit:             result.Limit,
		Offset:            result.Offset,
		EstimatedTotal:    result.EstimatedTotal,
		FacetDistribution: result.FacetDistribution,
	}, nil
}

func multiSearchAs[T any](ctx context.Context, gs *GormSearch, queries ...SearchQuery) (*TypedMultiSearchResult[T], error) {
	result, err := gs.MultiSearchWithContext(ctx, queries...)
	if err != nil {
		return nil, err
	}

	typedResults := make([]TypedSearchResult[T], 0, len(result.Results))
	for _, r := range result.Results {
		hits, err := decodeHits[T](r.Hits)
		if err != nil {
			return nil, err
		}
		typedResults = append(typedResults, TypedSearchResult[T]{
			Hits:              hits,
			Query:             r.Query,
			ProcessingTimeMs:  r.ProcessingTimeMs,
			Limit:             r.Limit,
			Offset:            r.Offset,
			EstimatedTotal:    r.EstimatedTotal,
			FacetDistribution: r.FacetDistribution,
		})
	}

	return &TypedMultiSearchResult[T]{
		Results:          typedResults,
		ProcessingTimeMs: result.ProcessingTimeMs,
	}, nil
}

func indexNameFor[T any]() string {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		t = reflect.TypeOf(&zero).Elem()
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	key := t.PkgPath() + "." + t.Name()
	if cached, ok := indexNameCache.Load(key); ok {
		return cached.(string)
	}

	config, err := parseModel(&zero)
	if err != nil {
		return toSnakeCase(t.Name()) + "s"
	}

	indexNameCache.Store(key, config.IndexName)
	return config.IndexName
}

// DecodeHits decodes raw hits into a typed slice.
func DecodeHits[T any](hits []map[string]any) ([]T, error) {
	return decodeHits[T](hits)
}

// DecodeInto decodes Meilisearch hits directly into a typed slice.
func DecodeInto[T any](hits meilisearch.Hits) ([]T, error) {
	result := make([]T, 0, len(hits))
	for _, hit := range hits {
		var item T
		if err := hit.DecodeInto(&item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func decodeHits[T any](hits []map[string]any) ([]T, error) {
	result := make([]T, 0, len(hits))
	for _, hit := range hits {
		data, err := json.Marshal(hit)
		if err != nil {
			return nil, err
		}
		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}
