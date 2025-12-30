package gormsearch

import (
	"reflect"
	"strings"

	"gorm.io/gorm/schema"
)

// parseModel extracts index configuration from a model using struct tags.
func parseModel(model any) (*IndexConfig, error) {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	config := &IndexConfig{
		IndexName:        resolveIndexName(model, t),
		PrimaryKey:       "id",
		ModelType:        t.Name(),
		SearchableFields: make([]string, 0),
		FilterableFields: make([]string, 0),
		SortableFields:   make([]string, 0),
		FieldMapping:     make(map[string]fieldInfo),
		Model:            model,
		FieldExtractors:  make([]FieldExtractor, 0),
	}

	// Recursively parse fields
	parseFieldsRecursive(t, config, nil)

	return config, nil
}

// parseFieldsRecursive walks the struct type tree to find all fields and build extractors.
func parseFieldsRecursive(t reflect.Type, config *IndexConfig, parentIndex []int) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		currIndex := append(parentIndex, i)

		// Skip unexported fields (unless anonymous/embedded)
		if !field.IsExported() {
			continue
		}

		// Handle embedded structs
		if field.Anonymous {
			parseFieldsRecursive(field.Type, config, currIndex)
			continue
		}

		// Parse tags
		info := parseFieldTags(field)
		if info.Skip {
			continue
		}

		config.FieldMapping[field.Name] = info

		// Update config based on tags
		if info.PrimaryKey {
			config.PrimaryKey = info.JSONName
			// Store ID field indices only if it's the primary key
			config.IDFieldIndices = make([]int, len(currIndex))
			copy(config.IDFieldIndices, currIndex)
		} else if field.Name == "ID" && len(config.IDFieldIndices) == 0 {
			// Default ID fallback
			config.IDFieldIndices = make([]int, len(currIndex))
			copy(config.IDFieldIndices, currIndex)
		}

		// Check for DeletedAt (soft delete)
		if field.Name == "DeletedAt" && len(config.DeletedAtIndex) == 0 {
			config.DeletedAtIndex = make([]int, len(currIndex))
			copy(config.DeletedAtIndex, currIndex)
		}

		if info.Searchable {
			config.SearchableFields = append(config.SearchableFields, info.JSONName)
		}
		if info.Filterable {
			config.FilterableFields = append(config.FilterableFields, info.JSONName)
		}
		if info.Sortable {
			config.SortableFields = append(config.SortableFields, info.JSONName)
		}

		// Add to extractors
		extractor := FieldExtractor{
			FieldIndex: make([]int, len(currIndex)),
			JSONName:   info.JSONName,
			IsGeo:      info.Geo,
		}
		copy(extractor.FieldIndex, currIndex)

		// Optimization: Pre-compute Geo field indices
		if info.Geo {
			if field.Type.Kind() == reflect.Struct {
				latIdx, lngIdx := findGeoIndices(field.Type)
				if latIdx != nil && lngIdx != nil {
					extractor.GeoLatIndex = latIdx
					extractor.GeoLngIndex = lngIdx
				}
			}
		}

		config.FieldExtractors = append(config.FieldExtractors, extractor)
	}
}

// findGeoIndices searches for Lat/Latitude and Lng/Longitude fields in a struct.
func findGeoIndices(t reflect.Type) ([]int, []int) {
	var latIndex, lngIndex []int

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name := strings.ToLower(field.Name)

		if name == "lat" || name == "latitude" {
			latIndex = []int{i}
		} else if name == "lng" || name == "lon" || name == "longitude" {
			lngIndex = []int{i}
		}
	}
	return latIndex, lngIndex
}

// parseFieldTags extracts field configuration from struct tags.
func parseFieldTags(field reflect.StructField) fieldInfo {
	info := fieldInfo{
		JSONName: toSnakeCase(field.Name),
	}

	// Parse json tag for field name
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		parts := strings.Split(jsonTag, ",")
		if parts[0] == "-" {
			info.Skip = true
			return info
		}
		if parts[0] != "" {
			info.JSONName = parts[0]
		}
	}

	// Parse meili tag for search configuration
	meiliTag := field.Tag.Get("meili")
	if meiliTag == "-" {
		info.Skip = true
		return info
	}

	if meiliTag != "" {
		options := strings.Split(meiliTag, ",")
		for _, opt := range options {
			switch strings.TrimSpace(opt) {
			case "searchable":
				info.Searchable = true
			case "filterable":
				info.Filterable = true
			case "sortable":
				info.Sortable = true
			case "primaryKey":
				info.PrimaryKey = true
			case "geo":
				info.Geo = true
				info.JSONName = "_geo" // Force standard name for geo fields
			}
		}
	}

	return info
}

// mergeFieldMappings merges embedded struct fields into the main config.
func mergeFieldMappings(main, embedded *IndexConfig) {
	for name, info := range embedded.FieldMapping {
		if _, exists := main.FieldMapping[name]; !exists {
			main.FieldMapping[name] = info
		}
	}
}

// toSnakeCase converts CamelCase to snake_case.
// defaultNamingStrategy is efficient to reuse as it's stateless for standard usage.
var defaultNamingStrategy = schema.NamingStrategy{}

// toSnakeCase converts CamelCase to snake_case.
func toSnakeCase(s string) string {
	return defaultNamingStrategy.ColumnName("", s)
}

// resolveIndexName determines the index name with priority:
// 1. IndexName() if model implements Indexable
// 2. TableName() if model implements Tabler (GORM)
// 3. Default: snake_case(StructName) + "s"
func resolveIndexName(model any, t reflect.Type) string {
	if indexable, ok := model.(Indexable); ok {
		return indexable.IndexName()
	}
	if tabler, ok := model.(Tabler); ok {
		return tabler.TableName()
	}
	return toSnakeCase(t.Name()) + "s"
}
