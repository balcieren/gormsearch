package gormsearch

import (
	"fmt"
	"strconv"
	"strings"
)

// FilterBuilder helps construct complex Meilisearch filter strings.
type FilterBuilder struct {
	parts []string
}

// NewFilter creates a new FilterBuilder.
func NewFilter() *FilterBuilder {
	return &FilterBuilder{parts: make([]string, 0)}
}

// Where starts a new filter condition.
func (f *FilterBuilder) Where(field string) *Condition {
	return &Condition{
		builder: f,
		field:   field,
	}
}

// And adds an AND operator.
func (f *FilterBuilder) And() *FilterBuilder {
	return f.addOperator("AND")
}

// Or adds an OR operator.
func (f *FilterBuilder) Or() *FilterBuilder {
	return f.addOperator("OR")
}

// addOperator appends a boolean operator, ignoring it at the start of an
// expression or straight after another operator, where it would not parse.
func (f *FilterBuilder) addOperator(op string) *FilterBuilder {
	if len(f.parts) == 0 {
		return f
	}
	switch f.parts[len(f.parts)-1] {
	case "AND", "OR":
		return f
	}
	f.parts = append(f.parts, op)
	return f
}

// Group wraps the inner builder's result in parentheses.
func (f *FilterBuilder) Group(fn func(*FilterBuilder)) *FilterBuilder {
	sub := NewFilter()
	fn(sub)
	if len(sub.parts) > 0 {
		f.parts = append(f.parts, "("+sub.Build()+")")
	}
	return f
}

// Build returns the final filter string.
func (f *FilterBuilder) Build() string {
	return strings.Join(f.parts, " ")
}

// Condition represents a pending condition on a field.
type Condition struct {
	builder *FilterBuilder
	field   string
}

// Eq adds an equality check (=).
func (c *Condition) Eq(value any) *FilterBuilder {
	c.builder.parts = append(c.builder.parts, c.field+" = "+formatValue(value))
	return c.builder
}

// Neq adds a non-equality check (!=).
func (c *Condition) Neq(value any) *FilterBuilder {
	c.builder.parts = append(c.builder.parts, c.field+" != "+formatValue(value))
	return c.builder
}

// Gt adds a greater than check (>).
func (c *Condition) Gt(value any) *FilterBuilder {
	c.builder.parts = append(c.builder.parts, c.field+" > "+formatValue(value))
	return c.builder
}

// Gte adds a greater than or equal check (>=).
func (c *Condition) Gte(value any) *FilterBuilder {
	c.builder.parts = append(c.builder.parts, c.field+" >= "+formatValue(value))
	return c.builder
}

// Lt adds a less than check (<).
func (c *Condition) Lt(value any) *FilterBuilder {
	c.builder.parts = append(c.builder.parts, c.field+" < "+formatValue(value))
	return c.builder
}

// Lte adds a less than or equal check (<=).
func (c *Condition) Lte(value any) *FilterBuilder {
	c.builder.parts = append(c.builder.parts, c.field+" <= "+formatValue(value))
	return c.builder
}

// In adds an IN check.
func (c *Condition) In(values ...any) *FilterBuilder {
	formatted := make([]string, len(values))
	for i, v := range values {
		formatted[i] = formatValue(v)
	}
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s IN [%s]", c.field, strings.Join(formatted, ", ")))
	return c.builder
}

// quoteReplacer escapes values for Meilisearch's single-quoted string syntax.
// A Replacer makes one pass over the input, so the backslash it inserts when
// escaping a quote is not itself re-escaped; chained ReplaceAll calls would
// double it.
var quoteReplacer = strings.NewReplacer(`\`, `\\`, `'`, `\'`)

// formatValue formats the value for Meilisearch filter syntax.
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return "'" + quoteReplacer.Replace(val) + "'"
	case float32:
		return formatFloat(float64(val))
	case float64:
		return formatFloat(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// formatFloat renders a float in plain decimal notation. The %v verb switches
// to exponent form for large or small magnitudes ("1e+07"), which Meilisearch
// does not accept in filters.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// Not wraps a sub-filter with NOT.
//
// The sub-filter is always parenthesized: without it, NOT binds to the first
// condition only, so Not(a = 1 AND b = 2) would mean (NOT a = 1) AND b = 2.
func (f *FilterBuilder) Not(fn func(*FilterBuilder)) *FilterBuilder {
	sub := NewFilter()
	fn(sub)
	if len(sub.parts) > 0 {
		f.parts = append(f.parts, "NOT ("+sub.Build()+")")
	}
	return f
}

// IsNull adds an IS NULL check on the field.
func (c *Condition) IsNull() *FilterBuilder {
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s IS NULL", c.field))
	return c.builder
}

// IsNotNull adds an IS NOT NULL check on the field.
func (c *Condition) IsNotNull() *FilterBuilder {
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s IS NOT NULL", c.field))
	return c.builder
}

// Exists adds an EXISTS check on the field.
func (c *Condition) Exists() *FilterBuilder {
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s EXISTS", c.field))
	return c.builder
}

// NotExists adds a NOT EXISTS check on the field.
func (c *Condition) NotExists() *FilterBuilder {
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s NOT EXISTS", c.field))
	return c.builder
}

// IsEmpty adds an IS EMPTY check on the field.
func (c *Condition) IsEmpty() *FilterBuilder {
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s IS EMPTY", c.field))
	return c.builder
}

// IsNotEmpty adds an IS NOT EMPTY check on the field.
func (c *Condition) IsNotEmpty() *FilterBuilder {
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s IS NOT EMPTY", c.field))
	return c.builder
}

// GeoRadius adds a _geoRadius filter.
func (f *FilterBuilder) GeoRadius(lat, lng, distanceInMeters float64) *FilterBuilder {
	f.parts = append(f.parts, fmt.Sprintf("_geoRadius(%s, %s, %s)",
		formatFloat(lat), formatFloat(lng), formatFloat(distanceInMeters)))
	return f
}
