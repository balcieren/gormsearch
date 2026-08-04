package gormsearch

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meilisearch/meilisearch-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ============================================================================
// Bug Fix: Geo decode traversed the root struct instead of the geo field
// ============================================================================

type GeoLocation struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type GeoPlace struct {
	ID       uint        `json:"id"`
	Name     string      `json:"name"`
	Location GeoLocation `json:"location" meili:"geo"`
}

func TestBugFix_GeoDecodeRoundTrip(t *testing.T) {
	gs := &GormSearch{
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
		config:         &Config{},
	}

	config, err := parseModel(&GeoPlace{})
	assert.NoError(t, err)
	gs.registry[config.IndexName] = config
	gs.registryByType[config.ModelType] = config

	// Encode
	original := &GeoPlace{
		ID:       7,
		Name:     "NYC",
		Location: GeoLocation{Lat: 40.7128, Lng: -74.0060},
	}
	doc := gs.toDocument(original, config)

	geo, ok := doc["_geo"].(map[string]float64)
	assert.True(t, ok, "_geo should be a map[string]float64")
	assert.Equal(t, 40.7128, geo["lat"])
	assert.Equal(t, -74.0060, geo["lng"])

	// Decode (convertHits produces map[string]any with float64 numbers)
	hits := convertHits(meilisearch.Hits{
		{
			"id":   json.RawMessage(`7`),
			"name": json.RawMessage(`"NYC"`),
			"_geo": json.RawMessage(`{"lat":40.7128,"lng":-74.0060}`),
		},
	})

	var decoded GeoPlace
	err = gs.decodeDocument(hits[0], &decoded)
	assert.NoError(t, err)
	assert.Equal(t, uint(7), decoded.ID)
	assert.Equal(t, "NYC", decoded.Name)
	assert.Equal(t, 40.7128, decoded.Location.Lat, "geo lat must be decoded into the geo struct")
	assert.Equal(t, -74.0060, decoded.Location.Lng, "geo lng must be decoded into the geo struct")
}

// ============================================================================
// Bug Fix: Fast decoder silently dropped slices, maps and nested structs
// ============================================================================

type NestedValue struct {
	Value int `json:"value"`
}

type ComplexModel struct {
	ID     uint              `json:"id"`
	Name   string            `json:"name"`
	Tags   []string          `json:"tags" meili:"filterable"`
	Scores []float64         `json:"scores"`
	Meta   map[string]string `json:"meta"`
	Nested NestedValue       `json:"nested"`
}

func TestBugFix_DecodeComplexFields(t *testing.T) {
	gs := &GormSearch{
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
		config:         &Config{},
	}

	config, err := parseModel(&ComplexModel{})
	assert.NoError(t, err)
	gs.registry[config.IndexName] = config
	gs.registryByType[config.ModelType] = config

	doc := map[string]any{
		"id":     float64(1),
		"name":   "test",
		"tags":   []any{"a", "b", "c"},
		"scores": []any{1.5, 2.5},
		"meta":   map[string]any{"key": "val"},
		"nested": map[string]any{"value": float64(42)},
	}

	var decoded ComplexModel
	err = gs.decodeDocument(doc, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, decoded.Tags, "slice fields must not be dropped")
	assert.Equal(t, []float64{1.5, 2.5}, decoded.Scores)
	assert.Equal(t, map[string]string{"key": "val"}, decoded.Meta)
	assert.Equal(t, 42, decoded.Nested.Value, "nested structs must not be dropped")
}

// ============================================================================
// Bug Fix: Parser panicked on embedded pointer structs (*gorm.Model)
// ============================================================================

type PtrEmbedProduct struct {
	*gorm.Model
	Name string `json:"name" meili:"searchable"`
}

type SKU string

type NonStructEmbedProduct struct {
	ID  uint `json:"id"`
	SKU      // embedded non-struct type
}

func TestBugFix_ParseModelWithPointerEmbed(t *testing.T) {
	assert.NotPanics(t, func() {
		config, err := parseModel(&PtrEmbedProduct{})
		assert.NoError(t, err)
		assert.NotNil(t, config)
		assert.NotEmpty(t, config.IDFieldIndices, "ID from *gorm.Model embed should be found")
		assert.NotEmpty(t, config.DeletedAtIndex, "DeletedAt from *gorm.Model embed should be found")

		// extractID works with populated embed
		model := &PtrEmbedProduct{Model: &gorm.Model{ID: 42}, Name: "x"}
		assert.Equal(t, "42", extractID(model, config))

		// nil embed must not panic
		gs := &GormSearch{config: &Config{}}
		nilModel := &PtrEmbedProduct{}
		assert.Equal(t, "", extractID(nilModel, config))
		assert.NotPanics(t, func() {
			gs.toDocument(nilModel, config)
		})
		assert.False(t, isSoftDeleted(nilModel, config))
	})
}

func TestBugFix_ParseModelWithNonStructEmbed(t *testing.T) {
	assert.NotPanics(t, func() {
		config, err := parseModel(&NonStructEmbedProduct{})
		assert.NoError(t, err)
		_, ok := config.FieldMapping["SKU"]
		assert.True(t, ok, "embedded non-struct type should be treated as a regular field")
	})
}

// ============================================================================
// Bug Fix: MultiSearch ignored IndexPrefix (Query used bare indexNameFor)
// ============================================================================

type PrefixProduct struct {
	ID   uint   `json:"id"`
	Name string `json:"name" meili:"searchable"`
}

func (PrefixProduct) IndexName() string { return "prefix_products" }

func TestBugFix_MultiSearchWithIndexPrefix(t *testing.T) {
	mockClient := new(MockClient)

	gs := &GormSearch{
		client:         mockClient,
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
		ctx:            context.Background(),
		config:         &Config{IndexPrefix: "prod_"},
	}

	config, err := parseModel(&PrefixProduct{})
	assert.NoError(t, err)
	config.IndexName = "prod_" + config.IndexName // What Register() does
	gs.registry[config.IndexName] = config
	gs.registryByType[config.ModelType] = config

	var capturedReq *meilisearch.MultiSearchRequest
	mockClient.On("MultiSearchWithContext", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			capturedReq = args.Get(1).(*meilisearch.MultiSearchRequest)
		}).
		Return(&meilisearch.MultiSearchResponse{
			Results: []meilisearch.SearchResponse{
				{Hits: []meilisearch.Hit{{"id": json.RawMessage(`1`)}}, EstimatedTotalHits: 1},
			},
		}, nil)

	var results []PrefixProduct
	searchResults, err := gs.MultiSearch(Query(&results, "test"))
	assert.NoError(t, err, "MultiSearch must resolve the prefixed index name")
	assert.Len(t, searchResults.Results, 1)

	assert.NotNil(t, capturedReq)
	assert.Len(t, capturedReq.Queries, 1)
	assert.Equal(t, "prod_prefix_products", capturedReq.Queries[0].IndexUID)
}

// ============================================================================
// Bug Fix: WithAsync(false) was a no-op
// ============================================================================

func TestBugFix_AsyncOptionPropagatesToDispatcher(t *testing.T) {
	mockClient := new(MockClient)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// Default: async enabled
	gs, err := New(db, mockClient)
	assert.NoError(t, err)
	dd, ok := gs.config.Dispatcher.(*DefaultDispatcher)
	assert.True(t, ok)
	assert.True(t, dd.async, "dispatcher should be async by default")

	// WithAsync(false): sync mode
	gs2, err := New(db, mockClient, WithAsync(false))
	assert.NoError(t, err)
	dd2, ok := gs2.config.Dispatcher.(*DefaultDispatcher)
	assert.True(t, ok)
	assert.False(t, dd2.async, "WithAsync(false) must disable async dispatch")
}

func TestBugFix_SyncDispatchExecutesInline(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	mockClient.On("Index", "sync_idx").Return(mockIndex)
	var called int32
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			atomic.StoreInt32(&called, 1)
		}).
		Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)

	dispatcher := NewDefaultDispatcher(mockClient, 10, 1, nil)
	dispatcher.async = false

	job := Job{IndexName: "sync_idx", Operation: "create", Document: map[string]any{"id": 1}}
	err := dispatcher.Dispatch(context.Background(), job)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&called),
		"sync dispatch must execute before Dispatch returns (no sleep needed)")

	// Sync mode must surface errors to the caller
	errIdx := new(MockIndex)
	mockClient.On("Index", "err_idx").Return(errIdx)
	errIdx.On("DeleteDocumentWithContext", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, assert.AnError)

	err = dispatcher.Dispatch(context.Background(), Job{IndexName: "err_idx", Operation: "delete", ID: "1"})
	assert.Error(t, err, "sync dispatch must return execution errors")
}

// ============================================================================
// Bug Fix: indexNameFor panicked for T = *Product
// ============================================================================

type PtrReceiverModel struct {
	ID uint `json:"id"`
}

func (m *PtrReceiverModel) TableName() string { return "ptr_receiver" }

func TestBugFix_IndexNameForPointerType(t *testing.T) {
	assert.NotPanics(t, func() {
		name := indexNameFor[*PtrReceiverModel]()
		assert.Equal(t, "ptr_receiver", name,
			"pointer-receiver TableName must be detected even for T = *Model")
	})
}

// ============================================================================
// Bug Fix: Searcher.Search could mutate the caller's options backing array
// ============================================================================

func TestBugFix_SearcherSearchDoesNotMutateOpts(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	gs := &GormSearch{
		client:         mockClient,
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
		ctx:            context.Background(),
	}
	gs.registry["products"] = &IndexConfig{IndexName: "products"}

	mockClient.On("Index", "products").Return(mockIndex)
	mockIndex.On("SearchWithContext", mock.Anything, "q", mock.Anything).
		Return(&meilisearch.SearchResponse{}, nil)

	searcher := Of[GenericTestProduct](gs)

	// Options slice with spare capacity and a sentinel in the backing array
	opts := make([]SearchOption, 1, 2)
	opts[0] = WithLimit(5)
	sentinel := WithLimit(99)
	backing := opts[:2]
	backing[1] = sentinel

	_, err := searcher.Search("q", opts...)
	assert.NoError(t, err)

	// The sentinel must not have been overwritten by append
	var applied SearchOptions
	backing[1](&applied)
	assert.Equal(t, int64(99), applied.Limit,
		"Searcher.Search must not write into the caller's options backing array")
}

// ============================================================================
// Performance Fix: Dispatcher backpressure bounds in-flight jobs
// ============================================================================

func TestBugFix_DispatcherBackpressure(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	release := make(chan struct{})
	mockClient.On("Index", "bp_idx").Return(mockIndex)
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			<-release // Block until the test releases the worker
		}).
		Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)

	// Pool of 1: the first job occupies the only worker slot
	dispatcher := NewDefaultDispatcher(mockClient, 1, 1, nil)

	job := Job{IndexName: "bp_idx", Operation: "create", Document: map[string]any{"id": 1}}

	err := dispatcher.Dispatch(context.Background(), job)
	assert.NoError(t, err, "first Dispatch acquires the only slot and returns")

	// Wait for the first job to actually start executing
	time.Sleep(50 * time.Millisecond)

	// Second Dispatch must block: the pool is exhausted and the goroutine
	// is only spawned after a slot frees up.
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- dispatcher.Dispatch(context.Background(), job)
	}()

	select {
	case <-secondDone:
		t.Fatal("second Dispatch returned while all workers were busy (no backpressure)")
	case <-time.After(100 * time.Millisecond):
		// Expected: still blocked
	}

	// Release both jobs; the second Dispatch must now proceed
	close(release)

	select {
	case err := <-secondDone:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("second Dispatch did not proceed after a worker slot freed up")
	}
}

func TestBugFix_DispatcherBackpressureRespectsContext(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	release := make(chan struct{})
	defer close(release)
	mockClient.On("Index", "bp_ctx").Return(mockIndex)
	mockIndex.On("AddDocumentsWithContext", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			<-release
		}).
		Return(&meilisearch.TaskInfo{TaskUID: 1}, nil)

	dispatcher := NewDefaultDispatcher(mockClient, 1, 1, nil)
	job := Job{IndexName: "bp_ctx", Operation: "create", Document: map[string]any{"id": 1}}

	assert.NoError(t, dispatcher.Dispatch(context.Background(), job))
	time.Sleep(50 * time.Millisecond)

	// Cancelled context while waiting for a slot -> error instead of blocking forever
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := dispatcher.Dispatch(ctx, job)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
