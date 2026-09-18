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
	HitsPerPage       int64
	Page              int64
	TotalPages        int64
	TotalHits         int64
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
		indexName: indexNameForGS[T](gs),
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

// Index sets a custom index name, overriding the auto-detected one.
//
//	results, _ := gormsearch.Of[Product](gs).Index("archived_products").Search("macbook")
func (s *Searcher[T]) Index(name string) *Searcher[T] {
	return &Searcher[T]{
		gs:        s.gs,
		indexName: name,
	}
}

// Search performs a typed search with the configured index name.
// Note: Index() takes priority over WithIndexName option.
func (s *Searcher[T]) Search(query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	// Append the stored index name at the end so it takes priority.
	// Build a new slice to avoid mutating the caller's backing array.
	if s.indexName != "" {
		newOpts := make([]SearchOption, 0, len(opts)+1)
		newOpts = append(newOpts, opts...)
		newOpts = append(newOpts, WithIndexName(s.indexName))
		opts = newOpts
	}
	return SearchFor[T](s.gs, query, opts...)
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
		indexName = indexNameForGS[T](gs)
	}

	// 2. Perform search
	resp, err := gs.searchResponse(indexName, query, opts...)
	if err != nil {
		return nil, err
	}

	// 3. Decode results. Only the custom-decoder path needs the hits
	// materialized as maps; the default path reads the response bytes directly.
	var hits []T
	if gs.config != nil && gs.config.MapDecoder != nil {
		if err := gs.config.MapDecoder(convertHits(resp.Hits), &hits); err != nil {
			return nil, err
		}
	} else {
		hits, err = decodeRawHits[T](gs, resp.Hits)
		if err != nil {
			return nil, err
		}
	}

	// 4. Return typed results with metadata
	result := newSearchResult(resp)
	return &TypedSearchResult[T]{
		Hits:              hits,
		Query:             result.Query,
		ProcessingTimeMs:  result.ProcessingTimeMs,
		Limit:             result.Limit,
		Offset:            result.Offset,
		EstimatedTotal:    result.EstimatedTotal,
		FacetDistribution: result.FacetDistribution,
		HitsPerPage:       result.HitsPerPage,
		Page:              result.Page,
		TotalPages:        result.TotalPages,
		TotalHits:         result.TotalHits,
	}, nil
}

// ============================================================================
// Internal
// ============================================================================

// indexNameFor returns the base index name for a generic type T (without prefix).
// It uses caching to avoid repeated reflection lookups.
func indexNameFor[T any]() string {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		t = reflect.TypeOf(&zero).Elem()
	}
	// Dereference all pointer levels (supports T = *Product)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Non-struct types can't be parsed; use the conventional fallback
	if t.Kind() != reflect.Struct {
		return toSnakeCase(t.Name()) + "s"
	}

	key := t.PkgPath() + "." + t.Name()
	if cached, ok := indexNameCache.Load(key); ok {
		return cached.(string)
	}

	// Parse a fresh instance so pointer-receiver methods
	// (IndexName, TableName) are always in the method set.
	config, err := parseModel(reflect.New(t).Interface())
	if err != nil {
		return toSnakeCase(t.Name()) + "s"
	}

	indexNameCache.Store(key, config.IndexName)
	return config.IndexName
}

// indexNameForGS returns the index name for a generic type T, applying the IndexPrefix if configured.
func indexNameForGS[T any](gs *GormSearch) string {
	// First check if the type is registered (which already has the prefix applied)
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		t = reflect.TypeOf(&zero).Elem()
	}

	if config := gs.configForType(t); config != nil {
		return config.IndexName // Already has prefix
	}

	// Fallback: compute base name and apply prefix
	baseName := indexNameFor[T]()
	if gs.config != nil && gs.config.IndexPrefix != "" {
		return gs.config.IndexPrefix + baseName
	}
	return baseName
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
// Note: This is only used when no GormSearch instance is available.
// decodeRawHits is faster and lossless where an instance can be passed.
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

// decodeRawHits decodes Meilisearch hits with the instance's field-index
// decoder, straight from the response bytes.
func decodeRawHits[T any](gs *GormSearch, hits meilisearch.Hits) ([]T, error) {
	result := make([]T, 0, len(hits))
	for _, hit := range hits {
		var item T
		if err := gs.decodeRawDocument(hit, &item); err != nil {
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
	decodeDefault func(*GormSearch, meilisearch.Hits) error
	decodeCustom  func([]map[string]any, MapDecoder) error
}

// As sets a unique key for the query to be used in MultiSearch results.
func (q TypedQuery) As(key string) TypedQuery {
	q.opts.Key = key
	return q
}

// resolveQueryIndex resolves a query's index name against the registry.
// TypedQuery computes its index name without access to the GormSearch
// instance, so IndexPrefix is not applied at that point. This resolves the
// prefixed/registered name when available, matching SearchFor behavior.
func (gs *GormSearch) resolveQueryIndex(name string) string {
	if _, ok := gs.getConfig(name); ok {
		return name
	}
	if gs.config != nil && gs.config.IndexPrefix != "" {
		prefixed := gs.config.IndexPrefix + name
		if _, ok := gs.getConfig(prefixed); ok {
			return prefixed
		}
	}
	return name
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
		decodeDefault: func(gs *GormSearch, hits meilisearch.Hits) error {
			// Use optimized decoder that needs GS instance
			items, err := decodeRawHits[T](gs, hits)
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
		decodeDefault: func(gs *GormSearch, hits meilisearch.Hits) error {
			items, err := decodeRawHits[T](gs, hits)
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
//	fmt.Println("Total Products:", results.Results[0].EstimatedTotal)
func (gs *GormSearch) MultiSearch(queries ...TypedQuery) (*MultiSearchResult, error) {
	if len(queries) == 0 {
		return nil, ErrNoQueries
	}

	// Build search queries
	searchQueries := make([]SearchQuery, 0, len(queries))
	for _, q := range queries {
		searchQueries = append(searchQueries, SearchQuery{
			IndexName:               gs.resolveQueryIndex(q.indexName),
			Query:                   q.query,
			Limit:                   q.opts.Limit,
			Offset:                  q.opts.Offset,
			Filter:                  q.opts.Filter,
			Sort:                    q.opts.Sort,
			Facets:                  q.opts.Facets,
			AttributesToRetrieve:    q.opts.AttributesToRetrieve,
			AttributesToSearchOn:    q.opts.AttributesToSearchOn,
			AttributesToCrop:        q.opts.AttributesToCrop,
			CropLength:              q.opts.CropLength,
			CropMarker:              q.opts.CropMarker,
			AttributesToHighlight:   q.opts.Highlight, // Mapping SearchOptions.Highlight to SearchQuery.AttributesToHighlight
			HighlightPreTag:         q.opts.HighlightPreTag,
			HighlightPostTag:        q.opts.HighlightPostTag,
			MatchingStrategy:        q.opts.MatchingStrategy,
			ShowMatchesPosition:     q.opts.ShowMatchesPosition,
			ShowRankingScore:        q.opts.ShowRankingScore,
			ShowRankingScoreDetails: q.opts.ShowRankingScoreDetails,
			HitsPerPage:             q.opts.HitsPerPage,
			Page:                    q.opts.Page,
			Distinct:                q.opts.Distinct,
			Key:                     q.opts.Key,
		})
	}

	// Execute multi-search (single HTTP request)
	resp, err := gs.multiSearchResponse(searchQueries...)
	if err != nil {
		return nil, err
	}
	results := buildMultiSearchResult(resp, searchQueries)

	// Decode each result using pre-built closures (no reflection)
	for i, q := range queries {
		if i >= len(resp.Results) {
			break
		}

		if gs.config != nil && gs.config.MapDecoder != nil {
			if err := q.decodeCustom(results.Results[i].Hits, gs.config.MapDecoder); err != nil {
				return nil, err
			}
			continue
		}

		// Optimized path: decode from the raw response via closure
		if err := q.decodeDefault(gs, resp.Results[i].Hits); err != nil {
			return nil, err
		}
	}

	return results, nil
}
