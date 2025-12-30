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
	}

	// Parse struct fields
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Handle embedded structs (like gorm.Model)
		if field.Anonymous {
			embeddedConfig, err := parseEmbeddedStruct(field.Type)
			if err != nil {
				continue
			}
			mergeFieldMappings(config, embeddedConfig)
			continue
		}

		info := parseFieldTags(field)
		if info.Skip {
			continue
		}

		config.FieldMapping[field.Name] = info

		// Check for primary key
		if info.PrimaryKey {
			config.PrimaryKey = info.JSONName
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
	}

	return config, nil
}

// parseEmbeddedStruct handles embedded struct fields.
func parseEmbeddedStruct(t reflect.Type) (*IndexConfig, error) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	config := &IndexConfig{
		FieldMapping: make(map[string]fieldInfo),
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		info := parseFieldTags(field)
		if !info.Skip {
			config.FieldMapping[field.Name] = info
		}
	}

	return config, nil
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
func toSnakeCase(s string) string {
	namingStrategy := schema.NamingStrategy{}
	return namingStrategy.ColumnName("", s)
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
