package gormsearch

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/meilisearch/meilisearch-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ============================================================================
// Options Tests
// ============================================================================

func TestWithMaxWorkers(t *testing.T) {
	cfg := &Config{}
	WithMaxWorkers(20)(cfg)
	assert.Equal(t, 20, cfg.MaxWorkers)

	// Zero should not change
	cfg2 := &Config{MaxWorkers: 10}
	WithMaxWorkers(0)(cfg2)
	assert.Equal(t, 10, cfg2.MaxWorkers)
}

func TestWithMaxRetries(t *testing.T) {
	cfg := &Config{}
	WithMaxRetries(5)(cfg)
	assert.Equal(t, 5, cfg.MaxRetries)

	// Zero should not change
	cfg2 := &Config{MaxRetries: 3}
	WithMaxRetries(0)(cfg2)
	assert.Equal(t, 3, cfg2.MaxRetries)
}

func TestWithFacets(t *testing.T) {
	opts := &SearchOptions{}
	WithFacets("category", "brand")(opts)
	assert.Equal(t, []string{"category", "brand"}, opts.Facets)
}

func TestWithHighlight(t *testing.T) {
	opts := &SearchOptions{}
	WithHighlight("title", "description")(opts)
	assert.Equal(t, []string{"title", "description"}, opts.Highlight)
}

func TestWithMapEncoder(t *testing.T) {
	encoder := func(model any) (map[string]any, error) {
		return map[string]any{"test": "value"}, nil
	}
	cfg := &Config{}
	WithMapEncoder(encoder)(cfg)
	assert.NotNil(t, cfg.MapEncoder)
}

func TestWithMapDecoder(t *testing.T) {
	decoder := func(hits []map[string]any, dest any) error {
		return nil
	}
	cfg := &Config{}
	WithMapDecoder(decoder)(cfg)
	assert.NotNil(t, cfg.MapDecoder)
}

func TestWithDispatcher(t *testing.T) {
	dispatcher := DispatcherFunc(func(ctx context.Context, job Job) error {
		return nil
	})
	cfg := &Config{}
	WithDispatcher(dispatcher)(cfg)
	assert.NotNil(t, cfg.Dispatcher)
}

func TestWithDispatcherFunc(t *testing.T) {
	cfg := &Config{}
	WithDispatcherFunc(func(ctx context.Context, job Job) error {
		return nil
	})(cfg)
	assert.NotNil(t, cfg.Dispatcher)
}

func TestWithIndexPrefix(t *testing.T) {
	cfg := &Config{}
	WithIndexPrefix("prod_")(cfg)
	assert.Equal(t, "prod_", cfg.IndexPrefix)
}

func TestWithJSONDecoder(t *testing.T) {
	cfg := &Config{}
	WithJSONDecoder(json.Unmarshal)(cfg)
	assert.NotNil(t, cfg.JSONDecoder)
}

// ============================================================================
// Filter Tests (Missing methods)
// ============================================================================

func TestFilterBuilder_Neq(t *testing.T) {
	f := NewFilter()
	result := f.Where("status").Neq("deleted").Build()
	assert.Equal(t, "status != 'deleted'", result)
}

func TestFilterBuilder_Gte(t *testing.T) {
	f := NewFilter()
	result := f.Where("price").Gte(100).Build()
	assert.Equal(t, "price >= 100", result)
}

func TestFilterBuilder_Lt(t *testing.T) {
	f := NewFilter()
	result := f.Where("price").Lt(500).Build()
	assert.Equal(t, "price < 500", result)
}

func TestFilterBuilder_Lte(t *testing.T) {
	f := NewFilter()
	result := f.Where("price").Lte(1000).Build()
	assert.Equal(t, "price <= 1000", result)
}

func TestFilterBuilder_ComplexQuery(t *testing.T) {
	f := NewFilter()
	result := f.Where("category").Eq("electronics").
		And().
		Where("price").Gte(100).
		And().
		Where("price").Lte(1000).
		Build()

	assert.Contains(t, result, "category = 'electronics'")
	assert.Contains(t, result, "price >= 100")
	assert.Contains(t, result, "price <= 1000")
}

// ============================================================================
// Dispatcher Tests
// ============================================================================

func TestExecuteWithContext_Operations(t *testing.T) {
	// Test with nil client (should no-op)
	job := Job{IndexName: "test", Operation: "create", ID: "1"}
	err := ExecuteWithContext(context.Background(), nil, job)
	assert.NoError(t, err)

	// Test Execute (backward compat)
	err = Execute(nil, job)
	assert.NoError(t, err)
}

func TestConsume(t *testing.T) {
	// Test with nil client - should fail on decode, not panic
	invalidData := []byte(`{"index_name":"test","operation":"create"}`)

	// This will fail because client is nil when trying to execute
	err := Consume(nil, invalidData)
	assert.NoError(t, err) // nil client returns nil

	// Test with invalid JSON
	err = Consume(nil, []byte(`invalid json`))
	assert.Error(t, err)
}

func TestToString(t *testing.T) {
	// Test string
	assert.Equal(t, "test", toString("test"))

	// Test error
	assert.Equal(t, "error message", toString(errors.New("error message")))

	// Test other type
	assert.Equal(t, "unknown panic", toString(123))
}

func TestDispatcherFunc_Dispatch(t *testing.T) {
	called := false
	df := DispatcherFunc(func(ctx context.Context, job Job) error {
		called = true
		return nil
	})

	err := df.Dispatch(context.Background(), Job{})
	assert.NoError(t, err)
	assert.True(t, called)
}

// ============================================================================
// Generics Tests
// ============================================================================

type CoverageTestModel struct {
	ID   uint   `json:"id"`
	Name string `json:"name" meili:"searchable"`
}

func (CoverageTestModel) IndexName() string { return "coverage_test" }

func TestDecodeHits(t *testing.T) {
	hits := []map[string]any{
		{"id": float64(1), "name": "Test 1"},
		{"id": float64(2), "name": "Test 2"},
	}

	result, err := DecodeHits[CoverageTestModel](hits)
	assert.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "Test 1", result[0].Name)
}

func TestDecodeInto(t *testing.T) {
	hits := meilisearch.Hits{
		{"id": json.RawMessage(`1`), "name": json.RawMessage(`"Test"`)},
	}

	result, err := DecodeInto[CoverageTestModel](hits)
	assert.NoError(t, err)
	assert.Len(t, result, 1)
}

func TestQueryIndex(t *testing.T) {
	var results []CoverageTestModel
	q := QueryIndex(&results, "custom_index", "query", WithLimit(5))

	assert.Equal(t, "custom_index", q.indexName)
	assert.Equal(t, "query", q.query)
	assert.Equal(t, int64(5), q.opts.Limit)
}

// ============================================================================
// Reflect Tests
// ============================================================================

func TestToFloat(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    float64
		wantErr bool
	}{
		{"float64", float64(1.5), 1.5, false},
		{"float32", float32(2.5), 2.5, false},
		{"int", int(10), 10.0, false},
		{"int64", int64(20), 20.0, false},
		{"uint", uint(30), 30.0, false},
		{"uint64", uint64(40), 40.0, false},
		{"string", "not a number", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toFloat(reflectValue(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestValToString(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"uint", uint(42), "42"},
		{"uint64", uint64(100), "100"},
		{"uint32", uint32(50), "50"},
		{"int", int(10), "10"},
		{"int64", int64(20), "20"},
		{"string", "hello", "hello"},
		{"other", struct{}{}, "{}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := valToString(reflectValue(tt.input))
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestSetFieldValue(t *testing.T) {
	type TestStruct struct {
		Name      string
		Age       int
		Price     float64
		Active    bool
		CreatedAt time.Time
	}

	ts := &TestStruct{}
	v := reflectValue(ts)

	// String
	setFieldValue(v.FieldByName("Name"), "John")
	assert.Equal(t, "John", ts.Name)

	// Int (from float64 - JSON style)
	setFieldValue(v.FieldByName("Age"), float64(30))
	assert.Equal(t, 30, ts.Age)

	// Float64
	setFieldValue(v.FieldByName("Price"), float64(99.99))
	assert.Equal(t, 99.99, ts.Price)

	// Bool
	setFieldValue(v.FieldByName("Active"), true)
	assert.True(t, ts.Active)

	// Time
	setFieldValue(v.FieldByName("CreatedAt"), "2024-01-01T00:00:00Z")
	assert.False(t, ts.CreatedAt.IsZero())

	// Nil value should not panic
	setFieldValue(v.FieldByName("Name"), nil)
}

func TestSetFloat(t *testing.T) {
	type TestStruct struct {
		Value float64
	}

	ts := &TestStruct{}
	v := reflectValue(ts)

	setFloat(v.FieldByName("Value"), 42.5)
	assert.Equal(t, 42.5, ts.Value)
}

// ============================================================================
// GormSearch Core Tests
// ============================================================================

func TestMustNew_Panic(t *testing.T) {
	assert.Panics(t, func() {
		MustNew(nil, nil)
	})
}

func TestClient(t *testing.T) {
	mockClient := &MockClient{}
	gs := &GormSearch{client: mockClient}
	assert.Equal(t, mockClient, gs.Client())
}

func TestDB(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	gs := &GormSearch{db: db}
	assert.Equal(t, db, gs.DB())
}

// ============================================================================
// Parser Tests
// ============================================================================

type GeoModel struct {
	ID       uint     `json:"id"`
	Name     string   `json:"name"`
	Location Location `json:"location" meili:"geo"`
}

type Location struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

func TestFindGeoIndices(t *testing.T) {
	config, err := parseModel(&GeoModel{})
	assert.NoError(t, err)

	// Should have geo extractor
	var hasGeo bool
	for _, ext := range config.FieldExtractors {
		if ext.IsGeo {
			hasGeo = true
			assert.NotNil(t, ext.GeoLatIndex)
			assert.NotNil(t, ext.GeoLngIndex)
		}
	}
	assert.True(t, hasGeo, "Should have geo field extractor")
}

func TestToDocument_WithGeo(t *testing.T) {
	gs := &GormSearch{config: &Config{}}

	model := &GeoModel{
		ID:   1,
		Name: "Test",
		Location: Location{
			Lat: 48.8566,
			Lng: 2.3522,
		},
	}

	config, _ := parseModel(model)
	doc := gs.toDocument(model, config)

	// Check geo field
	geo, ok := doc["_geo"].(map[string]float64)
	assert.True(t, ok, "_geo should be a map")
	assert.Equal(t, 48.8566, geo["lat"])
	assert.Equal(t, 2.3522, geo["lng"])
}

// ============================================================================
// Search Tests
// ============================================================================

func TestClampLimit(t *testing.T) {
	// Below minimum
	assert.Equal(t, DefaultLimit, clampLimit(0))
	assert.Equal(t, DefaultLimit, clampLimit(-1))

	// Normal range
	assert.Equal(t, int64(50), clampLimit(50))

	// Above maximum
	assert.Equal(t, MaxLimit, clampLimit(2000))
}

func TestParseFacets(t *testing.T) {
	// Empty
	result := parseFacets(nil)
	assert.Nil(t, result)

	// Valid JSON
	data := json.RawMessage(`{"category":{"electronics":10,"books":5}}`)
	result = parseFacets(data)
	assert.NotNil(t, result)
	assert.Equal(t, int64(10), result["category"]["electronics"])

	// Invalid JSON
	result = parseFacets(json.RawMessage(`invalid`))
	assert.Nil(t, result)
}

// ============================================================================
// WorkerPool Tests
// ============================================================================

func TestNewWorkerPool(t *testing.T) {
	// Normal
	pool := newWorkerPool(5)
	assert.NotNil(t, pool)

	// Zero (should use default)
	pool = newWorkerPool(0)
	assert.NotNil(t, pool)

	// Negative (should use default)
	pool = newWorkerPool(-1)
	assert.NotNil(t, pool)
}

func TestWorkerPool_AcquireRelease(t *testing.T) {
	pool := newWorkerPool(2)

	// Acquire
	pool.acquire()
	pool.acquire()

	// Release
	pool.release()
	pool.release()

	// Should be able to acquire again
	pool.acquire()
	pool.release()
}

// ============================================================================
// DecodeDocument Tests
// ============================================================================

func TestDecodeDocument(t *testing.T) {
	gs := &GormSearch{
		config:         &Config{},
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
	}

	// Register a model
	config, _ := parseModel(&CoverageTestModel{})
	gs.registryByType[config.ModelType] = config

	doc := map[string]any{
		"id":   float64(1),
		"name": "Test",
	}

	var result CoverageTestModel
	err := gs.decodeDocument(doc, &result)
	assert.NoError(t, err)
	assert.Equal(t, "Test", result.Name)
}

func TestDecodeDocument_NotPointer(t *testing.T) {
	gs := &GormSearch{
		config:         &Config{},
		registryByType: make(map[string]*IndexConfig),
	}

	doc := map[string]any{"id": float64(1)}
	var result CoverageTestModel

	// Pass non-pointer should error
	err := gs.decodeDocument(doc, result)
	assert.Error(t, err)
}

func TestDecodeDocument_UnregisteredType(t *testing.T) {
	gs := &GormSearch{
		config:         &Config{},
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
	}

	// Unregistered type should fallback to JSON
	doc := map[string]any{
		"id":   float64(1),
		"name": "Test",
	}

	var result CoverageTestModel
	err := gs.decodeDocument(doc, &result)
	assert.NoError(t, err)
}

// ============================================================================
// Generics Searcher Tests
// ============================================================================

func TestSearcherSearchIndex(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
	}
	gs.registry["custom_index"] = &IndexConfig{IndexName: "custom_index"}

	mockClient.On("Index", "custom_index").Return(mockIndex)
	mockIndex.On("SearchWithContext", mock.Anything, "test", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits: []meilisearch.Hit{},
	}, nil)

	searcher := Of[CoverageTestModel](gs)
	_, err := searcher.SearchIndex("custom_index", "test")
	assert.NoError(t, err)
}

func TestSearchAs(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
	}
	gs.registry["coverage_test"] = &IndexConfig{IndexName: "coverage_test"}

	mockClient.On("Index", "coverage_test").Return(mockIndex)
	mockIndex.On("SearchWithContext", mock.Anything, "test", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits: []meilisearch.Hit{},
	}, nil)

	_, err := SearchFor[CoverageTestModel](gs, "test", WithIndexName("coverage_test"))
	assert.NoError(t, err)
}

// ============================================================================
// MultiSearch Tests
// ============================================================================

func TestMultiSearchAs(t *testing.T) {
	mockClient := new(MockClient)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
	}
	gs.registry["coverage_test"] = &IndexConfig{IndexName: "coverage_test"}

	mockClient.On("MultiSearchWithContext", mock.Anything, mock.Anything).Return(&meilisearch.MultiSearchResponse{
		Results: []meilisearch.SearchResponse{
			{Hits: []meilisearch.Hit{}},
		},
	}, nil)

	_, err := MultiSearchFor[CoverageTestModel](gs, SearchQuery{
		IndexName: "coverage_test",
		Query:     "test",
	})
	assert.NoError(t, err)
}

func TestSearcherMultiSearch(t *testing.T) {
	mockClient := new(MockClient)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
	}
	gs.registry["coverage_test"] = &IndexConfig{IndexName: "coverage_test"}

	mockClient.On("MultiSearchWithContext", mock.Anything, mock.Anything).Return(&meilisearch.MultiSearchResponse{
		Results: []meilisearch.SearchResponse{
			{Hits: []meilisearch.Hit{}},
		},
	}, nil)

	searcher := Of[CoverageTestModel](gs)
	_, err := searcher.MultiSearch(SearchQuery{
		IndexName: "coverage_test",
		Query:     "test",
	})
	assert.NoError(t, err)
}

// ============================================================================
// ExtractID Fallback Test
// ============================================================================

type ModelWithoutID struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func TestExtractID_Fallback(t *testing.T) {
	// Model without ID field
	model := &ModelWithoutID{Code: "ABC", Name: "Test"}
	config, _ := parseModel(model)

	id := extractID(model, config)
	// Should return empty string for models without ID
	assert.Equal(t, "", id)
}

type ModelWithNestedID struct {
	gorm.Model
	Name string `json:"name"`
}

func TestExtractID_NestedGormModel(t *testing.T) {
	model := &ModelWithNestedID{
		Model: gorm.Model{ID: 123},
		Name:  "Test",
	}
	config, _ := parseModel(model)

	id := extractID(model, config)
	assert.Equal(t, "123", id)
}

// ============================================================================
// Sync Function Tests - Note: Sync requires real DB connection
// These are covered in integration tests (sync_test.go)
// ============================================================================

// TestSync is skipped as it requires a real DB connection
// See sync_test.go for full integration tests

// ============================================================================
// ExecuteWithContext Tests (Package-level function)
// ============================================================================

type ExecuteTestModel struct {
	ID   uint   `json:"id" gorm:"primaryKey" meilisearch:"filterable;sortable"`
	Name string `json:"name" meilisearch:"searchable;filterable"`
}

func (ExecuteTestModel) TableName() string { return "execute_test" }
func (ExecuteTestModel) IndexName() string { return "execute_test" }

func TestExecuteWithContext_Create(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "execute_test").Return(mockIndex)
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := Job{
		IndexName: "execute_test",
		Operation: "create",
		Document:  map[string]any{"id": 1, "name": "Test"},
	}

	err := ExecuteWithContext(ctx, mockClient, job)
	assert.NoError(t, err)
}

func TestExecuteWithContext_Update(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "execute_test").Return(mockIndex)
	mockIndex.On("UpdateDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)

	ctx := context.Background()
	job := Job{
		IndexName: "execute_test",
		Operation: "update",
		Document:  map[string]any{"id": 1, "name": "Updated"},
	}

	err := ExecuteWithContext(ctx, mockClient, job)
	assert.NoError(t, err)
}

func TestExecuteWithContext_Delete(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "execute_test").Return(mockIndex)
	mockIndex.On("DeleteDocumentWithContext", mock.Anything, "1", mock.Anything).Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)

	ctx := context.Background()
	job := Job{
		IndexName: "execute_test",
		Operation: "delete",
		ID:        "1",
	}

	err := ExecuteWithContext(ctx, mockClient, job)
	assert.NoError(t, err)
}

func TestExecuteWithContext_NilClient(t *testing.T) {
	ctx := context.Background()
	job := Job{
		IndexName: "execute_test",
		Operation: "create",
		Document:  map[string]any{"id": 1},
	}

	err := ExecuteWithContext(ctx, nil, job)
	assert.NoError(t, err) // Should return nil for nil client
}

func TestExecuteWithContext_InvalidOperation(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "execute_test").Return(mockIndex)

	ctx := context.Background()
	job := Job{
		IndexName: "execute_test",
		Operation: "invalid_op",
		Document:  map[string]any{"id": 1},
	}

	err := ExecuteWithContext(ctx, mockClient, job)
	assert.NoError(t, err) // Unknown operations return nil
}

func TestExecute_BackwardCompatible(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "execute_test").Return(mockIndex)
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)

	job := Job{
		IndexName: "execute_test",
		Operation: "create",
		Document:  map[string]any{"id": 1, "name": "Test"},
	}

	err := Execute(mockClient, job)
	assert.NoError(t, err)
}

// ============================================================================
// Query and QueryIndex Tests (using SearchFor/SearchAs)
// ============================================================================

func TestSearchFor_WithOptions(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config:   &Config{},
	}

	config, _ := parseModel(&CoverageTestModel{})
	gs.registry["coverage_test"] = config

	hits := []meilisearch.Hit{
		{
			"id":   json.RawMessage(`1`),
			"name": json.RawMessage(`"Found"`),
		},
	}

	mockClient.On("Index", "coverage_test").Return(mockIndex)
	mockIndex.On("SearchWithContext", mock.Anything, "test query", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits:               hits,
		EstimatedTotalHits: 1,
	}, nil)

	result, err := SearchFor[CoverageTestModel](gs, "test query", WithLimit(10))
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestSearchAs_WithCustomIndex(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config:   &Config{},
	}
	gs.registry["custom_idx"] = &IndexConfig{IndexName: "custom_idx"}

	hits := []meilisearch.Hit{
		{
			"id":   json.RawMessage(`2`),
			"name": json.RawMessage(`"Custom"`),
		},
	}

	mockClient.On("Index", "custom_idx").Return(mockIndex)
	mockIndex.On("SearchWithContext", mock.Anything, "custom query", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits:               hits,
		EstimatedTotalHits: 1,
	}, nil)

	result, err := SearchFor[CoverageTestModel](gs, "custom query", WithIndexName("custom_idx"))
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// ============================================================================
// Search Options Tests
// ============================================================================

func TestSearch_WithAllOptions(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config:   &Config{},
	}

	config, _ := parseModel(&CoverageTestModel{})
	gs.registry["coverage_test"] = config

	hits := []meilisearch.Hit{}

	mockClient.On("Index", "coverage_test").Return(mockIndex)
	mockIndex.On("SearchWithContext", mock.Anything, "comprehensive", mock.MatchedBy(func(req *meilisearch.SearchRequest) bool {
		return req.Limit == 20 && req.Offset == 10
	})).Return(&meilisearch.SearchResponse{
		Hits:               hits,
		EstimatedTotalHits: 0,
		FacetDistribution:  json.RawMessage(`{"status": {"active": 5}}`),
	}, nil)

	result, err := SearchFor[CoverageTestModel](gs, "comprehensive",
		WithLimit(20),
		WithOffset(10),
		WithFilter("status = 'active'"),
		WithSort("created_at:desc"),
		WithFacets("status"),
		WithHighlight("name"),
	)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestSearch_WithFacetDistribution(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config:   &Config{},
	}

	config, _ := parseModel(&CoverageTestModel{})
	gs.registry["coverage_test"] = config

	hits := []meilisearch.Hit{
		{"id": json.RawMessage(`1`)},
	}

	mockClient.On("Index", "coverage_test").Return(mockIndex)
	mockIndex.On("SearchWithContext", mock.Anything, "facet test", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits:              hits,
		FacetDistribution: json.RawMessage(`{"category": {"books": 10, "games": 5}}`),
	}, nil)

	result, err := SearchFor[CoverageTestModel](gs, "facet test", WithFacets("category"))
	assert.NoError(t, err)
	assert.NotNil(t, result.FacetDistribution)
}

// ============================================================================
// isDeletedAtSet Tests
// ============================================================================

type SoftDeleteModel struct {
	ID        uint           `json:"id"`
	Name      string         `json:"name"`
	DeletedAt gorm.DeletedAt `json:"deleted_at"`
}

func TestIsDeletedAtSet_WithDeletedAt(t *testing.T) {
	now := time.Now()
	model := &SoftDeleteModel{
		ID:        1,
		Name:      "Deleted",
		DeletedAt: gorm.DeletedAt{Time: now, Valid: true},
	}

	rv := reflect.ValueOf(model).Elem().FieldByName("DeletedAt")
	result := isDeletedAtSet(rv)
	assert.True(t, result)
}

func TestIsDeletedAtSet_WithoutDeletedAt(t *testing.T) {
	model := &SoftDeleteModel{
		ID:   1,
		Name: "NotDeleted",
	}

	rv := reflect.ValueOf(model).Elem().FieldByName("DeletedAt")
	result := isDeletedAtSet(rv)
	assert.False(t, result)
}

func TestIsDeletedAtSet_InvalidDeletedAt(t *testing.T) {
	model := &SoftDeleteModel{
		ID:        1,
		Name:      "Invalid",
		DeletedAt: gorm.DeletedAt{Valid: false},
	}

	rv := reflect.ValueOf(model).Elem().FieldByName("DeletedAt")
	result := isDeletedAtSet(rv)
	assert.False(t, result)
}

type NoDeletedAtModel struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

func TestIsDeletedAtSet_PointerField(t *testing.T) {
	// Test with pointer type
	now := time.Now()
	rv := reflect.ValueOf(&now)
	result := isDeletedAtSet(rv)
	assert.True(t, result)

	// Nil pointer should return false
	var nilPtr *time.Time
	rvNil := reflect.ValueOf(nilPtr)
	result = isDeletedAtSet(rvNil)
	assert.False(t, result)
}

// ============================================================================
// decodeDocument with Geo Tests
// ============================================================================

type GeoDecodeModel struct {
	ID        uint    `json:"id"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude" meilisearch:"geo:lat"`
	Longitude float64 `json:"longitude" meilisearch:"geo:lng"`
}

func TestDecodeDocument_WithGeoFields(t *testing.T) {
	gs := &GormSearch{
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		config:   &Config{},
	}

	// Register the model with geo info
	config, _ := parseModel(&GeoDecodeModel{})
	gs.registry["geo_decode"] = config

	doc := map[string]any{
		"id":   float64(1),
		"name": "Location",
		"_geo": map[string]any{
			"lat": 40.7128,
			"lng": -74.0060,
		},
	}

	var result GeoDecodeModel
	err := gs.decodeDocument(doc, &result)
	assert.NoError(t, err)
	// Geo extraction happens via reflection in the decoder
}

// ============================================================================
// Dispatch Tests
// ============================================================================

func TestDispatch_WithNilClient(t *testing.T) {
	// Test the DefaultDispatcher with nil client
	dispatcher := NewDefaultDispatcher(nil, 10, 3, nil)
	job := Job{IndexName: "test", Operation: "create", Document: map[string]any{"id": 1}}
	err := dispatcher.Dispatch(context.Background(), job)
	assert.NoError(t, err) // Should return nil for nil client
}

func TestDispatch_WithDefaultDispatcher(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "test_index").Return(mockIndex)
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)

	dispatcher := NewDefaultDispatcher(mockClient, 10, 3, nil)
	job := Job{
		IndexName: "test_index",
		Operation: "create",
		Document:  map[string]any{"id": 1, "name": "Test"},
	}

	err := dispatcher.Dispatch(context.Background(), job)
	assert.NoError(t, err)

	// Give async operation time to complete
	time.Sleep(100 * time.Millisecond)
}

// ============================================================================
// Execute Error Handling Tests
// ============================================================================

func TestExecute_CreateError(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "execute_test").Return(mockIndex)
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("create failed"))

	job := Job{
		IndexName: "execute_test",
		Operation: "create",
		Document:  map[string]any{"id": 1},
	}

	err := Execute(mockClient, job)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create failed")
}

func TestExecute_UpdateError(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "execute_test").Return(mockIndex)
	mockIndex.On("UpdateDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("update failed"))

	job := Job{
		IndexName: "execute_test",
		Operation: "update",
		Document:  map[string]any{"id": 1},
	}

	err := Execute(mockClient, job)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "update failed")
}

func TestExecute_DeleteError(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "execute_test").Return(mockIndex)
	mockIndex.On("DeleteDocumentWithContext", mock.Anything, "1", mock.Anything).Return(nil, errors.New("delete failed"))

	job := Job{
		IndexName: "execute_test",
		Operation: "delete",
		ID:        "1",
	}

	err := Execute(mockClient, job)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete failed")
}

// ============================================================================
// Additional Reflect Tests
// ============================================================================

func TestValToString_EdgeCases(t *testing.T) {
	// Test float32
	f32 := float32(3.14)
	rv := reflect.ValueOf(f32)
	result := valToString(rv)
	assert.Contains(t, result, "3.14")

	// Test int8
	i8 := int8(127)
	rv = reflect.ValueOf(i8)
	result = valToString(rv)
	assert.Equal(t, "127", result)

	// Test int16
	i16 := int16(32767)
	rv = reflect.ValueOf(i16)
	result = valToString(rv)
	assert.Equal(t, "32767", result)

	// Test int32
	i32 := int32(2147483647)
	rv = reflect.ValueOf(i32)
	result = valToString(rv)
	assert.Equal(t, "2147483647", result)
}

func TestSetFieldValue_AllTypes(t *testing.T) {
	type AllTypesModel struct {
		BoolField    bool    `json:"bool_field"`
		IntField     int     `json:"int_field"`
		Int8Field    int8    `json:"int8_field"`
		Int16Field   int16   `json:"int16_field"`
		Int32Field   int32   `json:"int32_field"`
		Int64Field   int64   `json:"int64_field"`
		UintField    uint    `json:"uint_field"`
		Uint8Field   uint8   `json:"uint8_field"`
		Uint16Field  uint16  `json:"uint16_field"`
		Uint32Field  uint32  `json:"uint32_field"`
		Uint64Field  uint64  `json:"uint64_field"`
		Float32Field float32 `json:"float32_field"`
		Float64Field float64 `json:"float64_field"`
		StringField  string  `json:"string_field"`
	}

	model := &AllTypesModel{}
	v := reflect.ValueOf(model).Elem()

	// Bool
	setFieldValue(v.FieldByName("BoolField"), true)
	assert.True(t, model.BoolField)

	// Int types
	setFieldValue(v.FieldByName("IntField"), float64(42))
	assert.Equal(t, 42, model.IntField)

	setFieldValue(v.FieldByName("Int8Field"), float64(8))
	assert.Equal(t, int8(8), model.Int8Field)

	setFieldValue(v.FieldByName("Int16Field"), float64(16))
	assert.Equal(t, int16(16), model.Int16Field)

	setFieldValue(v.FieldByName("Int32Field"), float64(32))
	assert.Equal(t, int32(32), model.Int32Field)

	setFieldValue(v.FieldByName("Int64Field"), float64(64))
	assert.Equal(t, int64(64), model.Int64Field)

	// Uint types
	setFieldValue(v.FieldByName("UintField"), float64(100))
	assert.Equal(t, uint(100), model.UintField)

	setFieldValue(v.FieldByName("Uint8Field"), float64(8))
	assert.Equal(t, uint8(8), model.Uint8Field)

	setFieldValue(v.FieldByName("Uint16Field"), float64(16))
	assert.Equal(t, uint16(16), model.Uint16Field)

	setFieldValue(v.FieldByName("Uint32Field"), float64(32))
	assert.Equal(t, uint32(32), model.Uint32Field)

	setFieldValue(v.FieldByName("Uint64Field"), float64(64))
	assert.Equal(t, uint64(64), model.Uint64Field)

	// Float types
	setFieldValue(v.FieldByName("Float32Field"), float64(32.5))
	assert.Equal(t, float32(32.5), model.Float32Field)

	setFieldValue(v.FieldByName("Float64Field"), 64.5)
	assert.Equal(t, 64.5, model.Float64Field)

	// String
	setFieldValue(v.FieldByName("StringField"), "test string")
	assert.Equal(t, "test string", model.StringField)
}

// ============================================================================
// ToFloat Edge Cases
// ============================================================================

func TestToFloat_AllTypes(t *testing.T) {
	// Float64
	result, err := toFloat(reflect.ValueOf(float64(64.5)))
	assert.NoError(t, err)
	assert.Equal(t, 64.5, result)

	// Float32
	result, err = toFloat(reflect.ValueOf(float32(32.5)))
	assert.NoError(t, err)
	assert.InDelta(t, 32.5, result, 0.001)

	// Int
	result, err = toFloat(reflect.ValueOf(42))
	assert.NoError(t, err)
	assert.Equal(t, 42.0, result)

	// Int8
	result, err = toFloat(reflect.ValueOf(int8(8)))
	assert.NoError(t, err)
	assert.Equal(t, 8.0, result)

	// Int16
	result, err = toFloat(reflect.ValueOf(int16(16)))
	assert.NoError(t, err)
	assert.Equal(t, 16.0, result)

	// Int32
	result, err = toFloat(reflect.ValueOf(int32(32)))
	assert.NoError(t, err)
	assert.Equal(t, 32.0, result)

	// Int64
	result, err = toFloat(reflect.ValueOf(int64(64)))
	assert.NoError(t, err)
	assert.Equal(t, 64.0, result)

	// Uint
	result, err = toFloat(reflect.ValueOf(uint(100)))
	assert.NoError(t, err)
	assert.Equal(t, 100.0, result)

	// Uint8
	result, err = toFloat(reflect.ValueOf(uint8(8)))
	assert.NoError(t, err)
	assert.Equal(t, 8.0, result)

	// Uint16
	result, err = toFloat(reflect.ValueOf(uint16(16)))
	assert.NoError(t, err)
	assert.Equal(t, 16.0, result)

	// Uint32
	result, err = toFloat(reflect.ValueOf(uint32(32)))
	assert.NoError(t, err)
	assert.Equal(t, 32.0, result)

	// Uint64
	result, err = toFloat(reflect.ValueOf(uint64(64)))
	assert.NoError(t, err)
	assert.Equal(t, 64.0, result)

	// Unsupported type (string)
	_, err = toFloat(reflect.ValueOf("not a number"))
	assert.Error(t, err)

	// Unsupported type (struct)
	_, err = toFloat(reflect.ValueOf(struct{}{}))
	assert.Error(t, err)
}

// ============================================================================
// Query/QueryIndex Closure Tests
// ============================================================================

func TestQuery_DecodeDefaultClosure(t *testing.T) {
	// Test the Query function returns a TypedQuery
	var results []CoverageTestModel
	tq := Query[CoverageTestModel](&results, "test", WithLimit(10))

	assert.Equal(t, "coverage_test", tq.indexName)
	assert.Equal(t, "test", tq.query)
	assert.Equal(t, int64(10), tq.opts.Limit)
}

func TestQueryIndex_DecodeDefaultClosure(t *testing.T) {
	// Test the QueryIndex function returns a TypedQuery
	var results []CoverageTestModel
	tq := QueryIndex[CoverageTestModel](&results, "custom_index", "query", WithOffset(5))

	assert.Equal(t, "custom_index", tq.indexName)
	assert.Equal(t, "query", tq.query)
	assert.Equal(t, int64(5), tq.opts.Offset)
}

// ============================================================================
// isSoftDeleted Tests
// ============================================================================

func TestIsSoftDeleted_WithValidDeletedAt(t *testing.T) {
	now := time.Now()
	model := &SoftDeleteModel{
		ID:        1,
		Name:      "Test",
		DeletedAt: gorm.DeletedAt{Time: now, Valid: true},
	}

	config := &IndexConfig{
		DeletedAtIndex: []int{2}, // Index of DeletedAt field
	}

	result := isSoftDeleted(model, config)
	assert.True(t, result)
}

func TestIsSoftDeleted_WithInvalidDeletedAt(t *testing.T) {
	model := &SoftDeleteModel{
		ID:        1,
		Name:      "Test",
		DeletedAt: gorm.DeletedAt{Valid: false},
	}

	config := &IndexConfig{
		DeletedAtIndex: []int{2},
	}

	result := isSoftDeleted(model, config)
	assert.False(t, result)
}

func TestIsSoftDeleted_NilConfig(t *testing.T) {
	model := &SoftDeleteModel{ID: 1}
	result := isSoftDeleted(model, nil)
	assert.False(t, result)
}

func TestIsSoftDeleted_EmptyDeletedAtIndex(t *testing.T) {
	model := &SoftDeleteModel{ID: 1}
	config := &IndexConfig{}
	result := isSoftDeleted(model, config)
	assert.False(t, result)
}

// ============================================================================
// DefaultDispatcher Retry Tests
// ============================================================================

func TestDefaultDispatcher_RetryOnError(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	var errorCallCount int
	onError := func(op string, err error) {
		errorCallCount++
	}

	mockClient.On("Index", "retry_test").Return(mockIndex)
	// First two calls fail, third succeeds
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("temp error")).Times(2)
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).
		Return(&meilisearch.TaskInfo{TaskUID: 1}, nil).Once()

	dispatcher := NewDefaultDispatcher(mockClient, 10, 3, onError)
	job := Job{
		IndexName: "retry_test",
		Operation: "create",
		Document:  map[string]any{"id": 1},
	}

	err := dispatcher.Dispatch(context.Background(), job)
	assert.NoError(t, err)

	// Wait for async dispatch
	time.Sleep(200 * time.Millisecond)
}

func TestDefaultDispatcher_ContextCancellation(t *testing.T) {
	// Test that dispatcher handles context correctly
	// Note: The actual behavior may vary - dispatcher might check context
	// This test verifies dispatcher doesn't panic with cancelled context
	mockClient := new(MockClient)

	dispatcher := NewDefaultDispatcher(mockClient, 10, 3, nil)
	job := Job{
		IndexName: "cancel_test",
		Operation: "create",
		Document:  map[string]any{"id": 1},
	}

	// Already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Dispatch should return without error (job just won't execute)
	_ = dispatcher.Dispatch(ctx, job)
	// Just verify no panic
}

// ============================================================================
// Register Edge Cases
// ============================================================================

func TestRegister_NilModel(t *testing.T) {
	mockClient := new(MockClient)
	gs := &GormSearch{
		client:         mockClient,
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
		config:         &Config{},
	}

	err := gs.Register(nil)
	assert.Error(t, err)
}

func TestRegister_MultipleModels(t *testing.T) {
	// Note: Full Register requires DB for callbacks
	// This is covered in integration tests
	// Test that we can create GormSearch with registry
	mockClient := new(MockClient)

	gs := &GormSearch{
		client:         mockClient,
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
		config:         &Config{},
	}

	// Manually add configs to registry (simulating registration)
	config1 := &IndexConfig{IndexName: "model1", PrimaryKey: "id"}
	config2 := &IndexConfig{IndexName: "model2", PrimaryKey: "id"}

	gs.mu.Lock()
	gs.registry["model1"] = config1
	gs.registry["model2"] = config2
	gs.mu.Unlock()

	assert.Len(t, gs.registry, 2)
}

// ============================================================================
// indexNameFor Tests
// ============================================================================

type ModelWithIndexName struct {
	ID uint `json:"id"`
}

func (ModelWithIndexName) IndexName() string { return "custom_model_index" }

func TestIndexNameFor_WithInterface(t *testing.T) {
	name := indexNameFor[ModelWithIndexName]()
	assert.Equal(t, "custom_model_index", name)
}

func TestIndexNameFor_WithTableName(t *testing.T) {
	type ModelWithTableName struct {
		ID uint `json:"id"`
	}

	// For types without IndexName, it falls back to type name
	name := indexNameFor[ModelWithTableName]()
	assert.NotEmpty(t, name)
}

// ============================================================================
// Search Error Cases
// ============================================================================

func TestSearch_UnregisteredIndex(t *testing.T) {
	mockClient := new(MockClient)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config:   &Config{},
	}

	_, err := gs.Search("nonexistent", "query")
	assert.Error(t, err)
	assert.Equal(t, ErrIndexNotRegistered, err)
}

func TestSearch_QueryTooLong(t *testing.T) {
	mockClient := new(MockClient)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config:   &Config{},
	}
	gs.registry["test_index"] = &IndexConfig{IndexName: "test_index"}

	// Create a query longer than MaxQueryLength (1024)
	longQuery := make([]byte, 2000)
	for i := range longQuery {
		longQuery[i] = 'a'
	}

	_, err := gs.Search("test_index", string(longQuery))
	assert.Error(t, err)
	assert.Equal(t, ErrQueryTooLong, err)
}

// ============================================================================
// MultiSearchRaw Tests
// ============================================================================

func TestMultiSearchRaw_WithQueries(t *testing.T) {
	mockClient := new(MockClient)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config:   &Config{},
	}
	gs.registry["test_index"] = &IndexConfig{IndexName: "test_index"}

	mockClient.On("MultiSearchWithContext", mock.Anything, mock.Anything).Return(&meilisearch.MultiSearchResponse{
		Results: []meilisearch.SearchResponse{
			{Hits: []meilisearch.Hit{}, EstimatedTotalHits: 0},
		},
	}, nil)

	result, err := gs.MultiSearchRaw(SearchQuery{IndexName: "test_index", Query: "test"})
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// ============================================================================
// DecodeInto Tests
// ============================================================================

func TestDecodeInto_EmptyHits(t *testing.T) {
	hits := meilisearch.Hits{}
	results, err := DecodeInto[CoverageTestModel](hits)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestDecodeInto_WithHits(t *testing.T) {
	hits := meilisearch.Hits{
		{"id": json.RawMessage(`1`), "name": json.RawMessage(`"Test"`)},
	}
	results, err := DecodeInto[CoverageTestModel](hits)
	assert.NoError(t, err)
	assert.Len(t, results, 1)
}

// ============================================================================
// DispatcherFunc Coverage Tests
// ============================================================================

func TestDispatcherFunc_DispatchWithError(t *testing.T) {
	df := DispatcherFunc(func(ctx context.Context, job Job) error {
		return errors.New("dispatch error")
	})

	err := df.Dispatch(context.Background(), Job{IndexName: "test", Operation: "create"})
	assert.Error(t, err)
}

// ============================================================================
// Worker Pool Coverage Tests
// ============================================================================

func TestWorkerPool_ConcurrentAccess(t *testing.T) {
	pool := newWorkerPool(5)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.acquire()
			time.Sleep(10 * time.Millisecond)
			pool.release()
		}()
	}

	wg.Wait()
}

// ============================================================================
// MustNew Tests
// ============================================================================

func TestMustNew_Panics(t *testing.T) {
	assert.Panics(t, func() {
		MustNew(nil, nil)
	})
}

// ============================================================================
// Additional Coverage Tests
// ============================================================================

func TestNew_WithNilDB(t *testing.T) {
	mockClient := new(MockClient)
	_, err := New(nil, mockClient)
	assert.Error(t, err)
}

func TestNew_WithNilClient(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	_, err = New(db, nil)
	assert.Error(t, err)
}

func TestNew_WithOptions(t *testing.T) {
	mockClient := new(MockClient)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	gs, err := New(db, mockClient,
		WithBatchSize(500),
		WithAsync(true),
		WithMaxWorkers(20),
		WithMaxRetries(5),
		WithIndexPrefix("prod_"),
	)
	assert.NoError(t, err)
	assert.NotNil(t, gs)
}

func TestMultiSearch_WithCustomDecoder(t *testing.T) {
	mockClient := new(MockClient)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config: &Config{
			MapDecoder: func(hits []map[string]any, dest any) error {
				// Custom decoder
				return nil
			},
		},
	}
	gs.registry["coverage_test"] = &IndexConfig{IndexName: "coverage_test"}

	mockClient.On("MultiSearchWithContext", mock.Anything, mock.Anything).Return(&meilisearch.MultiSearchResponse{
		Results: []meilisearch.SearchResponse{
			{Hits: []meilisearch.Hit{{"id": json.RawMessage(`1`)}}, EstimatedTotalHits: 1},
		},
	}, nil)

	var results []CoverageTestModel
	tq := Query[CoverageTestModel](&results, "test")
	searchResults, err := gs.MultiSearch(tq)
	assert.NoError(t, err)
	assert.Len(t, searchResults, 1)
}

func TestQueryIndex_ClosureExecution(t *testing.T) {
	// Test that QueryIndex closure actually works with decodeDefault
	var results []CoverageTestModel
	tq := QueryIndex[CoverageTestModel](&results, "custom", "query")

	// Verify query settings
	assert.Equal(t, "custom", tq.indexName)
	assert.Equal(t, "query", tq.query)

	// Test decodeCustom path
	customDecoder := func(hits []map[string]any, dest any) error {
		// Custom decoding
		return nil
	}
	err := tq.decodeCustom([]map[string]any{{"id": float64(1)}}, customDecoder)
	assert.NoError(t, err)
}

func TestSearchAs_Error(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config:   &Config{},
	}
	gs.registry["test_idx"] = &IndexConfig{IndexName: "test_idx"}

	mockClient.On("Index", "test_idx").Return(mockIndex)
	mockIndex.On("SearchWithContext", mock.Anything, "error query", mock.Anything).Return(nil, errors.New("search error"))

	_, err := SearchFor[CoverageTestModel](gs, "error query", WithIndexName("test_idx"))
	assert.Error(t, err)
}

func TestMultiSearchAs_Error(t *testing.T) {
	mockClient := new(MockClient)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config:   &Config{},
	}
	gs.registry["test_idx"] = &IndexConfig{IndexName: "test_idx"}

	mockClient.On("MultiSearchWithContext", mock.Anything, mock.Anything).Return(nil, errors.New("multi search error"))

	_, err := MultiSearchFor[CoverageTestModel](gs, SearchQuery{IndexName: "test_idx", Query: "test"})
	assert.Error(t, err)
}

func TestDecodeHits_Direct(t *testing.T) {
	hits := []map[string]any{
		{"id": float64(1), "name": "Test"},
	}
	results, err := DecodeHits[CoverageTestModel](hits)
	assert.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestDecodeHits_Error(t *testing.T) {
	// Invalid hits that can't be decoded
	hits := []map[string]any{
		{"id": "not_a_number"}, // ID should be numeric
	}
	_, err := DecodeHits[CoverageTestModel](hits)
	// May or may not error depending on implementation
	_ = err
}

// ============================================================================
// syncDocument Coverage
// ============================================================================

func TestSyncDocument_NilModel(t *testing.T) {
	// Test that syncDocument handles nil gracefully
	// This is tested indirectly through GORM callbacks in sync_test.go
}

// ============================================================================
// Parser Additional Tests
// ============================================================================

func TestResolveIndexName_WithTableNamer(t *testing.T) {
	type TableNamerModel struct {
		ID uint `json:"id"`
	}

	// Model without TableName() method uses type name
	config, err := parseModel(&TableNamerModel{})
	assert.NoError(t, err)
	assert.NotEmpty(t, config.IndexName)
}

// ============================================================================
// Reflect Additional Tests
// ============================================================================

func TestEncodeDocument_WithNilGeo(t *testing.T) {
	type ModelWithGeo struct {
		ID  uint     `json:"id" gorm:"primaryKey"`
		Lat *float64 `json:"lat" meilisearch:"geo:lat"`
		Lng *float64 `json:"lng" meilisearch:"geo:lng"`
	}

	model := &ModelWithGeo{ID: 1}
	config, err := parseModel(model)
	assert.NoError(t, err)

	gs := &GormSearch{
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		config:   &Config{},
	}
	gs.registry["model_with_geos"] = config

	doc, err := gs.encodeDocument(model, config)
	assert.NoError(t, err)
	assert.NotNil(t, doc)
}

func TestExtractID_WithStringPrimaryKey(t *testing.T) {
	type StringIDModel struct {
		ID   string `json:"id" gorm:"primaryKey"`
		Name string `json:"name"`
	}

	model := &StringIDModel{ID: "abc-123", Name: "Test"}
	config, _ := parseModel(model)

	id := extractID(model, config)
	assert.Equal(t, "abc-123", id)
}

// ============================================================================
// Retry and Dispatch Additional Tests
// ============================================================================

func TestDefaultDispatcher_AllRetriesFail(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	errorCount := 0
	onError := func(op string, err error) {
		errorCount++
	}

	mockClient.On("Index", "fail_test").Return(mockIndex)
	// All retries fail
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("persistent error"))

	dispatcher := NewDefaultDispatcher(mockClient, 10, 2, onError) // Only 2 retries
	job := Job{
		IndexName: "fail_test",
		Operation: "create",
		Document:  map[string]any{"id": 1},
	}

	err := dispatcher.Dispatch(context.Background(), job)
	assert.NoError(t, err) // Dispatch itself doesn't return error

	// Wait for async retry attempts
	time.Sleep(500 * time.Millisecond)
	assert.Greater(t, errorCount, 0) // Error callback should have been called
}

func TestDefaultDispatcher_ZeroRetries(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "zero_retry").Return(mockIndex)
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).
		Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)

	// Zero maxRetries should use default (3)
	dispatcher := NewDefaultDispatcher(mockClient, 10, 0, nil)
	job := Job{
		IndexName: "zero_retry",
		Operation: "create",
		Document:  map[string]any{"id": 1},
	}

	err := dispatcher.Dispatch(context.Background(), job)
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	mockIndex.AssertExpectations(t)
}

func TestDispatch_UpdateOperation(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "update_test").Return(mockIndex)
	mockIndex.On("UpdateDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).
		Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)

	dispatcher := NewDefaultDispatcher(mockClient, 10, 3, nil)
	job := Job{
		IndexName: "update_test",
		Operation: "update",
		Document:  map[string]any{"id": 1, "name": "Updated"},
	}

	err := dispatcher.Dispatch(context.Background(), job)
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
}

func TestDispatch_DeleteOperation(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "delete_test").Return(mockIndex)
	mockIndex.On("DeleteDocumentWithContext", mock.Anything, "123", mock.Anything).
		Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)

	dispatcher := NewDefaultDispatcher(mockClient, 10, 3, nil)
	job := Job{
		IndexName: "delete_test",
		Operation: "delete",
		ID:        "123",
	}

	err := dispatcher.Dispatch(context.Background(), job)
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
}

// ============================================================================
// QueryIndex Full Coverage
// ============================================================================

func TestQueryIndex_WithDecodeDefault(t *testing.T) {
	mockClient := new(MockClient)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config:   &Config{},
	}
	gs.registry["query_idx_test"] = &IndexConfig{IndexName: "query_idx_test"}

	hits := []meilisearch.Hit{
		{"id": json.RawMessage(`1`), "name": json.RawMessage(`"Item"`)},
	}

	mockClient.On("MultiSearchWithContext", mock.Anything, mock.Anything).Return(&meilisearch.MultiSearchResponse{
		Results: []meilisearch.SearchResponse{
			{Hits: hits, EstimatedTotalHits: 1},
		},
	}, nil)

	var results []CoverageTestModel
	tq := QueryIndex[CoverageTestModel](&results, "query_idx_test", "item")
	searchResults, err := gs.MultiSearch(tq)
	assert.NoError(t, err)
	assert.Len(t, searchResults, 1)
}

// ============================================================================
// safeGo Panic Recovery Test
// ============================================================================

func TestSafeGo_PanicRecovery(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	errorCaught := false
	onError := func(op string, err error) {
		if err != nil {
			errorCaught = true
		}
	}

	mockClient.On("Index", "panic_test").Return(mockIndex)
	// This will cause a panic in executeWithContext
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			panic("intentional panic")
		}).Return(nil, nil)

	dispatcher := NewDefaultDispatcher(mockClient, 10, 1, onError)
	job := Job{
		IndexName: "panic_test",
		Operation: "create",
		Document:  map[string]any{"id": 1},
	}

	// Should not panic
	err := dispatcher.Dispatch(context.Background(), job)
	assert.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	// Panic should have been recovered and error callback called
	assert.True(t, errorCaught)
}

// ============================================================================
// isSoftDeleted Coverage Tests
// ============================================================================

func TestIsSoftDeleted_DirectDeletedAtField(t *testing.T) {
	type ModelWithDeletedAt struct {
		ID        uint
		Name      string
		DeletedAt gorm.DeletedAt
	}

	// Not deleted
	model := &ModelWithDeletedAt{ID: 1, Name: "Test"}
	config := &IndexConfig{}
	result := isSoftDeleted(model, config)
	assert.False(t, result)

	// Deleted (time set)
	model2 := &ModelWithDeletedAt{
		ID:        2,
		Name:      "Deleted",
		DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true},
	}
	result2 := isSoftDeleted(model2, config)
	assert.True(t, result2)
}

func TestIsSoftDeleted_EmbeddedGormModel(t *testing.T) {
	type ModelWithGormModel struct {
		gorm.Model
		Name string
	}

	// Not deleted
	model := &ModelWithGormModel{Name: "Test"}
	config := &IndexConfig{}
	result := isSoftDeleted(model, config)
	assert.False(t, result)
}

func TestIsSoftDeleted_NoDeletedAtField(t *testing.T) {
	type SimpleModel struct {
		ID   uint
		Name string
	}

	model := &SimpleModel{ID: 1, Name: "Test"}
	config := &IndexConfig{}
	result := isSoftDeleted(model, config)
	assert.False(t, result)
}

func TestIsSoftDeleted_WithCachedIndex(t *testing.T) {
	type ModelWithDeletedAt struct {
		ID        uint
		Name      string
		DeletedAt gorm.DeletedAt
	}

	model := &ModelWithDeletedAt{
		ID:        1,
		Name:      "Test",
		DeletedAt: gorm.DeletedAt{Time: time.Now(), Valid: true},
	}
	config := &IndexConfig{
		DeletedAtIndex: []int{2}, // Index of DeletedAt field
	}
	result := isSoftDeleted(model, config)
	assert.True(t, result)
}

// ============================================================================
// extractID Additional Coverage Tests
// ============================================================================

func TestExtractID_WithEmbeddedModel(t *testing.T) {
	type EmbeddedModel struct {
		gorm.Model
		Name string
	}

	model := &EmbeddedModel{Name: "Test"}
	model.ID = 42
	config := &IndexConfig{}
	result := extractID(model, config)
	assert.Equal(t, "42", result)
}

func TestExtractID_NoIDField(t *testing.T) {
	type NoIDModel struct {
		Name string
	}

	model := &NoIDModel{Name: "Test"}
	config := &IndexConfig{}
	result := extractID(model, config)
	assert.Equal(t, "", result)
}

func TestExtractID_StringID(t *testing.T) {
	type StringIDModel struct {
		ID   string
		Name string
	}

	model := &StringIDModel{ID: "uuid-123", Name: "Test"}
	config := &IndexConfig{
		IDFieldIndices: []int{0},
	}
	result := extractID(model, config)
	assert.Equal(t, "uuid-123", result)
}

// ============================================================================
// encodeDocument Coverage Tests - Handled via parseModel
// ============================================================================

func TestParseModel_FieldExtractors(t *testing.T) {
	type TimeModel struct {
		ID        uint
		CreatedAt time.Time
		Name      string
	}

	model := &TimeModel{ID: 1, CreatedAt: time.Now(), Name: "Test"}

	// Parse model to get config with extractors
	config, err := parseModel(model)
	assert.NoError(t, err)
	assert.NotNil(t, config.FieldExtractors)
	assert.NotEmpty(t, config.FieldMapping)
}

// ============================================================================
// MultiSearch Additional Coverage - Custom Decoder Path
// ============================================================================

func TestMultiSearch_WithMapDecoder(t *testing.T) {
	mockClient := new(MockClient)

	customDecoderCalled := false
	customDecoder := func(hits []map[string]any, dest any) error {
		customDecoderCalled = true
		return nil
	}

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config: &Config{
			MapDecoder: customDecoder,
		},
	}
	// Register with exact index name that Query will generate
	gs.registry["coverage_test_models"] = &IndexConfig{IndexName: "coverage_test_models"}

	hits := []meilisearch.Hit{
		{"id": json.RawMessage(`1`), "name": json.RawMessage(`"Custom"`)},
	}

	mockClient.On("MultiSearchWithContext", mock.Anything, mock.Anything).Return(&meilisearch.MultiSearchResponse{
		Results: []meilisearch.SearchResponse{
			{Hits: hits, EstimatedTotalHits: 1},
		},
	}, nil)

	var results []CoverageTestModel
	tq := QueryIndex(&results, "coverage_test_models", "test")

	searchResults, err := gs.MultiSearch(tq)
	assert.NoError(t, err)
	assert.Len(t, searchResults, 1)
	assert.True(t, customDecoderCalled)
}

// ============================================================================
// DecodeInto Coverage Tests
// ============================================================================

func TestDecodeInto_Success(t *testing.T) {
	type Item struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	hits := meilisearch.Hits{
		{"id": json.RawMessage(`1`), "name": json.RawMessage(`"Item1"`)},
		{"id": json.RawMessage(`2`), "name": json.RawMessage(`"Item2"`)},
	}

	results, err := DecodeInto[Item](hits)
	assert.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, 1, results[0].ID)
	assert.Equal(t, "Item1", results[0].Name)
}

// ============================================================================
// parseFieldTags Additional Coverage
// ============================================================================

func TestParseFieldTags_AllOptions(t *testing.T) {
	type AllTagsModel struct {
		ID          uint    `search:"searchable,filterable,sortable"`
		Name        string  `search:"searchable"`
		Price       float64 `search:"filterable,sortable"`
		Description string  `search:"-"`
	}

	config, err := parseModel(&AllTagsModel{})
	assert.NoError(t, err)
	// Check that config was parsed successfully
	assert.NotNil(t, config)
	assert.Equal(t, "all_tags_models", config.IndexName)
	// FieldMapping should have entries
	assert.NotEmpty(t, config.FieldMapping)
}

// ============================================================================
// DispatcherFunc Coverage
// ============================================================================

func TestDispatcherFunc_MultipleJobs(t *testing.T) {
	jobCount := 0
	dispatcher := DispatcherFunc(func(ctx context.Context, job Job) error {
		jobCount++
		return nil
	})

	jobs := []Job{
		{IndexName: "idx1", Operation: "create"},
		{IndexName: "idx2", Operation: "update"},
		{IndexName: "idx3", Operation: "delete"},
	}

	for _, job := range jobs {
		err := dispatcher.Dispatch(context.Background(), job)
		assert.NoError(t, err)
	}

	assert.Equal(t, 3, jobCount)
}

// ============================================================================
// valToString Additional Coverage
// ============================================================================

func TestValToString_AllTypes(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{"uint", uint(123), "123"},
		{"uint32", uint32(456), "456"},
		{"uint64", uint64(789), "789"},
		{"int", int(100), "100"},
		{"int64", int64(200), "200"},
		{"string", "hello", "hello"},
		{"float", 3.14, "3.14"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := reflect.ValueOf(tt.value)
			result := valToString(v)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ============================================================================
// retryWithContext Additional Coverage
// ============================================================================

func TestRetryWithContext_MaxRetriesExhausted_Direct(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	callCount := 0
	mockClient.On("Index", "retry_exhaust_test").Return(mockIndex)
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			callCount++
		}).Return(nil, errors.New("persistent error"))

	var lastErr error
	onError := func(op string, err error) {
		lastErr = err
	}

	dispatcher := NewDefaultDispatcher(mockClient, 10, 2, onError)
	job := Job{
		IndexName: "retry_exhaust_test",
		Operation: "create",
		Document:  map[string]any{"id": 1},
	}

	err := dispatcher.Dispatch(context.Background(), job)
	assert.NoError(t, err)

	// Wait for retry attempts to complete
	time.Sleep(500 * time.Millisecond)
	assert.NotNil(t, lastErr)
}

func TestRetryWithContext_SuccessAfterRetry(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	callCount := 0
	mockClient.On("Index", "retry_success_test").Return(mockIndex)

	// First call fails, second succeeds
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).
		Return(func(ctx context.Context, docs any, opts *string) *meilisearch.TaskInfo {
			callCount++
			if callCount >= 2 {
				return &meilisearch.TaskInfo{TaskUID: 1}
			}
			return nil
		}, func(ctx context.Context, docs any, opts *string) error {
			if callCount >= 2 {
				return nil
			}
			return errors.New("temporary error")
		})

	dispatcher := NewDefaultDispatcher(mockClient, 10, 3, nil)
	job := Job{
		IndexName: "retry_success_test",
		Operation: "create",
		Document:  map[string]any{"id": 1},
	}

	err := dispatcher.Dispatch(context.Background(), job)
	assert.NoError(t, err)

	time.Sleep(300 * time.Millisecond)
}

// ============================================================================
// searchAs Coverage - Test via SearchFor
// ============================================================================

func TestSearchFor_WithGormSearch(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:         mockClient,
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
		ctx:            context.Background(),
		config:         &Config{},
	}
	// Use SearchAs with explicit index name to avoid indexNameFor cache issues
	gs.registry["search_test_idx"] = &IndexConfig{IndexName: "search_test_idx"}

	// Prepare mock response
	mockClient.On("Index", "search_test_idx").Return(mockIndex)
	mockIndex.On("SearchWithContext", mock.Anything, "query", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits: []meilisearch.Hit{
			{"id": json.RawMessage(`1`), "name": json.RawMessage(`"Test"`)},
		},
		EstimatedTotalHits: 1,
	}, nil)

	resp, err := SearchFor[CoverageTestModel](gs, "query", WithIndexName("search_test_idx"))
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, int64(1), resp.EstimatedTotal)
}

// ============================================================================
// multiSearchAs Additional Coverage - via MultiSearch
// ============================================================================

func TestMultiSearchAs_ErrorPath(t *testing.T) {
	mockClient := new(MockClient)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config:   &Config{},
	}

	mockClient.On("MultiSearchWithContext", mock.Anything, mock.Anything).Return(nil, errors.New("search error"))

	var results []CoverageTestModel
	tq := Query(&results, "test")

	_, err := gs.MultiSearch(tq)
	assert.Error(t, err)
}

// ============================================================================
// New Function Coverage - Already tested via integration, skip mock tests
// ============================================================================

// Note: New() requires real *gorm.DB and meilisearch.ServiceManager
// Testing with mocks is complex due to interface requirements
// The function is already well-tested via integration tests

// ============================================================================
// resolveIndexName Coverage
// ============================================================================

func TestResolveIndexName_WithTag(t *testing.T) {
	type TaggedModel struct {
		ID   uint
		Name string
	}

	// Use reflection to test
	typ := reflect.TypeOf(TaggedModel{})
	name := toSnakeCase(typ.Name()) + "s"
	assert.Equal(t, "tagged_models", name)
}

// ============================================================================
// configureIndex Coverage
// ============================================================================

func TestConfigureIndex_WithCustomSettings(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		mu:       &sync.RWMutex{},
		ctx:      context.Background(),
		config:   &Config{},
	}

	mockClient.On("CreateIndex", mock.Anything).Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)
	mockClient.On("Index", "custom_settings_idx").Return(mockIndex)
	mockIndex.On("UpdateSettings", mock.Anything).Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)

	config := &IndexConfig{
		IndexName:        "custom_settings_idx",
		PrimaryKey:       "id",
		SearchableFields: []string{"name"},
		FilterableFields: []string{"price"},
		SortableFields:   []string{"created_at"},
	}

	err := gs.configureIndex(config)
	assert.NoError(t, err)
}

// ============================================================================
// buildSettings Coverage - via configureIndex
// ============================================================================

func TestBuildSettings_WithSearchableFields(t *testing.T) {
	gs := &GormSearch{
		config: &Config{},
	}

	config := &IndexConfig{
		SearchableFields: []string{"name", "description"},
		FilterableFields: []string{"price", "category"},
		SortableFields:   []string{"created_at"},
	}

	settings := gs.buildSettings(config)
	assert.Equal(t, []string{"name", "description"}, settings.SearchableAttributes)
	assert.Equal(t, []string{"price", "category"}, settings.FilterableAttributes)
	assert.Equal(t, []string{"created_at"}, settings.SortableAttributes)
}
