package gormsearch

import (
	"testing"
)

func TestFilterBuilder(t *testing.T) {
	tests := []struct {
		name     string
		build    func() string
		expected string
	}{
		{
			name: "Eq",
			build: func() string {
				return NewFilter().Where("category").Eq("electronics").Build()
			},
			expected: "category = 'electronics'",
		},
		{
			name: "Gt",
			build: func() string {
				return NewFilter().Where("price").Gt(100).Build()
			},
			expected: "price > 100",
		},
		{
			name: "And",
			build: func() string {
				return NewFilter().
					Where("category").Eq("books").
					And().
					Where("price").Lte(20).
					Build()
			},
			expected: "category = 'books' AND price <= 20",
		},
		{
			name: "Or",
			build: func() string {
				return NewFilter().
					Where("status").Eq("active").
					Or().
					Where("status").Eq("pending").
					Build()
			},
			expected: "status = 'active' OR status = 'pending'",
		},
		{
			name: "Group",
			build: func() string {
				return NewFilter().
					Where("public").Eq(true).
					And().
					Group(func(sub *FilterBuilder) {
						sub.Where("author").Eq("john").
							Or().
							Where("author").Eq("jane")
					}).
					Build()
			},
			expected: "public = true AND (author = 'john' OR author = 'jane')",
		},
		{
			name: "GeoRadius",
			build: func() string {
				return NewFilter().GeoRadius(45.5, 10.0, 2000).Build()
			},
			expected: "_geoRadius(45.500000, 10.000000, 2000.000000)",
		},
		{
			name: "In",
			build: func() string {
				return NewFilter().Where("tag").In("red", "blue").Build()
			},
			expected: "tag IN ['red', 'blue']",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.build()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
