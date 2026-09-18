package gormsearch

import (
	"bytes"
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

	if v := reflectValue(model); !v.IsValid() || v.Kind() != reflect.Struct {
		return nil, ErrInvalidModel
	}
	return gs.toDocument(model, config), nil
}

// toDocument converts a GORM model to a Meilisearch document using cached extractors.
func (gs *GormSearch) toDocument(model any, config *IndexConfig) map[string]any {
	// Optimization: Pre-allocate map with capacity hint
	doc := make(map[string]any, len(config.FieldExtractors))

	v := reflectValue(model)
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return doc
	}

	for _, extractor := range config.FieldExtractors {
		// FieldByIndexErr safely handles nil embedded pointer structs
		val, err := v.FieldByIndexErr(extractor.FieldIndex)
		if err != nil {
			continue
		}

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

// extractID returns the primary key of a model as a Meilisearch document id.
// An empty string means the key could not be resolved (see idFromValue).
func extractID(model any, config *IndexConfig) string {
	_, id := idFromValue(reflectValue(model), config)
	return id
}

// idFromValue returns the primary key of a struct value both as its raw Go
// value and as a string.
//
// The raw value is what SQL conditions must be built from: stringifying it
// first breaks non-numeric keys, because GORM reads a non-numeric string
// condition as raw SQL rather than as a primary key.
//
// A zero key yields "" so callers skip it. A zero key means the statement
// identified its rows by condition rather than by value, and acting on it
// would target the wrong document (id "0").
func idFromValue(v reflect.Value, config *IndexConfig) (any, string) {
	field := primaryKeyField(v, config)
	if !field.IsValid() || field.IsZero() {
		return nil, ""
	}
	return field.Interface(), valToString(field)
}

// primaryKeyField locates the primary key field, preferring the index path
// cached at parse time and falling back to ID / Model.ID lookups.
func primaryKeyField(v reflect.Value, config *IndexConfig) reflect.Value {
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return reflect.Value{}
	}

	if config != nil && len(config.IDFieldIndices) > 0 {
		// FieldByIndexErr safely handles nil embedded pointer structs
		field, err := v.FieldByIndexErr(config.IDFieldIndices)
		if err != nil {
			return reflect.Value{}
		}
		return field
	}

	// Fallback to legacy lookup (should rarely happen if parsed correctly)
	if field := v.FieldByName("ID"); field.IsValid() {
		return field
	}
	if model := v.FieldByName("Model"); model.IsValid() && model.Kind() == reflect.Struct {
		return model.FieldByName("ID")
	}
	return reflect.Value{}
}

// valToString converts a reflect.Value to its string representation.
// Switching on Kind rather than the concrete type keeps named key types
// (type UserID uint64) on the allocation-free path.
func valToString(v reflect.Value) string {
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Invalid:
		return ""
	default:
		// Covers Stringer keys such as uuid.UUID.
		return fmt.Sprint(v.Interface())
	}
}

// isSoftDeleted checks if a model has been soft deleted (DeletedAt is set).
func isSoftDeleted(model any, config *IndexConfig) bool {
	v := reflectValue(model)
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return false
	}

	// Optimization: Use cached field index if available
	if config != nil && len(config.DeletedAtIndex) > 0 {
		deletedAt, err := v.FieldByIndexErr(config.DeletedAtIndex)
		if err != nil {
			return false
		}
		return isDeletedAtSet(deletedAt)
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
	name := structTypeName(reflect.TypeOf(model))
	return name != "" && name == config.ModelType
}

// structType returns the struct type t ultimately refers to, unwrapping
// pointers and slices/arrays. Batch statements hand GORM a *[]Product, so the
// element type is what identifies the registered model. Returns nil when there
// is no struct underneath.
func structType(t reflect.Type) reflect.Type {
	for t != nil {
		switch t.Kind() {
		case reflect.Ptr, reflect.Slice, reflect.Array:
			t = t.Elem()
		case reflect.Struct:
			return t
		default:
			return nil
		}
	}
	return nil
}

// structTypeName returns the bare name of the struct t refers to, or "".
func structTypeName(t reflect.Type) string {
	if st := structType(t); st != nil {
		return st.Name()
	}
	return ""
}

// typeKey returns the package-qualified name of the struct t refers to. It is
// the registry key: the bare type name alone would let two models named
// Product, from different packages, silently share one entry.
func typeKey(t reflect.Type) string {
	st := structType(t)
	if st == nil || st.Name() == "" {
		return ""
	}
	if pkg := st.PkgPath(); pkg != "" {
		return pkg + "." + st.Name()
	}
	return st.Name()
}

// eachStructValue calls fn for every struct in model, which may be a struct, a
// pointer to one, or a (pointer to a) slice or array of either. This is what
// lets batch writes fan out to one job per record.
func eachStructValue(model any, fn func(reflect.Value)) {
	v := reflect.ValueOf(model)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		fn(v)
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			elem := v.Index(i)
			for elem.Kind() == reflect.Ptr && !elem.IsNil() {
				elem = elem.Elem()
			}
			if elem.Kind() == reflect.Struct {
				fn(elem)
			}
		}
	}
}

// decodeDocument populates a struct from a map using cached field extractors.
// This avoids the JSON roundtrip (map -> json -> struct) which is very expensive.
func (gs *GormSearch) decodeDocument(doc map[string]any, dest any) error {
	// 1. Get the IndexConfig for this type
	// We can infer the index name from the type of dest (which is a pointer to the struct)

	// Unpack pointer
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return ErrInvalidDest
	}
	v = v.Elem() // Now we have the struct

	// Find config
	// Optimization: Use registryByType for O(1) lookup
	config := gs.configForType(v.Type())

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

		// FieldByIndexErr safely handles nil embedded pointer structs
		field, err := v.FieldByIndexErr(extractor.FieldIndex)
		if err != nil || !field.CanSet() {
			continue
		}

		// Handle Geo fields specifically if implemented,
		// but standard Meilisearch geo is _geo: {lat, lng}
		// Our custom extractor handles flat geo fields in struct -> nested json.
		// Reverse mapping (JSON -> Struct) for Geo:
		if extractor.IsGeo {
			if geoMap, ok := val.(map[string]any); ok {
				// GeoLatIndex/GeoLngIndex are relative to the geo struct,
				// which is the field we already traversed to above.
				if lat, ok := geoMap["lat"].(float64); ok && len(extractor.GeoLatIndex) > 0 {
					setFloat(field.FieldByIndex(extractor.GeoLatIndex), lat)
				}
				if lng, ok := geoMap["lng"].(float64); ok && len(extractor.GeoLngIndex) > 0 {
					setFloat(field.FieldByIndex(extractor.GeoLngIndex), lng)
				}
			}
			continue
		}

		// Set value based on kind
		setFieldValue(field, val)
	}

	return nil
}

// stringSliceType is cached to fast-path []string decoding.
var stringSliceType = reflect.TypeOf([]string(nil))

// setFieldValue sets a struct field value from an interface{} value.
// Handles type conversion for common types including strings, numbers, bools,
// time.Time, and falls back to a JSON round-trip for complex types
// (slices, maps, pointers, nested structs) so they are not silently dropped.
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
			return
		}
		setFieldJSON(field, val)
	case reflect.Slice:
		// Fast path for the common []string case
		if field.Type() == stringSliceType {
			if arr, ok := val.([]any); ok {
				strs := make([]string, 0, len(arr))
				for _, item := range arr {
					if s, ok := item.(string); ok {
						strs = append(strs, s)
					}
				}
				field.Set(reflect.ValueOf(strs))
				return
			}
		}
		setFieldJSON(field, val)
	case reflect.Map, reflect.Ptr:
		setFieldJSON(field, val)
	}
}

// setFieldJSON decodes val into field via a JSON round-trip.
// Used for complex types (slices, maps, pointers, nested structs) that the
// scalar fast paths don't cover. Type mismatches are silently ignored,
// consistent with the other decode paths.
func setFieldJSON(field reflect.Value, val any) {
	data, err := json.Marshal(val)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, field.Addr().Interface())
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

// configForType returns the registered config for a model type, if any.
func (gs *GormSearch) configForType(t reflect.Type) *IndexConfig {
	key := typeKey(t)
	if key == "" {
		return nil
	}
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.registryByType[key]
}

// jsonDecoder returns the configured JSON decoder, defaulting to encoding/json.
func (gs *GormSearch) jsonDecoder() func([]byte, any) error {
	if gs.config != nil && gs.config.JSONDecoder != nil {
		return gs.config.JSONDecoder
	}
	return json.Unmarshal
}

// jsonNull is compared against raw hit values to leave absent fields at their
// zero value rather than writing an explicit null through.
var jsonNull = []byte("null")

// decodeRawDocument populates dest from a Meilisearch hit, decoding every field
// straight from its raw JSON.
//
// Decoding from the raw bytes rather than from an intermediate map[string]any
// is both faster and lossless: routing a document through `any` turns every
// JSON number into a float64, which silently corrupts integers beyond 2^53
// (Snowflake-style IDs, timestamps in nanoseconds), and forces complex fields
// through an extra marshal/unmarshal round trip.
func (gs *GormSearch) decodeRawDocument(hit map[string]json.RawMessage, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return ErrInvalidDest
	}
	v = v.Elem()

	unmarshal := gs.jsonDecoder()

	config := gs.configForType(v.Type())
	if config == nil || v.Kind() != reflect.Struct {
		// Unregistered type: fall back to encoding/json field matching.
		data, err := json.Marshal(hit)
		if err != nil {
			return err
		}
		return unmarshal(data, dest)
	}

	for i := range config.FieldExtractors {
		extractor := &config.FieldExtractors[i]

		raw, ok := hit[extractor.JSONName]
		if !ok || len(raw) == 0 || bytes.Equal(raw, jsonNull) {
			continue
		}

		// FieldByIndexErr safely handles nil embedded pointer structs
		field, err := v.FieldByIndexErr(extractor.FieldIndex)
		if err != nil || !field.CanSet() {
			continue
		}

		if extractor.IsGeo {
			decodeGeoRaw(field, raw, extractor, unmarshal)
			continue
		}

		// A document whose shape has drifted from the struct shouldn't fail
		// the whole search, so per-field type mismatches are skipped.
		_ = unmarshal(raw, field.Addr().Interface())
	}

	return nil
}

// decodeGeoRaw maps Meilisearch's nested _geo object back onto the flat
// lat/lng fields of a geo struct.
func decodeGeoRaw(field reflect.Value, raw []byte, extractor *FieldExtractor, unmarshal func([]byte, any) error) {
	if field.Kind() != reflect.Struct ||
		len(extractor.GeoLatIndex) == 0 || len(extractor.GeoLngIndex) == 0 {
		return
	}

	var geo struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	}
	if err := unmarshal(raw, &geo); err != nil {
		return
	}

	setFloat(field.FieldByIndex(extractor.GeoLatIndex), geo.Lat)
	setFloat(field.FieldByIndex(extractor.GeoLngIndex), geo.Lng)
}
