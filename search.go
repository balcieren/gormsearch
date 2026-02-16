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
	}

	if options.MatchingStrategy != "" {
		req.MatchingStrategy = meilisearch.MatchingStrategy(options.MatchingStrategy)
	}

	if len(options.Sort) > 0 {
		req.Sort = options.Sort
	}

	if len(options.Facets) > 0 {
		req.Facets = options.Facets
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
		HitsPerPage:       resp.HitsPerPage,
		Page:              resp.Page,
		TotalPages:        resp.TotalPages,
		TotalHits:         resp.TotalHits,
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
			IndexUID:                q.IndexName,
			Query:                   q.Query,
			Limit:                   limit,
			Offset:                  q.Offset,
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
		}

		if q.MatchingStrategy != "" {
			req.MatchingStrategy = meilisearch.MatchingStrategy(q.MatchingStrategy)
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
	byKey := make(map[string]SearchResult)

	for i, r := range resp.Results {
		sr := SearchResult{
			Hits:              convertHits(r.Hits),
			Query:             r.Query,
			ProcessingTimeMs:  r.ProcessingTimeMs,
			Limit:             r.Limit,
			Offset:            r.Offset,
			EstimatedTotal:    r.EstimatedTotalHits,
			FacetDistribution: parseFacets(r.FacetDistribution),
			HitsPerPage:       r.HitsPerPage,
			Page:              r.Page,
			TotalPages:        r.TotalPages,
			TotalHits:         r.TotalHits,
		}
		results = append(results, sr)

		if i < len(queries) {
			if key := queries[i].Key; key != "" {
				byKey[key] = sr
			}
		}
	}

	return &MultiSearchResult{
		Results:          results,
		ByKey:            byKey,
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
