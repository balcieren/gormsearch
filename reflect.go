package gormsearch

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// encodeDocument encodes a model using custom encoder or default reflection.
func (gs *GormSearch) encodeDocument(model any, config *IndexConfig) (map[string]any, error) {
	if gs.config != nil && gs.config.Encoder != nil {
		return gs.config.Encoder(model)
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
			if geo, ok := extractGeo(val); ok {
				doc[extractor.JSONName] = geo
			}
			continue
		}

		doc[extractor.JSONName] = val.Interface()
	}

	return doc
}

// extractGeo attempts to extract lat/lng from a struct value.
func extractGeo(v reflect.Value) (map[string]float64, bool) {
	v = reflectValue(v.Interface())
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return nil, false
	}

	var lat, lng float64
	var foundLat, foundLng bool
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		name := strings.ToLower(t.Field(i).Name)
		val := v.Field(i)

		// Try to convert to float
		fVal, err := toFloat(val)
		if err != nil {
			continue
		}

		if name == "lat" || name == "latitude" {
			lat = fVal
			foundLat = true
		} else if name == "lng" || name == "lon" || name == "longitude" {
			lng = fVal
			foundLng = true
		}
	}

	if foundLat && foundLng {
		return map[string]float64{"lat": lat, "lng": lng}, true
	}
	return nil, false
}

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
