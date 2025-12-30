package gormsearch

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/meilisearch/meilisearch-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestWithCustomEncoder(t *testing.T) {
	encoderCalled := false
	customEncoder := func(model any) (map[string]any, error) {
		encoderCalled = true
		return map[string]any{"custom": "value"}, nil
	}

	gs := &GormSearch{
		config: &Config{
			Encoder: customEncoder,
		},
	}

	doc, err := gs.encodeDocument(struct{}{}, &IndexConfig{})
	assert.NoError(t, err)
	assert.True(t, encoderCalled)
	assert.Equal(t, "value", doc["custom"])
}

func TestWithCustomEncoderError(t *testing.T) {
	expectedErr := errors.New("encoder error")
	customEncoder := func(model any) (map[string]any, error) {
		return nil, expectedErr
	}

	gs := &GormSearch{
		config: &Config{
			Encoder: customEncoder,
		},
	}

	doc, err := gs.encodeDocument(struct{}{}, &IndexConfig{})
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, doc)
}

func TestWithCustomDecoder(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	decoderCalled := false
	customDecoder := func(hits []map[string]any, dest any) error {
		decoderCalled = true
		// verify hits passed
		if len(hits) != 1 {
			return errors.New("expected 1 hit")
		}
		// manually populate dest
		ptr := dest.(*[]GenericTestProduct)
		*ptr = []GenericTestProduct{{Name: "Decoded"}}
		return nil
	}

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		config: &Config{
			Decoder: customDecoder,
		},
	}
	gs.registry["products"] = &IndexConfig{IndexName: "products"}

	mockClient.On("Index", "products").Return(mockIndex)
	mockIndex.On("SearchWithContext", mock.Anything, "query", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits: []meilisearch.Hit{{"id": json.RawMessage(`1`)}},
	}, nil)

	results, err := SearchFor[GenericTestProduct](gs, "query")
	assert.NoError(t, err)
	assert.True(t, decoderCalled)
	assert.Len(t, results.Hits, 1)
	assert.Equal(t, "Decoded", results.Hits[0].Name)
}

func TestWithCustomDecoderError(t *testing.T) {
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	expectedErr := errors.New("decoder error")
	customDecoder := func(hits []map[string]any, dest any) error {
		return expectedErr
	}

	gs := &GormSearch{
		client:   mockClient,
		registry: make(map[string]*IndexConfig),
		config: &Config{
			Decoder: customDecoder,
		},
	}
	gs.registry["products"] = &IndexConfig{IndexName: "products"}

	mockClient.On("Index", "products").Return(mockIndex)
	mockIndex.On("SearchWithContext", mock.Anything, "query", mock.Anything).Return(&meilisearch.SearchResponse{
		Hits: []meilisearch.Hit{{"id": json.RawMessage(`1`)}},
	}, nil)

	results, err := SearchFor[GenericTestProduct](gs, "query")
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, results)
}
