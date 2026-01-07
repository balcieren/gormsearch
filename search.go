package gormsearch

import (
	"context"
	"encoding/json"

	"github.com/meilisearch/meilisearch-go"
)

// Search performs a search query on the specified index.
func (gs *GormSearch) Search(indexName, query string, opts ...SearchOption) (*SearchResult, error) {
	ctx := gs.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if _, exists := gs.getConfig(indexName); !exists {
		return nil, ErrIndexNotRegistered
	}

	if len(query) > MaxQueryLength {
		return nil, ErrQueryTooLong
	}

	options := &SearchOptions{
		Limit:  DefaultLimit,
		Offset: 0,
	}

	for _, opt := range opts {
		opt(options)
	}

	options.Limit = clampLimit(options.Limit)

	req := &meilisearch.SearchRequest{
		Limit:  options.Limit,
		Offset: options.Offset,
	}

	if options.Filter != "" {
		req.Filter = options.Filter
	}

	if len(options.Sort) > 0 {
		req.Sort = options.Sort
	}

	if len(options.Facets) > 0 {
		req.Facets = options.Facets
	}

	if len(options.Highlight) > 0 {
		req.AttributesToHighlight = options.Highlight
	}

	resp, err := gs.client.Index(indexName).SearchWithContext(ctx, query, req)
	if err != nil {
		return nil, err
	}

	return &SearchResult{
		Hits:              convertHits(resp.Hits),
		Query:             resp.Query,
		ProcessingTimeMs:  resp.ProcessingTimeMs,
		Limit:             resp.Limit,
		Offset:            resp.Offset,
		EstimatedTotal:    resp.EstimatedTotalHits,
		FacetDistribution: parseFacets(resp.FacetDistribution),
	}, nil
}

// MultiSearchRaw performs search across multiple indexes in a single request.
func (gs *GormSearch) MultiSearchRaw(queries ...SearchQuery) (*MultiSearchResult, error) {
	ctx := gs.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if len(queries) == 0 {
		return nil, ErrNoQueries
	}

	searchRequests := make([]*meilisearch.SearchRequest, 0, len(queries))

	for _, q := range queries {
		if _, exists := gs.getConfig(q.IndexName); !exists {
			return nil, ErrIndexNotRegistered
		}

		if len(q.Query) > MaxQueryLength {
			return nil, ErrQueryTooLong
		}

		limit := q.Limit
		if limit == 0 {
			limit = DefaultLimit
		}
		limit = clampLimit(limit)

		req := &meilisearch.SearchRequest{
			IndexUID: q.IndexName,
			Query:    q.Query,
			Limit:    limit,
			Offset:   q.Offset,
		}

		if q.Filter != "" {
			req.Filter = q.Filter
		}

		if len(q.Sort) > 0 {
			req.Sort = q.Sort
		}

		searchRequests = append(searchRequests, req)
	}

	resp, err := gs.client.MultiSearchWithContext(ctx, &meilisearch.MultiSearchRequest{
		Queries: searchRequests,
	})
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		results = append(results, SearchResult{
			Hits:             convertHits(r.Hits),
			Query:            r.Query,
			ProcessingTimeMs: r.ProcessingTimeMs,
			Limit:            r.Limit,
			Offset:           r.Offset,
			EstimatedTotal:   r.EstimatedTotalHits,
		})
	}

	return &MultiSearchResult{
		Results:          results,
		ProcessingTimeMs: resp.ProcessingTimeMs,
	}, nil
}

// clampLimit ensures limit is within valid range.
func clampLimit(limit int64) int64 {
	if limit < 1 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

// convertHits converts Meilisearch hits to a slice of maps.
func convertHits(hits meilisearch.Hits) []map[string]any {
	// Optimization: Avoid json.Unmarshal overhead.
	// We still need to copy the map because map[string]interface{} != map[string]any in Go's type system.
	result := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		m := make(map[string]any, len(hit))
		for k, v := range hit {
			// hit values are interface{}, direct assignment works
			m[k] = v
		}
		result = append(result, m)
	}
	return result
}

// parseFacets converts Meilisearch facet distribution to a typed map.
func parseFacets(raw json.RawMessage) map[string]map[string]int64 {
	if len(raw) == 0 {
		return nil
	}

	var result map[string]map[string]int64
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil
	}
	return result
}
