package gormsearch

import (
	"context"
	"sync"
	"testing"

	"github.com/meilisearch/meilisearch-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// meilisearch-go v0.37 made SearchRequest.HitsPerPage optional (*int64). The
// option keeps its int64 shape: zero leaves the parameter out of the request.
func TestSearchHitsPerPageIsOptional(t *testing.T) {
	tests := []struct {
		name string
		opts []SearchOption
		want *int64
	}{
		{name: "unset", want: nil},
		{name: "set", opts: []SearchOption{WithHitsPerPage(25)}, want: func() *int64 { n := int64(25); return &n }()},
		{name: "zero", opts: []SearchOption{WithHitsPerPage(0)}, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := new(MockClient)
			mockIndex := new(MockIndex)
			gs := &GormSearch{
				client:   mockClient,
				registry: map[string]*IndexConfig{"products": {IndexName: "products"}},
				mu:       &sync.RWMutex{},
				ctx:      context.Background(),
			}

			mockClient.On("Index", "products").Return(mockIndex)
			mockIndex.On("Search", mock.Anything, "q", mock.MatchedBy(func(req *meilisearch.SearchRequest) bool {
				return assert.ObjectsAreEqual(tt.want, req.HitsPerPage)
			})).Return(&meilisearch.SearchResponse{}, nil)

			_, err := gs.Search("products", "q", tt.opts...)
			assert.NoError(t, err)
			mockIndex.AssertExpectations(t)
		})
	}
}
