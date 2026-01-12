package gormsearch

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/meilisearch/meilisearch-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockClient is a mock for meilisearch.ServiceManager and Index
type MockClient struct {
	mock.Mock
	meilisearch.ServiceManager // Embed interface
}

func (m *MockClient) Index(uid string) meilisearch.IndexManager {
	args := m.Called(uid)
	return args.Get(0).(meilisearch.IndexManager)
}

func (m *MockClient) MultiSearchWithContext(ctx context.Context, request *meilisearch.MultiSearchRequest) (*meilisearch.MultiSearchResponse, error) {
	args := m.Called(ctx, request)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*meilisearch.MultiSearchResponse), args.Error(1)
}

func (m *MockClient) CreateIndex(config *meilisearch.IndexConfig) (*meilisearch.TaskInfo, error) {
	args := m.Called(config)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*meilisearch.TaskInfo), args.Error(1)
}

// MockIndex is a mock for meilisearch.IndexManager
type MockIndex struct {
	mock.Mock
	meilisearch.IndexManager // Embed interface
}

func (m *MockIndex) SearchWithContext(ctx context.Context, query string, request *meilisearch.SearchRequest) (*meilisearch.SearchResponse, error) {
	args := m.Called(ctx, query, request)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*meilisearch.SearchResponse), args.Error(1)
}

func (m *MockIndex) UpdateSettings(settings *meilisearch.Settings) (*meilisearch.TaskInfo, error) {
	args := m.Called(settings)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*meilisearch.TaskInfo), args.Error(1)
}

func (m *MockIndex) AddDocumentsWithContext(ctx context.Context, documents any, opts *meilisearch.DocumentOptions) (*meilisearch.TaskInfo, error) {
	args := m.Called(ctx, documents, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*meilisearch.TaskInfo), args.Error(1)
}

func (m *MockIndex) UpdateDocumentsWithContext(ctx context.Context, documents any, opts *meilisearch.DocumentOptions) (*meilisearch.TaskInfo, error) {
	args := m.Called(ctx, documents, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*meilisearch.TaskInfo), args.Error(1)
}

func (m *MockIndex) DeleteDocumentWithContext(ctx context.Context, id string, options *meilisearch.DocumentOptions) (*meilisearch.TaskInfo, error) {
	args := m.Called(ctx, id, options)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*meilisearch.TaskInfo), args.Error(1)
}

// Test Models for Generics
type GenericTestProduct struct {
	ID    uint    `json:"id"`
	Name  string  `json:"name" meili:"searchable"`
	Price float64 `json:"price"`
}

func (GenericTestProduct) IndexName() string { return "products" }

type GenericTestCategory struct {
	ID   uint   `json:"id"`
	Name string `json:"name" meili:"searchable"`
}

func (GenericTestCategory) IndexName() string { return "categories" }

func TestSearchFor(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
	}
	gs.registry["products"] = &IndexConfig{IndexName: "products"}

	// Setup expectations
	mockClient.On("Index", "products").Return(mockIndex)

	// Use []meilisearch.Hit with json.RawMessage
	hits1 := []meilisearch.Hit{
		{
			"id":    json.RawMessage(`1`),
			"name":  json.RawMessage(`"Laptop"`),
			"price": json.RawMessage(`1000.0`),
		},
	}

	mockIndex.On("SearchWithContext", mock.Anything, "laptop", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits: hits1,
	}, nil)

	// Execute
	results, err := SearchFor[GenericTestProduct](gs, "laptop")

	// Verify
	assert.NoError(t, err)
	assert.Len(t, results.Hits, 1)
	assert.Equal(t, "Laptop", results.Hits[0].Name)
	assert.Equal(t, 1000.0, results.Hits[0].Price)
}

func TestFluentMultiSearch(t *testing.T) {
	mockClient := new(MockClient)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
	}
	gs.registry["products"] = &IndexConfig{IndexName: "products"}
	gs.registry["categories"] = &IndexConfig{IndexName: "categories"}

	// Prepare output variables
	var products []GenericTestProduct
	var categories []GenericTestCategory

	// Mock response
	hits1 := []meilisearch.Hit{
		{
			"id":    json.RawMessage(`1`),
			"name":  json.RawMessage(`"MacBook"`),
			"price": json.RawMessage(`2000.0`),
		},
	}

	hits2 := []meilisearch.Hit{
		{
			"id":   json.RawMessage(`10`),
			"name": json.RawMessage(`"Electronics"`),
		},
	}

	mockResponse := &meilisearch.MultiSearchResponse{
		Results: []meilisearch.SearchResponse{
			{Hits: hits1},
			{Hits: hits2},
		},
	}

	mockClient.On("MultiSearchWithContext", mock.Anything, mock.MatchedBy(func(req *meilisearch.MultiSearchRequest) bool {
		return len(req.Queries) == 2 &&
			req.Queries[0].IndexUID == "products" &&
			req.Queries[1].IndexUID == "categories"
	})).Return(mockResponse, nil)

	// Execute fluent API
	results, err := gs.MultiSearch(
		Query(&products, "macbook"),
		Query(&categories, "electronics"),
	)

	// Verify
	assert.NoError(t, err)
	assert.Len(t, results.Results, 2)
	assert.Len(t, results.Results[0].Hits, 1) // Verify raw hits are present

	// Check products
	assert.Len(t, products, 1)
	assert.Equal(t, "MacBook", products[0].Name)
	assert.Equal(t, uint(1), products[0].ID)

	// Check categories
	assert.Len(t, categories, 1)
	assert.Equal(t, "Electronics", categories[0].Name)
}

func TestOfSearcher(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
	}
	gs.registry["products"] = &IndexConfig{IndexName: "products"}

	mockClient.On("Index", "products").Return(mockIndex)

	hits := []meilisearch.Hit{
		{
			"id":   json.RawMessage(`1`),
			"name": json.RawMessage(`"Test"`),
		},
	}

	mockIndex.On("SearchWithContext", mock.Anything, "test", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits: hits,
	}, nil)

	// Execute
	products := Of[GenericTestProduct](gs)
	results, err := products.WithContext(context.Background()).Search("test")

	assert.NoError(t, err)
	assert.Len(t, results.Hits, 1)
}

func TestSearcherIndex(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
	}
	gs.registry["products"] = &IndexConfig{IndexName: "products"}
	gs.registry["archived_products"] = &IndexConfig{IndexName: "archived_products"}

	mockClient.On("Index", "archived_products").Return(mockIndex)

	hits := []meilisearch.Hit{
		{
			"id":   json.RawMessage(`2`),
			"name": json.RawMessage(`"Old Product"`),
		},
	}

	mockIndex.On("SearchWithContext", mock.Anything, "old", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits: hits,
	}, nil)

	// Test Of[T].Index() chain
	results, err := Of[GenericTestProduct](gs).Index("archived_products").Search("old")

	assert.NoError(t, err)
	assert.Len(t, results.Hits, 1)
	assert.Equal(t, "Old Product", results.Hits[0].Name)
}

func TestIndexRef(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
	}
	gs.registry["products"] = &IndexConfig{IndexName: "products"}

	mockClient.On("Index", "products").Return(mockIndex)

	hits := []meilisearch.Hit{
		{
			"id":    json.RawMessage(`1`),
			"name":  json.RawMessage(`"MacBook"`),
			"price": json.RawMessage(`2000.0`),
		},
	}

	mockIndex.On("SearchWithContext", mock.Anything, "macbook", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits: hits,
	}, nil)

	// Test gs.Index().Search()
	results, err := gs.Index("products").Search("macbook")

	assert.NoError(t, err)
	assert.Len(t, results.Hits, 1)
	assert.NotNil(t, results.Hits[0]["name"])
}

func TestIndexRefWithContext(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
	}
	gs.registry["products"] = &IndexConfig{IndexName: "products"}

	mockClient.On("Index", "products").Return(mockIndex)

	hits := []meilisearch.Hit{
		{
			"id":   json.RawMessage(`1`),
			"name": json.RawMessage(`"Test"`),
		},
	}

	mockIndex.On("SearchWithContext", mock.Anything, "test", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits: hits,
	}, nil)

	ctx := context.Background()

	// Test gs.Index().WithContext().Search()
	results, err := gs.Index("products").WithContext(ctx).Search("test")

	assert.NoError(t, err)
	assert.Len(t, results.Hits, 1)

	// Verify Name() method
	indexRef := gs.Index("products")
	assert.Equal(t, "products", indexRef.Name())
}
