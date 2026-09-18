package gormsearch

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/meilisearch/meilisearch-go"
	"gorm.io/gorm"
)

// Benchmark models
type BenchProduct struct {
	gorm.Model
	Name        string  `json:"name" meili:"searchable,filterable"`
	Description string  `json:"description" meili:"searchable"`
	Price       float64 `json:"price" meili:"filterable,sortable"`
	Category    string  `json:"category" meili:"filterable"`
}

func (b BenchProduct) IndexName() string {
	return "bench_products"
}

// BenchmarkParseModel benchmarks the parseModel function.
func BenchmarkParseModel(b *testing.B) {
	model := &BenchProduct{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseModel(model)
	}
}

// BenchmarkToDocument benchmarks document conversion.
func BenchmarkToDocument(b *testing.B) {
	gs := &GormSearch{
		registry: make(map[string]*IndexConfig),
	}
	config, _ := parseModel(&BenchProduct{})
	model := &BenchProduct{
		Model:       gorm.Model{ID: 1},
		Name:        "Test Product",
		Description: "A test product description",
		Price:       99.99,
		Category:    "electronics",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.toDocument(model, config)
	}
}

// BenchmarkExtractID benchmarks ID extraction.
func BenchmarkExtractID(b *testing.B) {
	model := &BenchProduct{
		Model: gorm.Model{ID: 12345},
	}

	// ID sits at index {0, 0}: field 0 of the embedded gorm.Model. Parse the
	// model rather than hand-writing the path, so this measures key extraction
	// instead of formatting a whole struct.
	config, _ := parseModel(model)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractID(model, config)
	}
}

// BenchmarkIndexNameFor benchmarks cached index name lookup.
func BenchmarkIndexNameFor(b *testing.B) {
	// Warm up cache
	indexNameFor[BenchProduct]()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		indexNameFor[BenchProduct]()
	}
}

// BenchmarkIndexNameFor_Cold benchmarks cold index name lookup.
func BenchmarkIndexNameFor_Cold(b *testing.B) {
	for i := 0; i < b.N; i++ {
		// Clear cache by using different types is not possible,
		// so this measures already-cached performance
		indexNameFor[BenchProduct]()
	}
}

// BenchmarkDecodeHits benchmarks hit decoding.
func BenchmarkDecodeHits(b *testing.B) {
	hits := []map[string]any{
		{"id": 1, "name": "Product 1", "price": 99.99},
		{"id": 2, "name": "Product 2", "price": 149.99},
		{"id": 3, "name": "Product 3", "price": 199.99},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeHits[BenchProduct](hits)
	}
}

// BenchmarkIsMatchingModel benchmarks model matching.
func BenchmarkIsMatchingModel(b *testing.B) {
	config := &IndexConfig{ModelType: "BenchProduct"}
	model := &BenchProduct{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isMatchingModel(model, config)
	}
}

// BenchmarkIsSoftDeleted benchmarks soft delete check.
func BenchmarkIsSoftDeleted(b *testing.B) {
	model := &BenchProduct{
		Model: gorm.Model{ID: 1},
	}

	// Setup config with cached indices
	config, _ := parseModel(model)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isSoftDeleted(model, config)
	}
}

// benchHits is a representative Meilisearch response body for decode benchmarks.
var benchHits = func() meilisearch.Hits {
	hits := make(meilisearch.Hits, 0, 20)
	for i := 0; i < 20; i++ {
		hits = append(hits, meilisearch.Hit{
			"id":          json.RawMessage(`1`),
			"name":        json.RawMessage(`"Test Product"`),
			"description": json.RawMessage(`"A test product description"`),
			"price":       json.RawMessage(`99.99`),
			"category":    json.RawMessage(`"electronics"`),
			"created_at":  json.RawMessage(`"2024-01-01T00:00:00Z"`),
		})
	}
	return hits
}()

func benchDecodeGS() *GormSearch {
	gs := &GormSearch{
		config:         &Config{},
		registry:       make(map[string]*IndexConfig),
		registryByType: make(map[string]*IndexConfig),
		mu:             &sync.RWMutex{},
	}
	config, _ := parseModel(&BenchProduct{})
	gs.storeConfig(config)
	return gs
}

// BenchmarkDecodeHits_Raw decodes straight from the response bytes.
func BenchmarkDecodeHits_Raw(b *testing.B) {
	gs := benchDecodeGS()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := decodeRawHits[BenchProduct](gs, benchHits); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecodeHits_ViaMaps decodes through the intermediate
// []map[string]any, the shape typed search used to go through.
func BenchmarkDecodeHits_ViaMaps(b *testing.B) {
	gs := benchDecodeGS()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hits := convertHits(benchHits)
		for _, hit := range hits {
			var item BenchProduct
			if err := gs.decodeDocument(hit, &item); err != nil {
				b.Fatal(err)
			}
		}
	}
}
