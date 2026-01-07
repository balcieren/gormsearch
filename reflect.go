package gormsearch

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"
)

// timeType is cached to avoid repeated reflection lookups
var timeType = reflect.TypeOf(time.Time{})

// encodeDocument encodes a model using custom encoder or default reflection.
func (gs *GormSearch) encodeDocument(model any, config *IndexConfig) (any, error) {
	if gs.config != nil {
		// Use JSON Encoder if present
		if gs.config.JSONEncoder != nil {
			data, err := gs.config.JSONEncoder(model)
			if err != nil {
				return nil, err
			}
			return json.RawMessage(data), nil
		}
		// Use Legacy Encoder if present
		if gs.config.MapEncoder != nil {
			return gs.config.MapEncoder(model)
		}
	}
	return gs.toDocument(model, config), nil
}

// toDocument converts a GORM model to a Meilisearch document using cached extractors.
func (gs *GormSearch) toDocument(model any, config *IndexConfig) map[string]any {
	v := reflectValue(model)
	// Optimization: Pre-allocate map with capacity hint
	doc := make(map[string]any, len(config.FieldExtractors))

	for _, extractor := range config.FieldExtractors {
		val := v.FieldByIndex(extractor.FieldIndex)

		if extractor.IsGeo {
			// Zero-allocation Geo extraction
			if len(extractor.GeoLatIndex) > 0 && len(extractor.GeoLngIndex) > 0 {
				lat, err1 := toFloat(val.FieldByIndex(extractor.GeoLatIndex))
				lng, err2 := toFloat(val.FieldByIndex(extractor.GeoLngIndex))

				if err1 == nil && err2 == nil {
					doc[extractor.JSONName] = map[string]float64{
						"lat": lat,
						"lng": lng,
					}
				}
			}
			continue
		}

		doc[extractor.JSONName] = val.Interface()
	}

	return doc
}

// toFloat converts a reflect.Value to float64.
// Supports int, uint, and float types.
func toFloat(v reflect.Value) (float64, error) {
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint()), nil
	default:
		return 0, fmt.Errorf("not a number")
	}
}

// extractID extracts the primary key value from a model using cached indices.
func extractID(model any, config *IndexConfig) string {
	v := reflectValue(model)

	// Optimization: Use cached field index if available
	if len(config.IDFieldIndices) > 0 {
		return valToString(v.FieldByIndex(config.IDFieldIndices))
	}

	// Fallback to legacy lookup (should rarely happen if parsed correctly)
	idField := v.FieldByName("ID")
	if !idField.IsValid() {
		if modelField := v.FieldByName("Model"); modelField.IsValid() {
			idField = modelField.FieldByName("ID")
		}
	}

	if !idField.IsValid() {
		return ""
	}

	return valToString(idField)
}

// valToString converts a reflect.Value to its string representation.
// Uses fast paths for common numeric and string types.
func valToString(v reflect.Value) string {
	// Fast path for common types
	switch id := v.Interface().(type) {
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
func isSoftDeleted(model any, config *IndexConfig) bool {
	v := reflectValue(model)

	// Optimization: Use cached field index if available
	if config != nil && len(config.DeletedAtIndex) > 0 {
		return isDeletedAtSet(v.FieldByIndex(config.DeletedAtIndex))
	}

	// Fallback to legacy lookup (DeletedAt or Model.DeletedAt)
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

// decodeDocument populates a struct from a map using cached field extractors.
// This avoids the JSON roundtrip (map -> json -> struct) which is very expensive.
func (gs *GormSearch) decodeDocument(doc map[string]any, dest any) error {
	// 1. Get the IndexConfig for this type
	// We can infer the index name from the type of dest (which is a pointer to the struct)

	// Unpack pointer
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("gormsearch: decode destination must be a non-nil pointer")
	}
	v = v.Elem() // Now we have the struct

	// Find config
	// Optimization: Use registryByType for O(1) lookup
	typeName := v.Type().Name()
	config := gs.registryByType[typeName]

	if config == nil {
		// If config not found, fallback to JSON (slower but safe)
		data, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, dest)
	}

	// 2. Map fields
	for _, extractor := range config.FieldExtractors {
		val, ok := doc[extractor.JSONName]
		if !ok {
			continue
		}

		field := v.FieldByIndex(extractor.FieldIndex)
		if !field.CanSet() {
			continue
		}

		// Handle Geo fields specifically if implemented,
		// but standard Meilisearch geo is _geo: {lat, lng}
		// Our custom extractor handles flat geo fields in struct -> nested json.
		// Reverse mapping (JSON -> Struct) for Geo:
		if extractor.IsGeo {
			if geoMap, ok := val.(map[string]any); ok {
				if lat, ok := geoMap["lat"].(float64); ok && len(extractor.GeoLatIndex) > 0 {
					setFloat(v.FieldByIndex(extractor.GeoLatIndex), lat)
				}
				if lng, ok := geoMap["lng"].(float64); ok && len(extractor.GeoLngIndex) > 0 {
					setFloat(v.FieldByIndex(extractor.GeoLngIndex), lng)
				}
			}
			continue
		}

		// Set value based on kind
		setFieldValue(field, val)
	}

	return nil
}

// setFieldValue sets a struct field value from an interface{} value.
// Handles type conversion for common types including strings, numbers, bools, and time.Time.
func setFieldValue(field reflect.Value, val any) {
	if val == nil {
		return
	}

	switch field.Kind() {
	case reflect.String:
		if v, ok := val.(string); ok {
			field.SetString(v)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v, ok := val.(float64); ok { // JSON numbers are floats
			field.SetInt(int64(v))
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v, ok := val.(float64); ok {
			field.SetUint(uint64(v))
		}
	case reflect.Float32, reflect.Float64:
		if v, ok := val.(float64); ok {
			field.SetFloat(v)
		}
	case reflect.Bool:
		if v, ok := val.(bool); ok {
			field.SetBool(v)
		}
	case reflect.Struct:
		// Handle time.Time
		if field.Type() == timeType {
			if v, ok := val.(string); ok {
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					field.Set(reflect.ValueOf(t))
				}
			}
		}
	}
}

// setFloat sets a float value on a reflect.Value if it's a float type.
func setFloat(field reflect.Value, val float64) {
	if !field.CanSet() {
		return
	}
	switch field.Kind() {
	case reflect.Float32, reflect.Float64:
		field.SetFloat(val)
	}
}
