package gormsearch

import (
	"encoding/json"

	"github.com/meilisearch/meilisearch-go"
)

// TypedSearchResult wraps search results with typed hits.
type TypedSearchResult[T any] struct {
	Hits             []T
	Query            string
	ProcessingTimeMs int64
	Limit            int64
	Offset           int64
	EstimatedTotal   int64
}

// TypedMultiSearchResult wraps multi-search results with typed hits.
type TypedMultiSearchResult[T any] struct {
	Results          []TypedSearchResult[T]
	ProcessingTimeMs int64
}

// --- Option 1: Searcher wrapper (for multiple searches) ---

// Searcher provides typed search operations.
type Searcher[T any] struct {
	gs *GormSearch
}

// Of creates a typed searcher for the given GormSearch instance.
// Use this when making multiple typed searches.
//
//	products := gormsearch.Of[Product](gs)
//	results, _ := products.Search("products", "query")
func Of[T any](gs *GormSearch) *Searcher[T] {
	return &Searcher[T]{gs: gs}
}

// Search performs a typed search query.
func (s *Searcher[T]) Search(indexName, query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	return SearchAs[T](s.gs, indexName, query, opts...)
}

// MultiSearch performs a typed multi-search query.
func (s *Searcher[T]) MultiSearch(queries ...SearchQuery) (*TypedMultiSearchResult[T], error) {
	return MultiSearchAs[T](s.gs, queries...)
}

// --- Option 2: Direct functions (for single searches) ---

// SearchAs performs a typed search query.
// Use this for one-off typed searches.
//
//	results, _ := gormsearch.SearchAs[Product](gs, "products", "query")
func SearchAs[T any](gs *GormSearch, indexName, query string, opts ...SearchOption) (*TypedSearchResult[T], error) {
	result, err := gs.Search(indexName, query, opts...)
	if err != nil {
		return nil, err
	}

	hits, err := decodeHits[T](result.Hits)
	if err != nil {
		return nil, err
	}

	return &TypedSearchResult[T]{
		Hits:             hits,
		Query:            result.Query,
		ProcessingTimeMs: result.ProcessingTimeMs,
		Limit:            result.Limit,
		Offset:           result.Offset,
		EstimatedTotal:   result.EstimatedTotal,
	}, nil
}

// MultiSearchAs performs a typed multi-search query.
func MultiSearchAs[T any](gs *GormSearch, queries ...SearchQuery) (*TypedMultiSearchResult[T], error) {
	result, err := gs.MultiSearch(queries...)
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
			Hits:             hits,
			Query:            r.Query,
			ProcessingTimeMs: r.ProcessingTimeMs,
			Limit:            r.Limit,
			Offset:           r.Offset,
			EstimatedTotal:   r.EstimatedTotal,
		})
	}

	return &TypedMultiSearchResult[T]{
		Results:          typedResults,
		ProcessingTimeMs: result.ProcessingTimeMs,
	}, nil
}

// --- Utility functions ---

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

// decodeHits converts map hits to typed struct slice.
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
