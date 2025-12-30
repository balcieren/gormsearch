package gormsearch

import (
	"testing"
	"time"

	"gorm.io/gorm"
)

// Mock model for testing
type MockProduct struct {
	gorm.Model
	Name  string `json:"name" meili:"searchable"`
	Price int    `json:"price" meili:"filterable,sortable"`
}

func TestToDocument(t *testing.T) {
	gs := &GormSearch{
		config: &Config{},
	}

	product := &MockProduct{
		Model: gorm.Model{
			ID:        1,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		Name:  "Test Product",
		Price: 100,
	}

	// Parse config from model to get extractors
	parsedConfig, _ := parseModel(product)

	// Copy extractors to our manual config or just use parsedConfig
	doc := gs.toDocument(product, parsedConfig)

	if doc["name"] != "Test Product" {
		t.Errorf("expected name 'Test Product', got %v", doc["name"])
	}

	if doc["price"] != 100 {
		t.Errorf("expected price 100, got %v", doc["price"])
	}
}

func TestExtractID(t *testing.T) {
	product := &MockProduct{
		Model: gorm.Model{ID: 42},
		Name:  "Test",
	}
	config, _ := parseModel(product)

	id := extractID(product, config)
	if id != "42" {
		t.Errorf("expected ID '42', got '%s'", id)
	}
}

func TestIsMatchingModel(t *testing.T) {
	config := &IndexConfig{
		ModelType: "MockProduct",
	}

	product := &MockProduct{}
	if !isMatchingModel(product, config) {
		t.Error("expected model to match")
	}

	// Test with a different type
	other := &SimpleModel{}
	if isMatchingModel(other, config) {
		t.Error("expected model not to match")
	}
}

func TestFieldInfoFlags(t *testing.T) {
	config, err := parseModel(&MockProduct{})
	if err != nil {
		t.Fatalf("parseModel failed: %v", err)
	}

	// Check Name field
	nameInfo := config.FieldMapping["Name"]
	if !nameInfo.Searchable {
		t.Error("Name should be searchable")
	}
	if nameInfo.Filterable {
		t.Error("Name should not be filterable")
	}

	// Check Price field
	priceInfo := config.FieldMapping["Price"]
	if priceInfo.Searchable {
		t.Error("Price should not be searchable")
	}
	if !priceInfo.Filterable {
		t.Error("Price should be filterable")
	}
	if !priceInfo.Sortable {
		t.Error("Price should be sortable")
	}
}
