package gormsearch

import (
	"testing"
)

func TestWithBatchSize(t *testing.T) {
	config := &Config{}
	opt := WithBatchSize(50)
	opt(config)

	if config.BatchSize != 50 {
		t.Errorf("expected BatchSize 50, got %d", config.BatchSize)
	}
}

func TestWithAsync(t *testing.T) {
	config := &Config{}

	opt := WithAsync(true)
	opt(config)
	if !config.Async {
		t.Error("expected Async true")
	}

	opt = WithAsync(false)
	opt(config)
	if config.Async {
		t.Error("expected Async false")
	}
}

func TestWithLimit(t *testing.T) {
	opts := &SearchOptions{}
	opt := WithLimit(100)
	opt(opts)

	if opts.Limit != 100 {
		t.Errorf("expected Limit 100, got %d", opts.Limit)
	}
}

func TestWithOffset(t *testing.T) {
	opts := &SearchOptions{}
	opt := WithOffset(20)
	opt(opts)

	if opts.Offset != 20 {
		t.Errorf("expected Offset 20, got %d", opts.Offset)
	}
}

func TestWithFilter(t *testing.T) {
	opts := &SearchOptions{}
	opt := WithFilter("category = 'electronics'")
	opt(opts)

	if opts.Filter != "category = 'electronics'" {
		t.Errorf("expected Filter \"category = 'electronics'\", got %s", opts.Filter)
	}
}

func TestWithSort(t *testing.T) {
	opts := &SearchOptions{}
	opt := WithSort("price:asc", "created_at:desc")
	opt(opts)

	if len(opts.Sort) != 2 {
		t.Errorf("expected 2 sort fields, got %d", len(opts.Sort))
	}

	if opts.Sort[0] != "price:asc" {
		t.Errorf("expected first sort 'price:asc', got %s", opts.Sort[0])
	}

	if opts.Sort[1] != "created_at:desc" {
		t.Errorf("expected second sort 'created_at:desc', got %s", opts.Sort[1])
	}
}

func TestMultipleOptions(t *testing.T) {
	opts := &SearchOptions{}

	options := []SearchOption{
		WithLimit(50),
		WithOffset(10),
		WithFilter("price > 100"),
		WithSort("name:asc"),
	}

	for _, opt := range options {
		opt(opts)
	}

	if opts.Limit != 50 {
		t.Errorf("expected Limit 50, got %d", opts.Limit)
	}
	if opts.Offset != 10 {
		t.Errorf("expected Offset 10, got %d", opts.Offset)
	}
	if opts.Filter != "price > 100" {
		t.Errorf("expected Filter 'price > 100', got %s", opts.Filter)
	}
	if len(opts.Sort) != 1 || opts.Sort[0] != "name:asc" {
		t.Errorf("expected Sort ['name:asc'], got %v", opts.Sort)
	}
}
