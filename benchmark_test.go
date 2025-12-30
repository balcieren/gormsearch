package gormsearch

import (
	"testing"

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

	// Setup config with cached indices
	config := &IndexConfig{
		IDFieldIndices: []int{0}, // ID is the first field in gorm.Model
	}

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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isSoftDeleted(model)
	}
}
