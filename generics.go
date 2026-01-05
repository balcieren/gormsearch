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
}

// Of creates a typed searcher for the given GormSearch instance.
//
//	products := gormsearch.Of[Product](gs)
//	results, _ := products.Search("macbook")
func Of[T any](gs *GormSearch) *Searcher[T] {
	return &Searcher[T]{
		gs:        gs,
		indexName: indexNameFor[T](),
	}
}

// WithContext returns a new Searcher with the given context.
//
//	results, _ := products.WithContext(ctx).Search("macbook")
func (s *Searcher[T]) WithContext(ctx context.Context) *Searcher[T] {
	return &Searcher[T]{
		gs:        s.gs.WithContext(ctx),
		indexName: s.indexName,
	}
}

// Search performs a typed search with auto-detected index name.
func (s *Searcher[T]) Search(query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	return SearchFor[T](s.gs, query, opts...)
}

// SearchIndex performs a typed search on a specific index.
func (s *Searcher[T]) SearchIndex(indexName, query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	opts = append(opts, WithIndexName(indexName))
	return SearchFor[T](s.gs, query, opts...)
}

// MultiSearch performs a typed multi-search.
func (s *Searcher[T]) MultiSearch(queries ...SearchQuery) (*TypedMultiSearchResult[T], error) {
	return MultiSearchFor[T](s.gs, queries...)
}

// ============================================================================
// Direct Functions
// ============================================================================

// SearchFor performs a typed search.
// If index name is not provided via WithIndexName option, it is auto-detected from T.
//
//	results, _ := gormsearch.SearchFor[Product](gs, "macbook")
//	results, _ := gormsearch.SearchFor[Product](gs, "macbook", gormsearch.WithIndexName("custom_index"))
func SearchFor[T any](gs *GormSearch, query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	// 1. Determine index name
	// Check if explicit index name is provided in options
	var tempOpts SearchOptions
	for _, opt := range opts {
		opt(&tempOpts)
	}

	indexName := tempOpts.IndexName
	if indexName == "" {
		indexName = indexNameFor[T]()
	}

	// 2. Perform search
	result, err := gs.Search(indexName, query, opts...)
	if err != nil {
		return nil, err
	}

	// 3. Decode results
	var hits []T
	if gs.config != nil && gs.config.MapDecoder != nil {
		if err := gs.config.MapDecoder(result.Hits, &hits); err != nil {
			return nil, err
		}
	} else {
		hits, err = decodeHitsWithInstance[T](gs, result.Hits)
		if err != nil {
			return nil, err
		}
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

// MultiSearchFor performs a typed multi-search.
func MultiSearchFor[T any](gs *GormSearch, queries ...SearchQuery) (*TypedMultiSearchResult[T], error) {
	// For multi-search, index names are in the queries themselves.
	// But wait, `SearchQuery` struct has `IndexName`.
	// The generic wrapper usually implies the result type T.
	// If `queries` have explicit IndexName, we use them.
	// If `queries` have empty IndexName, should we fill it?
	// The current logic of `searchAs` (renamed to internal logic) was:
	// func multiSearchAs[T any](gs *GormSearch, queries ...SearchQuery)
	// It passed queries directly to `MultiSearchRaw`.
	// `MultiSearchRaw` executes them.
	// If the user uses `MultiSearchFor[T]`, they construct `SearchQuery`.
	// `SearchQuery` has `IndexName`.
	// Let's iterate and fill missing index names?

	// We'll rename `multiSearchAs` logic to here and improve it.

	for i := range queries {
		if queries[i].IndexName == "" {
			queries[i].IndexName = indexNameFor[T]()
		}
	}

	result, err := gs.MultiSearchRaw(queries...)
	if err != nil {
		return nil, err
	}

	typedResults := make([]TypedSearchResult[T], 0, len(result.Results))
	for _, r := range result.Results {
		var hits []T
		if gs.config != nil && gs.config.MapDecoder != nil {
			if err := gs.config.MapDecoder(r.Hits, &hits); err != nil {
				return nil, err
			}
		} else {
			var err error
			hits, err = decodeHitsWithInstance[T](gs, r.Hits)
			if err != nil {
				return nil, err
			}
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

// ============================================================================
// Internal
// ============================================================================

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

// decodeHits decodes using JSON roundtrip (Legacy/Fallback).
// Note: This is now only used if GormSearch instance is not available.
// Ideally, use decodeHitsWithInstance for better performance.
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

// decodeHitsWithInstance uses the optimized reflection decoder if available.
func decodeHitsWithInstance[T any](gs *GormSearch, hits []map[string]any) ([]T, error) {
	result := make([]T, 0, len(hits))
	for _, hit := range hits {
		var item T
		// Use optimized decoder
		if err := gs.decodeDocument(hit, &item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

// ============================================================================
// Multi-Type MultiSearch (Fluent Variadic API)
// ============================================================================

// TypedQuery holds a typed query with its closure-based decoder.
type TypedQuery struct {
	indexName     string
	query         string
	opts          SearchOptions
	decodeDefault func(*GormSearch, []map[string]any) error
	decodeCustom  func([]map[string]any, MapDecoder) error
}

// Query creates a typed query with auto-detected index name.
//
//	gormsearch.Query(&products, "macbook")
//	gormsearch.Query(&categories, "electronics", gormsearch.WithLimit(10))
func Query[T any](dest *[]T, query string, opts ...SearchOption) TypedQuery {
	options := SearchOptions{Limit: DefaultLimit}
	for _, opt := range opts {
		opt(&options)
	}

	return TypedQuery{
		indexName: indexNameFor[T](),
		query:     query,
		opts:      options,
		decodeDefault: func(gs *GormSearch, hits []map[string]any) error {
			// Use optimized decoder that needs GS instance
			items, err := decodeHitsWithInstance[T](gs, hits)
			if err != nil {
				return err
			}
			*dest = items
			return nil
		},
		decodeCustom: func(hits []map[string]any, decoder MapDecoder) error {
			var items []T
			if err := decoder(hits, &items); err != nil {
				return err
			}
			*dest = items
			return nil
		},
	}
}

// QueryIndex creates a typed query with explicit index name.
func QueryIndex[T any](dest *[]T, indexName, query string, opts ...SearchOption) TypedQuery {
	options := SearchOptions{Limit: DefaultLimit}
	for _, opt := range opts {
		opt(&options)
	}

	return TypedQuery{
		indexName: indexName,
		query:     query,
		opts:      options,
		decodeDefault: func(gs *GormSearch, hits []map[string]any) error {
			items, err := decodeHitsWithInstance[T](gs, hits)
			if err != nil {
				return err
			}
			*dest = items
			return nil
		},
		decodeCustom: func(hits []map[string]any, decoder MapDecoder) error {
			var items []T
			if err := decoder(hits, &items); err != nil {
				return err
			}
			*dest = items
			return nil
		},
	}
}

// MultiSearch executes multiple typed queries in a single request.
// It returns a slice of SearchResult containing metadata (Total, Facets, etc.) corresponding to each query.
// The actual hits are decoded into the destination slices provided in the queries.
//
//	var products []Product
//	var categories []Category
//	results, err := gs.MultiSearch(
//	    gormsearch.Query(&products, "macbook"),
//	    gormsearch.Query(&categories, "electronics"),
//	)
//	fmt.Println("Total Products:", results[0].EstimatedTotal)
func (gs *GormSearch) MultiSearch(queries ...TypedQuery) ([]SearchResult, error) {
	if len(queries) == 0 {
		return nil, nil
	}

	// Build search queries
	searchQueries := make([]SearchQuery, 0, len(queries))
	for _, q := range queries {
		searchQueries = append(searchQueries, SearchQuery{
			IndexName: q.indexName,
			Query:     q.query,
			Limit:     q.opts.Limit,
			Offset:    q.opts.Offset,
			Filter:    q.opts.Filter,
			Sort:      q.opts.Sort,
		})
	}

	// Execute multi-search (single HTTP request)
	results, err := gs.MultiSearchRaw(searchQueries...)
	if err != nil {
		return nil, err
	}

	metadataResults := make([]SearchResult, 0, len(results.Results))

	// Decode each result using pre-built closures (no reflection)
	for i, q := range queries {
		if i >= len(results.Results) {
			break
		}
		r := results.Results[i]

		// Save metadata AND hits
		metadataResults = append(metadataResults, SearchResult{
			Query:             r.Query,
			ProcessingTimeMs:  r.ProcessingTimeMs,
			Limit:             r.Limit,
			Offset:            r.Offset,
			EstimatedTotal:    r.EstimatedTotal,
			FacetDistribution: r.FacetDistribution,
			Hits:              r.Hits, // Include raw hits
		})

		if gs.config != nil && gs.config.MapDecoder != nil {
			if err := q.decodeCustom(r.Hits, gs.config.MapDecoder); err != nil {
				return nil, err
			}
		} else {
			// Optimized path: Use reflection decoder via closure
			if err := q.decodeDefault(gs, r.Hits); err != nil {
				return nil, err
			}
		}
	}

	return metadataResults, nil
}
