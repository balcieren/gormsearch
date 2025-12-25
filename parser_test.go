package gormsearch

import (
	"testing"

	"gorm.io/gorm"
)

// Test models
type TestProduct struct {
	gorm.Model
	Name        string  `json:"name" meili:"searchable,filterable"`
	Description string  `json:"description" meili:"searchable"`
	Price       float64 `json:"price" meili:"filterable,sortable"`
	Category    string  `json:"category" meili:"filterable"`
	InternalSKU string  `json:"-" meili:"-"`
}

func (t TestProduct) IndexName() string {
	return "test_products"
}

type SimpleModel struct {
	ID   uint   `json:"id"`
	Name string `json:"name" meili:"searchable"`
}

type NoTagsModel struct {
	ID   uint
	Name string
}

type SkipAllModel struct {
	ID   uint   `meili:"-"`
	Name string `meili:"-"`
}

func TestParseModel_WithIndexable(t *testing.T) {
	config, err := parseModel(&TestProduct{})
	if err != nil {
		t.Fatalf("parseModel failed: %v", err)
	}

	if config.IndexName != "test_products" {
		t.Errorf("expected IndexName 'test_products', got '%s'", config.IndexName)
	}

	if config.PrimaryKey != "id" {
		t.Errorf("expected PrimaryKey 'id', got '%s'", config.PrimaryKey)
	}

	if config.ModelType != "TestProduct" {
		t.Errorf("expected ModelType 'TestProduct', got '%s'", config.ModelType)
	}
}

func TestParseModel_SearchableFields(t *testing.T) {
	config, err := parseModel(&TestProduct{})
	if err != nil {
		t.Fatalf("parseModel failed: %v", err)
	}

	expected := []string{"name", "description"}
	if len(config.SearchableFields) != len(expected) {
		t.Errorf("expected %d searchable fields, got %d", len(expected), len(config.SearchableFields))
	}

	for _, exp := range expected {
		found := false
		for _, got := range config.SearchableFields {
			if got == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected searchable field '%s' not found", exp)
		}
	}
}

func TestParseModel_FilterableFields(t *testing.T) {
	config, err := parseModel(&TestProduct{})
	if err != nil {
		t.Fatalf("parseModel failed: %v", err)
	}

	expected := []string{"name", "price", "category"}
	if len(config.FilterableFields) != len(expected) {
		t.Errorf("expected %d filterable fields, got %d", len(expected), len(config.FilterableFields))
	}
}

func TestParseModel_SortableFields(t *testing.T) {
	config, err := parseModel(&TestProduct{})
	if err != nil {
		t.Fatalf("parseModel failed: %v", err)
	}

	if len(config.SortableFields) != 1 || config.SortableFields[0] != "price" {
		t.Errorf("expected sortable field 'price', got %v", config.SortableFields)
	}
}

func TestParseModel_SkippedFields(t *testing.T) {
	config, err := parseModel(&TestProduct{})
	if err != nil {
		t.Fatalf("parseModel failed: %v", err)
	}

	// InternalSKU should be skipped
	if _, exists := config.FieldMapping["InternalSKU"]; exists {
		t.Error("InternalSKU should be skipped but exists in FieldMapping")
	}
}

func TestParseModel_WithoutIndexable(t *testing.T) {
	config, err := parseModel(&SimpleModel{})
	if err != nil {
		t.Fatalf("parseModel failed: %v", err)
	}

	// Should use default snake_case + 's'
	if config.IndexName != "simple_models" {
		t.Errorf("expected IndexName 'simple_models', got '%s'", config.IndexName)
	}
}

func TestParseModel_NoTags(t *testing.T) {
	config, err := parseModel(&NoTagsModel{})
	if err != nil {
		t.Fatalf("parseModel failed: %v", err)
	}

	// Should have no searchable/filterable/sortable fields
	if len(config.SearchableFields) != 0 {
		t.Errorf("expected 0 searchable fields, got %d", len(config.SearchableFields))
	}
	if len(config.FilterableFields) != 0 {
		t.Errorf("expected 0 filterable fields, got %d", len(config.FilterableFields))
	}
	if len(config.SortableFields) != 0 {
		t.Errorf("expected 0 sortable fields, got %d", len(config.SortableFields))
	}
}

func TestParseModel_SkipAll(t *testing.T) {
	config, err := parseModel(&SkipAllModel{})
	if err != nil {
		t.Fatalf("parseModel failed: %v", err)
	}

	if len(config.FieldMapping) != 0 {
		t.Errorf("expected 0 fields in FieldMapping, got %d", len(config.FieldMapping))
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Name", "name"},
		{"FirstName", "first_name"},
		{"ID", "id"},
		{"HTMLParser", "html_parser"},
	}

	for _, tt := range tests {
		got := toSnakeCase(tt.input)
		if got != tt.expected {
			t.Errorf("toSnakeCase(%s) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}
