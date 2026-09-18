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
	"gorm.io/gorm/logger"
)

// ============================================================================
// Hook regression models & harness
// ============================================================================

type RegProduct struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Name      string         `json:"name" meili:"searchable"`
	Price     float64        `json:"price" meili:"filterable"`
}

func (RegProduct) TableName() string { return "reg_products" }
func (RegProduct) IndexName() string { return "reg_products" }

// RegDoc has a string primary key, the case GORM reads as raw SQL when a
// condition is built from the stringified id.
type RegDoc struct {
	ID   string `gorm:"primarykey" json:"id"`
	Name string `json:"name" meili:"searchable"`
}

func (RegDoc) TableName() string { return "reg_docs" }
func (RegDoc) IndexName() string { return "reg_docs" }

type regHarness struct {
	db     *gorm.DB
	jobs   *TestDispatcher
	gs     *GormSearch
	mu     sync.Mutex
	errors []error
	ops    []string
}

func newRegHarness(t *testing.T, models ...any) *regHarness {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Discard,
	})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(models...))

	h := &regHarness{db: db, jobs: NewTestDispatcher()}
	h.gs = &GormSearch{
		db: db,
		config: &Config{Dispatcher: h.jobs, OnError: func(op string, err error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.ops = append(h.ops, op)
			h.errors = append(h.errors, err)
		}},
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
		ctx:            context.Background(),
	}

	for _, m := range models {
		config, err := parseModel(m)
		assert.NoError(t, err)
		h.gs.storeConfig(config)
	}
	h.gs.registerCallbacks()
	return h
}

func (h *regHarness) reset() {
	h.jobs.mu.Lock()
	h.jobs.jobs = nil
	h.jobs.mu.Unlock()

	h.mu.Lock()
	h.errors, h.ops = nil, nil
	h.mu.Unlock()
}

func (h *regHarness) reportedErrors() []error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]error(nil), h.errors...)
}

// jobIDs returns the document ids dispatched for the given operation.
func (h *regHarness) jobIDs(op string) []string {
	var ids []string
	for _, j := range h.jobs.GetJobs() {
		if j.Operation == op {
			ids = append(ids, j.ID)
		}
	}
	return ids
}

// ============================================================================
// The write hook must not re-run the statement it is reacting to
// ============================================================================

// A plain Session() inherits the in-flight statement, SQL included, so the
// "reload" replayed the INSERT: one Create wrote two rows and indexed an
// empty document.
func TestHook_CreateDoesNotDuplicateRow(t *testing.T) {
	h := newRegHarness(t, &RegProduct{})

	assert.NoError(t, h.db.Create(&RegProduct{Name: "Solo", Price: 42}).Error)

	var rows []RegProduct
	assert.NoError(t, h.db.Find(&rows).Error)
	assert.Len(t, rows, 1, "one Create must insert exactly one row")

	jobs := h.jobs.GetJobs()
	assert.Len(t, jobs, 1)
	assert.Equal(t, "create", jobs[0].Operation)
	assert.Equal(t, "1", jobs[0].ID)

	doc := jobs[0].Document.(map[string]any)
	assert.Equal(t, "Solo", doc["name"], "document must carry the real column values")
	assert.Equal(t, 42.0, doc["price"])
	assert.Empty(t, h.reportedErrors())
}

// Updates never reached the index at all: the replayed UPDATE made the reload
// report "record not found".
func TestHook_UpdateSyncsReloadedRow(t *testing.T) {
	h := newRegHarness(t, &RegProduct{})

	p := &RegProduct{Name: "Orig", Price: 1}
	assert.NoError(t, h.db.Create(p).Error)
	h.reset()

	assert.NoError(t, h.db.Model(p).Update("price", 555).Error)

	jobs := h.jobs.GetJobs()
	assert.Len(t, jobs, 1)
	assert.Equal(t, "update", jobs[0].Operation)

	doc := jobs[0].Document.(map[string]any)
	assert.Equal(t, 555.0, doc["price"])
	assert.Equal(t, "Orig", doc["name"], "unwritten columns must still be indexed")
	assert.Empty(t, h.reportedErrors())
}

// A narrowed Select must not narrow the reload.
func TestHook_UpdateWithSelectStillIndexesWholeRow(t *testing.T) {
	h := newRegHarness(t, &RegProduct{})

	p := &RegProduct{Name: "Orig", Price: 99}
	assert.NoError(t, h.db.Create(p).Error)
	h.reset()

	assert.NoError(t, h.db.Model(p).Select("name").Updates(map[string]any{"name": "Changed"}).Error)

	jobs := h.jobs.GetJobs()
	assert.Len(t, jobs, 1)
	doc := jobs[0].Document.(map[string]any)
	assert.Equal(t, "Changed", doc["name"])
	assert.Equal(t, 99.0, doc["price"])
}

// Stringifying the key made GORM read a non-numeric id as a raw SQL condition.
func TestHook_StringPrimaryKey(t *testing.T) {
	h := newRegHarness(t, &RegDoc{})

	const id = "550e8400-e29b-41d4-a716-446655440000"
	assert.NoError(t, h.db.Create(&RegDoc{ID: id, Name: "Spec"}).Error)

	var rows []RegDoc
	assert.NoError(t, h.db.Find(&rows).Error)
	assert.Len(t, rows, 1)

	jobs := h.jobs.GetJobs()
	assert.Len(t, jobs, 1)
	assert.Equal(t, id, jobs[0].ID)
	assert.Equal(t, "Spec", jobs[0].Document.(map[string]any)["name"])
	assert.Empty(t, h.reportedErrors())
}

// Batch writes hand GORM a *[]Product, whose type name never matched the
// registered model, so nothing was synced.
func TestHook_BatchCreateFansOut(t *testing.T) {
	h := newRegHarness(t, &RegProduct{})

	batch := []RegProduct{{Name: "A"}, {Name: "B"}, {Name: "C"}}
	assert.NoError(t, h.db.Create(&batch).Error)

	assert.Equal(t, []string{"1", "2", "3"}, h.jobIDs("create"))
	assert.Empty(t, h.reportedErrors())
}

// db.Delete(&Model{}, id) leaves the struct zeroed, so the hook used to remove
// document "0" and leave the real one behind.
func TestHook_DeleteByIDRemovesCorrectDocument(t *testing.T) {
	h := newRegHarness(t, &RegProduct{})

	p := &RegProduct{Name: "Doomed"}
	assert.NoError(t, h.db.Create(p).Error)
	h.reset()

	assert.NoError(t, h.db.Delete(&RegProduct{}, p.ID).Error)

	assert.Equal(t, []string{"1"}, h.jobIDs("delete"))
	assert.Empty(t, h.reportedErrors())
}

// Deleting by condition resolves every affected key.
func TestHook_ConditionalDeleteResolvesAllIDs(t *testing.T) {
	h := newRegHarness(t, &RegProduct{})

	assert.NoError(t, h.db.Create(&[]RegProduct{
		{Name: "cheap", Price: 5},
		{Name: "dear", Price: 500},
		{Name: "dearer", Price: 900},
	}).Error)
	h.reset()

	assert.NoError(t, h.db.Where("price > ?", 100).Delete(&RegProduct{}).Error)

	assert.ElementsMatch(t, []string{"2", "3"}, h.jobIDs("delete"))
	assert.Empty(t, h.reportedErrors())
}

// Deleting a loaded struct must not trigger the resolution query.
func TestHook_DeleteLoadedStruct(t *testing.T) {
	h := newRegHarness(t, &RegProduct{})

	p := &RegProduct{Name: "Doomed"}
	assert.NoError(t, h.db.Create(p).Error)
	h.reset()

	assert.NoError(t, h.db.Delete(p).Error)
	assert.Equal(t, []string{"1"}, h.jobIDs("delete"))
}

// A write that identifies rows only by condition cannot be synced; that has to
// be reported rather than silently dropped or applied to document "0".
func TestHook_ConditionalUpdateReportsUnresolvedKey(t *testing.T) {
	h := newRegHarness(t, &RegProduct{})

	assert.NoError(t, h.db.Create(&RegProduct{Name: "Orig", Price: 1}).Error)
	h.reset()

	assert.NoError(t, h.db.Model(&RegProduct{}).Where("price = ?", 1).Update("price", 2).Error)

	assert.Empty(t, h.jobs.GetJobs())
	errs := h.reportedErrors()
	assert.Len(t, errs, 1)
	assert.ErrorIs(t, errs[0], ErrUnresolvedPrimaryKey)
}

// Soft deletes still have to leave the index.
func TestHook_SoftDeleteRemovesDocument(t *testing.T) {
	h := newRegHarness(t, &RegProduct{})

	p := &RegProduct{Name: "Doomed"}
	assert.NoError(t, h.db.Create(p).Error)
	h.reset()

	assert.NoError(t, h.db.Delete(p).Error)

	var live []RegProduct
	assert.NoError(t, h.db.Find(&live).Error)
	assert.Empty(t, live, "row should be soft deleted, not gone")
	assert.Equal(t, []string{"1"}, h.jobIDs("delete"))
}

// Callbacks are installed once per instance, not once per registered model.
func TestHook_RegisteringTwiceDoesNotDoubleDispatch(t *testing.T) {
	h := newRegHarness(t, &RegProduct{})
	h.gs.registerCallbacks()
	h.gs.registerCallbacks()

	assert.NoError(t, h.db.Create(&RegProduct{Name: "Once"}).Error)
	assert.Len(t, h.jobs.GetJobs(), 1)
}

// ============================================================================
// Primary key extraction
// ============================================================================

func TestIDFromValue_ZeroKeyIsUnresolved(t *testing.T) {
	config, err := parseModel(&RegProduct{})
	assert.NoError(t, err)

	_, id := idFromValue(reflect.ValueOf(RegProduct{}), config)
	assert.Equal(t, "", id, "a zero key must not be reported as document \"0\"")

	raw, id := idFromValue(reflect.ValueOf(RegProduct{ID: 7}), config)
	assert.Equal(t, "7", id)
	assert.Equal(t, uint(7), raw, "the raw key keeps its type for SQL conditions")
}

func TestValToString_NamedAndPointerTypes(t *testing.T) {
	type UserID uint64
	assert.Equal(t, "9007199254740993", valToString(reflect.ValueOf(UserID(9007199254740993))))

	n := int64(-42)
	assert.Equal(t, "-42", valToString(reflect.ValueOf(&n)))

	var nilPtr *int64
	assert.Equal(t, "", valToString(reflect.ValueOf(nilPtr)))
	assert.Equal(t, "", valToString(reflect.Value{}))
}

// Reflection helpers must not panic on values GORM can hand a callback.
func TestReflectHelpers_DoNotPanicOnOddValues(t *testing.T) {
	config, err := parseModel(&RegProduct{})
	assert.NoError(t, err)

	var nilModel *RegProduct
	assert.NotPanics(t, func() {
		assert.Equal(t, "", extractID(nilModel, config))
		assert.False(t, isSoftDeleted(nilModel, config))
		assert.Empty(t, (&GormSearch{config: &Config{}}).toDocument(nilModel, config))
	})

	assert.NotPanics(t, func() {
		assert.Equal(t, "", extractID([]RegProduct{{ID: 1}}, config))
		assert.False(t, isSoftDeleted(map[string]any{}, config))
	})
}

func TestStructTypeName_UnwrapsSlices(t *testing.T) {
	assert.Equal(t, "RegProduct", structTypeName(reflect.TypeOf(&[]RegProduct{})))
	assert.Equal(t, "RegProduct", structTypeName(reflect.TypeOf([]*RegProduct{})))
	assert.Equal(t, "", structTypeName(reflect.TypeOf("x")))
	assert.Equal(t, "", structTypeName(nil))
}

// The registry key is package qualified so same-named models cannot collide.
func TestTypeKey_IsPackageQualified(t *testing.T) {
	key := typeKey(reflect.TypeOf(&RegProduct{}))
	assert.Equal(t, "github.com/balcieren/gormsearch.RegProduct", key)
	assert.Equal(t, key, typeKey(reflect.TypeOf([]RegProduct{})))
	assert.Equal(t, "", typeKey(reflect.TypeOf(1)))
}

// ============================================================================
// Typed decoding
// ============================================================================

type RegBigID struct {
	ID    int64    `gorm:"primarykey" json:"id"`
	Name  string   `json:"name" meili:"searchable"`
	Tags  []string `json:"tags"`
	Ratio float64  `json:"ratio"`
}

func (RegBigID) IndexName() string { return "reg_bigids" }

func newSearchMock(t *testing.T, model any, hits meilisearch.Hits, opts ...Option) *GormSearch {
	t.Helper()

	client := new(MockClient)
	index := new(MockIndex)
	client.On("Index", mock.Anything).Return(index)
	index.On("SearchWithContext", mock.Anything, mock.Anything, mock.Anything).
		Return(&meilisearch.SearchResponse{Hits: hits}, nil)

	config := &Config{}
	for _, opt := range opts {
		opt(config)
	}

	gs := &GormSearch{
		client:         client,
		config:         config,
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
		ctx:            context.Background(),
	}
	parsed, err := parseModel(model)
	assert.NoError(t, err)
	gs.storeConfig(parsed)
	return gs
}

// Routing hits through map[string]any turned every JSON number into a float64,
// which silently rounds integers past 2^53.
func TestSearchFor_PreservesLargeIntegers(t *testing.T) {
	const bigID = int64(9007199254740993) // 2^53 + 1

	gs := newSearchMock(t, &RegBigID{}, meilisearch.Hits{{
		"id":    json.RawMessage(`9007199254740993`),
		"name":  json.RawMessage(`"snowflake"`),
		"tags":  json.RawMessage(`["a","b"]`),
		"ratio": json.RawMessage(`0.5`),
	}})

	res, err := SearchFor[RegBigID](gs, "x")
	assert.NoError(t, err)
	assert.Len(t, res.Hits, 1)
	assert.Equal(t, bigID, res.Hits[0].ID)
	assert.Equal(t, "snowflake", res.Hits[0].Name)
	assert.Equal(t, []string{"a", "b"}, res.Hits[0].Tags)
	assert.Equal(t, 0.5, res.Hits[0].Ratio)
}

// A document that no longer matches the struct must not fail the search.
func TestSearchFor_SkipsMismatchedFields(t *testing.T) {
	gs := newSearchMock(t, &RegBigID{}, meilisearch.Hits{{
		"id":   json.RawMessage(`7`),
		"name": json.RawMessage(`{"unexpected":"object"}`),
		"tags": json.RawMessage(`null`),
	}})

	res, err := SearchFor[RegBigID](gs, "x")
	assert.NoError(t, err)
	assert.Len(t, res.Hits, 1)
	assert.Equal(t, int64(7), res.Hits[0].ID)
	assert.Equal(t, "", res.Hits[0].Name)
	assert.Nil(t, res.Hits[0].Tags)
}

// WithJSONDecoder was accepted but never consulted.
func TestSearchFor_UsesConfiguredJSONDecoder(t *testing.T) {
	var calls int
	gs := newSearchMock(t, &RegBigID{},
		meilisearch.Hits{{"id": json.RawMessage(`5`)}},
		WithJSONDecoder(func(data []byte, v any) error {
			calls++
			return json.Unmarshal(data, v)
		}),
	)

	res, err := SearchFor[RegBigID](gs, "x")
	assert.NoError(t, err)
	assert.Equal(t, int64(5), res.Hits[0].ID)
	assert.Positive(t, calls, "the configured decoder must actually be used")
}

// ============================================================================
// Index settings
// ============================================================================

type RegSettingsModel struct {
	ID   uint   `gorm:"primarykey" json:"id"`
	Name string `json:"name"`
}

var sharedSettings = &meilisearch.Settings{DisplayedAttributes: []string{"name"}}

func (RegSettingsModel) IndexName() string                    { return "reg_settings" }
func (RegSettingsModel) MeiliSettings() *meilisearch.Settings { return sharedSettings }

// hasSettings enumerated fields by hand and silently skipped the rest.
func TestHasSettings_CoversAllFields(t *testing.T) {
	assert.False(t, hasSettings(&meilisearch.Settings{}))
	assert.False(t, hasSettings(nil))
	assert.True(t, hasSettings(&meilisearch.Settings{DisplayedAttributes: []string{"name"}}))
	assert.True(t, hasSettings(&meilisearch.Settings{SearchCutoffMs: 50}))
	assert.True(t, hasSettings(&meilisearch.Settings{Dictionary: []string{"w"}}))
}

// A provider commonly returns a shared value; merging tag settings into it
// would leak one model's attributes into every other model.
func TestBuildSettings_DoesNotMutateProviderValue(t *testing.T) {
	before := *sharedSettings

	gs := &GormSearch{config: &Config{}}
	config, err := parseModel(&RegSettingsModel{})
	assert.NoError(t, err)
	config.SearchableFields = []string{"name"}
	config.FilterableFields = []string{"id"}

	settings := gs.buildSettings(config)
	assert.Equal(t, []string{"name"}, settings.SearchableAttributes)
	assert.Equal(t, []string{"name"}, settings.DisplayedAttributes)
	assert.Equal(t, before, *sharedSettings, "provider settings must be left untouched")
}

// ============================================================================
// Filter builder
// ============================================================================

func TestFilterBuilder_Escaping(t *testing.T) {
	assert.Equal(t, `name = 'O\'Brien'`, NewFilter().Where("name").Eq("O'Brien").Build())
	assert.Equal(t, `path = 'a\\b'`, NewFilter().Where("path").Eq(`a\b`).Build())
	assert.Equal(t, `path = 'a\\\'b'`, NewFilter().Where("path").Eq(`a\'b`).Build())
}

// %v renders large and small magnitudes in exponent form, which Meilisearch
// rejects in filters.
func TestFilterBuilder_FloatsAvoidExponentForm(t *testing.T) {
	assert.Equal(t, "price > 10000000", NewFilter().Where("price").Gt(1e7).Build())
	assert.Equal(t, "ratio < 0.0000001", NewFilter().Where("ratio").Lt(1e-7).Build())
	assert.Equal(t, "p = 1.5", NewFilter().Where("p").Eq(float32(1.5)).Build())
}

// Without parentheses NOT binds to the first condition only.
func TestFilterBuilder_NotWrapsSubFilter(t *testing.T) {
	got := NewFilter().Not(func(f *FilterBuilder) {
		f.Where("a").Eq(1).And().Where("b").Eq(2)
	}).Build()
	assert.Equal(t, "NOT (a = 1 AND b = 2)", got)
}

func TestFilterBuilder_IgnoresDanglingOperators(t *testing.T) {
	assert.Equal(t, "a = 1 AND b = 2",
		NewFilter().Where("a").Eq(1).And().And().Or().Where("b").Eq(2).Build())
	assert.Equal(t, "a = 1", NewFilter().And().Where("a").Eq(1).Build())
}

// ============================================================================
// Parser
// ============================================================================

func TestParseModel_RejectsNonStruct(t *testing.T) {
	_, err := parseModel("not a model")
	assert.ErrorIs(t, err, ErrInvalidModel)

	_, err = parseModel(nil)
	assert.ErrorIs(t, err, ErrNilModel)
}

type RegSkippedEmbed struct {
	Secret string `json:"secret"`
}

type RegWithSkippedEmbed struct {
	ID              uint `gorm:"primarykey" json:"id"`
	RegSkippedEmbed `json:"-"`
	Name            string `json:"name"`
}

// An embedded struct was walked into before its own skip tag was read.
func TestParseModel_HonoursSkipTagOnEmbeddedStruct(t *testing.T) {
	config, err := parseModel(&RegWithSkippedEmbed{})
	assert.NoError(t, err)

	names := make([]string, 0, len(config.FieldExtractors))
	for _, e := range config.FieldExtractors {
		names = append(names, e.JSONName)
	}
	assert.ElementsMatch(t, []string{"id", "name"}, names)
	assert.NotContains(t, names, "secret")
}

// ============================================================================
// Dispatcher
// ============================================================================

// The old loop slept through a backoff it was never going to use.
func TestRetry_DoesNotSleepAfterFinalAttempt(t *testing.T) {
	d := &DefaultDispatcher{maxRetries: 3}

	var attempts int
	start := time.Now()
	err := d.retryWithContext(context.Background(), func() error {
		attempts++
		return errors.New("boom")
	})
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Equal(t, 3, attempts)
	// 100ms + 200ms between attempts, but no 400ms tail.
	assert.Less(t, elapsed, 380*time.Millisecond)
	assert.GreaterOrEqual(t, elapsed, 300*time.Millisecond)
}

func TestQueueDispatcher_DefaultsAndValidation(t *testing.T) {
	var published []byte
	d := &QueueDispatcher{publish: func(_ context.Context, data []byte) error {
		published = data
		return nil
	}}

	// encoder unset: falls back to encoding/json rather than panicking.
	assert.NoError(t, d.Dispatch(context.Background(), Job{IndexName: "i", Operation: "create", ID: "1"}))
	assert.Contains(t, string(published), `"IndexName":"i"`)

	assert.ErrorIs(t, (&QueueDispatcher{}).Dispatch(context.Background(), Job{}), ErrNoPublisher)
}

// ============================================================================
// Search plumbing
// ============================================================================

func TestMultiSearch_NoQueriesReturnsError(t *testing.T) {
	gs := &GormSearch{
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
		config:         &Config{},
	}

	res, err := gs.MultiSearch()
	assert.Nil(t, res)
	assert.ErrorIs(t, err, ErrNoQueries, "callers must not get (nil, nil) to dereference")
}
