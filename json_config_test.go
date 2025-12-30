package gormsearch

import (
	"encoding/json"
	"testing"

	"github.com/meilisearch/meilisearch-go"
	"github.com/stretchr/testify/mock"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestReflectJSONEncoder(t *testing.T) {
	// Setup DB
	db, _ := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	db.AutoMigrate(&MockProduct{})

	// Setup Mock Client
	mockClient := new(MockClient)
	mockIndex := new(MockIndex)

	// Mock Expectations
	mockClient.On("Index", "mock_products").Return(mockIndex)
	mockClient.On("CreateIndex", mock.Anything).Return(&meilisearch.TaskInfo{}, nil) // Fixed: CreateIndex returns (*TaskInfo, error)
	// UpdateSettings usually returns (*TaskInfo, error) in recent SDKs, verify signature if needed.
	// Assuming meilisearch-go v0.25+
	mockIndex.On("UpdateSettings", mock.Anything).Return(&meilisearch.TaskInfo{}, nil)

	// Variable to check if our encoder was called
	encoderCalled := false

	// Setup GormSearch with custom JSON encoder
	gs, _ := New(db, mockClient,
		WithJSONEncoder(func(v any) ([]byte, error) {
			encoderCalled = true
			return json.Marshal(v)
		}),
	)

	// Register model
	if err := gs.Register(&MockProduct{}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	model := &MockProduct{Name: "Test"}
	config, _ := parseModel(model)

	// Test encodeDocument
	docAny, err := gs.encodeDocument(model, config)
	if err != nil {
		t.Fatalf("encodeDocument failed: %v", err)
	}

	if !encoderCalled {
		t.Error("Custom JSONEncoder was not called")
	}

	// Verify doc is json.RawMessage
	if _, ok := docAny.(json.RawMessage); !ok {
		t.Errorf("Expected json.RawMessage, got %T", docAny)
	}
}
