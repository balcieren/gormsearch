package gormsearch

import "context"

// ============================================================================
// IndexRef (Raw Index Access)
// ============================================================================

// IndexRef provides raw search operations on a specific index.
// Use this when you don't need typed results.
//
//	results, _ := gs.Index("products").Search("macbook")
//	results, _ := gs.Index("products").WithContext(ctx).Search("macbook")
type IndexRef struct {
	gs        *GormSearch
	indexName string
}

// Index returns an IndexRef for raw search operations on the named index.
//
//	results, _ := gs.Index("products").Search("macbook")
func (gs *GormSearch) Index(name string) *IndexRef {
	return &IndexRef{
		gs:        gs,
		indexName: name,
	}
}

// WithContext returns a new IndexRef with the given context.
//
//	results, _ := gs.Index("products").WithContext(ctx).Search("macbook")
func (i *IndexRef) WithContext(ctx context.Context) *IndexRef {
	return &IndexRef{
		gs:        i.gs.WithContext(ctx),
		indexName: i.indexName,
	}
}

// Search performs a raw search on the index.
// Returns untyped results (map[string]any).
//
//	results, _ := gs.Index("products").Search("macbook",
//	    gormsearch.WithLimit(10),
//	    gormsearch.WithFilter("price < 1000"),
//	)
func (i *IndexRef) Search(query string, opts ...SearchOption) (*SearchResult, error) {
	return i.gs.Search(i.indexName, query, opts...)
}

// MultiSearch runs several raw queries in one request. Each query names its own
// index, so this index is not implied; it is a convenience shortcut for
// GormSearch.MultiSearchRaw with this ref's context.
func (i *IndexRef) MultiSearch(queries ...SearchQuery) (*MultiSearchResult, error) {
	return i.gs.MultiSearchRaw(queries...)
}

// Name returns the index name.
func (i *IndexRef) Name() string {
	return i.indexName
}
