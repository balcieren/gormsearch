package gormsearch

// Option is a functional option for configuring GormSearch.
type Option func(*Config)

// SearchOption is a functional option for configuring search queries.
type SearchOption func(*SearchOptions)

// WithBatchSize sets the batch size for bulk operations.
func WithBatchSize(size int) Option {
	return func(c *Config) {
		c.BatchSize = size
	}
}

// WithAsync enables or disables async mode.
func WithAsync(async bool) Option {
	return func(c *Config) {
		c.Async = async
	}
}

// WithLimit sets the maximum number of results to return.
func WithLimit(limit int64) SearchOption {
	return func(o *SearchOptions) {
		o.Limit = limit
	}
}

// WithOffset sets the offset for pagination.
func WithOffset(offset int64) SearchOption {
	return func(o *SearchOptions) {
		o.Offset = offset
	}
}

// WithFilter sets a filter expression for the search.
func WithFilter(filter string) SearchOption {
	return func(o *SearchOptions) {
		o.Filter = filter
	}
}

// WithSort sets the sort order for search results.
func WithSort(sort ...string) SearchOption {
	return func(o *SearchOptions) {
		o.Sort = sort
	}
}

// WithOnError sets an error callback for async operations.
func WithOnError(fn func(op string, err error)) Option {
	return func(c *Config) {
		c.OnError = fn
	}
}

// WithMaxWorkers sets the maximum number of concurrent async operations.
func WithMaxWorkers(n int) Option {
	return func(c *Config) {
		if n > 0 {
			c.MaxWorkers = n
		}
	}
}

// WithMaxRetries sets the maximum number of retries for failed operations.
func WithMaxRetries(n int) Option {
	return func(c *Config) {
		if n > 0 {
			c.MaxRetries = n
		}
	}
}

// WithFacets sets the attributes for faceted search.
func WithFacets(facets ...string) SearchOption {
	return func(o *SearchOptions) {
		o.Facets = facets
	}
}

// WithHighlight sets the attributes to highlight in search results.
func WithHighlight(attrs ...string) SearchOption {
	return func(o *SearchOptions) {
		o.Highlight = attrs
	}
}

// WithEncoder sets a custom encoder for converting models to Meilisearch documents.
// Use this to integrate external serialization packages like sonic or msgpack.
//
//	gs, _ := gormsearch.New(db, meili,
//	    gormsearch.WithEncoder(func(model any) (map[string]any, error) {
//	        // Custom encoding logic
//	    }),
//	)
func WithEncoder(enc Encoder) Option {
	return func(c *Config) {
		c.Encoder = enc
	}
}

// WithDecoder sets a custom decoder for converting search hits to typed results.
// The decoder receives raw hits and a pointer to destination slice.
//
//	gs, _ := gormsearch.New(db, meili,
//	    gormsearch.WithDecoder(func(hits []map[string]any, dest any) error {
//	        // Custom decoding logic
//	    }),
//	)
func WithDecoder(dec Decoder) Option {
	return func(c *Config) {
		c.Decoder = dec
	}
}
