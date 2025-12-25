package gormsearch

import (
	"fmt"
	"reflect"
	"strconv"
	"time"
)

// toDocument converts a GORM model to a Meilisearch document.
func (gs *GormSearch) toDocument(model any, config *IndexConfig) map[string]any {
	v := reflectValue(model)
	t := v.Type()
	doc := make(map[string]any)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		if !field.IsExported() {
			continue
		}

		if field.Anonymous {
			for k, v := range extractEmbeddedFields(value, config) {
				doc[k] = v
			}
			continue
		}

		info, exists := config.FieldMapping[field.Name]
		if !exists || info.Skip {
			continue
		}

		doc[info.JSONName] = value.Interface()
	}

	return doc
}

// extractEmbeddedFields extracts fields from embedded structs.
func extractEmbeddedFields(v reflect.Value, config *IndexConfig) map[string]any {
	v = reflectValue(v.Interface())
	if !v.IsValid() {
		return nil
	}

	t := v.Type()
	result := make(map[string]any)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		info, exists := config.FieldMapping[field.Name]
		if !exists || info.Skip {
			continue
		}

		result[info.JSONName] = v.Field(i).Interface()
	}

	return result
}

// extractID extracts the primary key value from a model.
func extractID(model any) string {
	v := reflectValue(model)

	idField := v.FieldByName("ID")
	if !idField.IsValid() {
		if modelField := v.FieldByName("Model"); modelField.IsValid() {
			idField = modelField.FieldByName("ID")
		}
	}

	if !idField.IsValid() {
		return ""
	}

	// Fast path for common types
	switch id := idField.Interface().(type) {
	case uint:
		return strconv.FormatUint(uint64(id), 10)
	case uint64:
		return strconv.FormatUint(id, 10)
	case uint32:
		return strconv.FormatUint(uint64(id), 10)
	case int:
		return strconv.FormatInt(int64(id), 10)
	case int64:
		return strconv.FormatInt(id, 10)
	case string:
		return id
	default:
		return fmt.Sprintf("%v", id)
	}
}

// isSoftDeleted checks if a model has been soft deleted (DeletedAt is set).
func isSoftDeleted(model any) bool {
	v := reflectValue(model)

	// Check direct DeletedAt field
	deletedAt := v.FieldByName("DeletedAt")
	if deletedAt.IsValid() {
		return isDeletedAtSet(deletedAt)
	}

	// Check embedded Model struct (gorm.Model)
	modelField := v.FieldByName("Model")
	if modelField.IsValid() {
		deletedAt = modelField.FieldByName("DeletedAt")
		if deletedAt.IsValid() {
			return isDeletedAtSet(deletedAt)
		}
	}

	return false
}

// isDeletedAtSet checks if DeletedAt field has a valid (non-zero) time.
func isDeletedAtSet(v reflect.Value) bool {
	// Handle gorm.DeletedAt (which has Valid field)
	if v.Kind() == reflect.Struct {
		validField := v.FieldByName("Valid")
		if validField.IsValid() && validField.Kind() == reflect.Bool {
			return validField.Bool()
		}

		// Handle time.Time directly
		timeField := v.FieldByName("Time")
		if timeField.IsValid() {
			if t, ok := timeField.Interface().(time.Time); ok {
				return !t.IsZero()
			}
		}
	}

	// Handle *time.Time
	if v.Kind() == reflect.Ptr {
		return !v.IsNil()
	}

	return false
}

// reflectValue returns the underlying reflect.Value, dereferencing pointers.
func reflectValue(v any) reflect.Value {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return reflect.Value{}
		}
		rv = rv.Elem()
	}
	return rv
}

// isMatchingModel checks if the model matches the registered config.
func isMatchingModel(model any, config *IndexConfig) bool {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name() == config.ModelType
}
