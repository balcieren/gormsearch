package main

import (
	"log"
	"time"

	"github.com/balcieren/gormsearch"
	"github.com/meilisearch/meilisearch-go"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Name  string `meili:"searchable"`
	Email string `meili:"filterable"`
}

func (u User) IndexName() string {
	return "users"
}

func main() {
	// Connect to SQLite
	db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}

	// Connect to Meilisearch (assumes local instance running on port 7700)
	client := meilisearch.New("http://localhost:7700", meilisearch.WithAPIKey(""))

	// Init gormsearch
	gs, err := gormsearch.New(db, client)
	if err != nil {
		log.Fatalf("failed to initialize gormsearch: %v", err)
	}

	// Register model
	if err := gs.Register(User{}); err != nil {
		log.Fatalf("failed to register model: %v", err)
	}

	// Migrate schema
	if err := db.AutoMigrate(&User{}); err != nil {
		log.Fatalf("failed to migrate schema: %v", err)
	}

	// Create user (automatically indexed)
	user := User{Name: "John Doe", Email: "john@example.com"}
	if err := db.Create(&user).Error; err != nil {
		log.Printf("failed to create user: %v", err)
	}

	// Give it a moment to index
	time.Sleep(100 * time.Millisecond)

	// Search
	results, err := gormsearch.SearchFor[User](gs, "John")
	if err != nil {
		log.Printf("search failed: %v", err)
	}

	log.Printf("Found %d users matching 'John'", len(results.Hits))
}
