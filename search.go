package gormsearch

import (
	"context"
	"encoding/json"

	"github.com/meilisearch/meilisearch-go"
)

// Search performs a search query on the specified index.
func (gs *GormSearch) Search(indexName, query string, opts ...SearchOption) (*SearchResult, error) {
	resp, err := gs.searchResponse(indexName, query, opts...)
	if err != nil {
		return nil, err
	}

	result := newSearchResult(resp)
	result.Hits = convertHits(resp.Hits)
	return result, nil
}

// searchResponse runs the query and returns Meilisearch's raw response, so
// typed callers can decode straight from the response bytes instead of paying
// for the intermediate []map[string]any that Search builds.
func (gs *GormSearch) searchResponse(indexName, query string, opts ...SearchOption) (*meilisearch.SearchResponse, error) {
	ctx := gs.context()

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
	if options.Offset < 0 {
		options.Offset = 0
	}

	req := &meilisearch.SearchRequest{
		Limit:                   options.Limit,
		Offset:                  options.Offset,
		AttributesToRetrieve:    options.AttributesToRetrieve,
		AttributesToSearchOn:    options.AttributesToSearchOn,
		AttributesToCrop:        options.AttributesToCrop,
		CropLength:              options.CropLength,
		CropMarker:              options.CropMarker,
		AttributesToHighlight:   options.Highlight,
		HighlightPreTag:         options.HighlightPreTag,
		HighlightPostTag:        options.HighlightPostTag,
		ShowMatchesPosition:     options.ShowMatchesPosition,
		ShowRankingScore:        options.ShowRankingScore,
		ShowRankingScoreDetails: options.ShowRankingScoreDetails,
		HitsPerPage:             options.HitsPerPage,
		Page:                    options.Page,
		Distinct:                options.Distinct,
		Filter:                  options.Filter,
		Sort:                    options.Sort,
		Facets:                  options.Facets,
	}

	if options.MatchingStrategy != "" {
		req.MatchingStrategy = meilisearch.MatchingStrategy(options.MatchingStrategy)
	}

	return gs.client.Index(indexName).SearchWithContext(ctx, query, req)
}

// newSearchResult copies the response metadata, leaving Hits to the caller.
func newSearchResult(resp *meilisearch.SearchResponse) *SearchResult {
	return &SearchResult{
		Query:             resp.Query,
		ProcessingTimeMs:  resp.ProcessingTimeMs,
		Limit:             resp.Limit,
		Offset:            resp.Offset,
		EstimatedTotal:    resp.EstimatedTotalHits,
		FacetDistribution: parseFacets(resp.FacetDistribution),
		HitsPerPage:       resp.HitsPerPage,
		Page:              resp.Page,
		TotalPages:        resp.TotalPages,
		TotalHits:         resp.TotalHits,
	}
}

// MultiSearchRaw performs search across multiple indexes in a single request.
func (gs *GormSearch) MultiSearchRaw(queries ...SearchQuery) (*MultiSearchResult, error) {
	resp, err := gs.multiSearchResponse(queries...)
	if err != nil {
		return nil, err
	}
	return buildMultiSearchResult(resp, queries), nil
}

// multiSearchResponse issues the multi-search and returns the raw response.
func (gs *GormSearch) multiSearchResponse(queries ...SearchQuery) (*meilisearch.MultiSearchResponse, error) {
	ctx := gs.context()

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

		offset := q.Offset
		if offset < 0 {
			offset = 0
		}

		req := &meilisearch.SearchRequest{
			IndexUID:                q.IndexName,
			Query:                   q.Query,
			Limit:                   clampLimit(q.Limit),
			Offset:                  offset,
			Filter:                  q.Filter,
			AttributesToRetrieve:    q.AttributesToRetrieve,
			AttributesToSearchOn:    q.AttributesToSearchOn,
			AttributesToCrop:        q.AttributesToCrop,
			CropLength:              q.CropLength,
			CropMarker:              q.CropMarker,
			AttributesToHighlight:   q.AttributesToHighlight,
			HighlightPreTag:         q.HighlightPreTag,
			HighlightPostTag:        q.HighlightPostTag,
			ShowMatchesPosition:     q.ShowMatchesPosition,
			ShowRankingScore:        q.ShowRankingScore,
			ShowRankingScoreDetails: q.ShowRankingScoreDetails,
			HitsPerPage:             q.HitsPerPage,
			Page:                    q.Page,
			Distinct:                q.Distinct,
			Sort:                    q.Sort,
			Facets:                  q.Facets,
		}

		if q.MatchingStrategy != "" {
			req.MatchingStrategy = meilisearch.MatchingStrategy(q.MatchingStrategy)
		}

		searchRequests = append(searchRequests, req)
	}

	return gs.client.MultiSearchWithContext(ctx, &meilisearch.MultiSearchRequest{
		Queries: searchRequests,
	})
}

// buildMultiSearchResult converts a multi-search response, keying results by
// the caller-supplied query keys.
func buildMultiSearchResult(resp *meilisearch.MultiSearchResponse, queries []SearchQuery) *MultiSearchResult {
	results := make([]SearchResult, 0, len(resp.Results))
	var byKey map[string]SearchResult

	for i := range resp.Results {
		sr := *newSearchResult(&resp.Results[i])
		sr.Hits = convertHits(resp.Results[i].Hits)
		results = append(results, sr)

		if i < len(queries) && queries[i].Key != "" {
			if byKey == nil {
				byKey = make(map[string]SearchResult, len(queries))
			}
			byKey[queries[i].Key] = sr
		}
	}

	return &MultiSearchResult{
		Results:          results,
		ByKey:            byKey,
		ProcessingTimeMs: resp.ProcessingTimeMs,
	}
}

// context returns the instance context, defaulting to Background.
func (gs *GormSearch) context() context.Context {
	if gs.ctx != nil {
		return gs.ctx
	}
	return context.Background()
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

// convertHits converts Meilisearch hits (map[string]json.RawMessage) to a slice of maps.
func convertHits(hits meilisearch.Hits) []map[string]any {
	if hits == nil {
		return nil
	}
	result := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		m := make(map[string]any, len(hit))
		for k, raw := range hit {
			var v any
			if err := json.Unmarshal(raw, &v); err == nil {
				m[k] = v
			}
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

	// JSON numbers are decoded as float64 by default, so we unmarshal
	// into float64 first and then convert to int64.
	var rawResult map[string]map[string]float64
	if err := json.Unmarshal(raw, &rawResult); err != nil {
		return nil
	}

	result := make(map[string]map[string]int64, len(rawResult))
	for attr, facets := range rawResult {
		converted := make(map[string]int64, len(facets))
		for key, val := range facets {
			converted[key] = int64(val)
		}
		result[attr] = converted
	}
	return result
}
