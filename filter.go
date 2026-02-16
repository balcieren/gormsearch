package gormsearch

import (
	"fmt"
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
	if len(f.parts) > 0 {
		f.parts = append(f.parts, "AND")
	}
	return f
}

// Or adds an OR operator.
func (f *FilterBuilder) Or() *FilterBuilder {
	if len(f.parts) > 0 {
		f.parts = append(f.parts, "OR")
	}
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
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s = %v", c.field, formatValue(value)))
	return c.builder
}

// Neq adds a non-equality check (!=).
func (c *Condition) Neq(value any) *FilterBuilder {
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s != %v", c.field, formatValue(value)))
	return c.builder
}

// Gt adds a greater than check (>).
func (c *Condition) Gt(value any) *FilterBuilder {
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s > %v", c.field, formatValue(value)))
	return c.builder
}

// Gte adds a greater than or equal check (>=).
func (c *Condition) Gte(value any) *FilterBuilder {
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s >= %v", c.field, formatValue(value)))
	return c.builder
}

// Lt adds a less than check (<).
func (c *Condition) Lt(value any) *FilterBuilder {
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s < %v", c.field, formatValue(value)))
	return c.builder
}

// Lte adds a less than or equal check (<=).
func (c *Condition) Lte(value any) *FilterBuilder {
	c.builder.parts = append(c.builder.parts, fmt.Sprintf("%s <= %v", c.field, formatValue(value)))
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

// formatValue formats the value for Meilisearch filter syntax.
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("'%s'", strings.ReplaceAll(val, "'", "\\'"))
	default:
		return fmt.Sprintf("%v", val)
	}
}

// Not wraps a sub-filter with NOT.
func (f *FilterBuilder) Not(fn func(*FilterBuilder)) *FilterBuilder {
	sub := NewFilter()
	fn(sub)
	if len(sub.parts) > 0 {
		f.parts = append(f.parts, "NOT "+sub.Build())
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
	f.parts = append(f.parts, fmt.Sprintf("_geoRadius(%f, %f, %f)", lat, lng, distanceInMeters))
	return f
}
