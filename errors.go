package gormsearch

import "errors"

// Errors
var (
	ErrNilDB     = errors.New("gormsearch: database connection is nil")
	ErrNilClient = errors.New("gormsearch: meilisearch client is nil")
	ErrNilModel  = errors.New("gormsearch: model is nil")
)

// Default limits
const (
	DefaultLimit   int64 = 20
	MaxLimit       int64 = 1000
	DefaultBatch   int   = 100
	DefaultWorkers int   = 10
	MaxQueryLength int   = 1000
)
