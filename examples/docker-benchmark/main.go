package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/balcieren/gormsearch"
	"github.com/meilisearch/meilisearch-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Product struct {
	gorm.Model
	Name        string  `json:"name" meili:"searchable,filterable"`
	Description string  `json:"description" meili:"searchable"`
	Price       float64 `json:"price" meili:"filterable,sortable"`
	Category    string  `json:"category" meili:"filterable"`
}

func (p Product) IndexName() string {
	return "products"
}

func main() {
	// 1. Get configuration from env
	meiliHost := os.Getenv("MEILISEARCH_HOST")
	meiliKey := os.Getenv("MEILISEARCH_API_KEY")
	pgDSN := os.Getenv("POSTGRES_DSN")

	if meiliHost == "" || pgDSN == "" {
		log.Fatal("MEILISEARCH_HOST and POSTGRES_DSN must be set")
	}

	// 2. Connect to Postgres
	db, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// 3. Connect to Meilisearch
	client := meilisearch.New(meiliHost, meilisearch.WithAPIKey(meiliKey))

	// 4. Initialize gormsearch
	gs, err := gormsearch.New(db, client)
	if err != nil {
		log.Fatalf("Failed to initialize gormsearch: %v", err)
	}

	// Register the model (important for index creation)
	if err := gs.Register(Product{}); err != nil {
		log.Fatalf("Failed to register model: %v", err)
	}

	// Migrate the schema (creates table)
	if err := db.AutoMigrate(&Product{}); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	log.Println("Database and Search Engine connected. Starting benchmarks...")

	// 5. Run Benchmarks
	runIndexBenchmark(db, 1000)
	runSearchBenchmark(gs, 1000)
}

func runIndexBenchmark(db *gorm.DB, count int) {
	log.Printf("Starting Index Benchmark (Create %d records)...", count)

	start := time.Now()

	// Batch create for speed, but GORM hooks will run for each if handled correctly.
	// Note: gormsearch hooks depend on GORM hooks. Batch inserts in GORM might bypass hooks
	// unless configured otherwise, but let's do loop for guaranteed hook execution for this test
	// to stress test the async dispatcher if any, or sync indexing.

	for i := 0; i < count; i++ {
		p := Product{
			Name:        fmt.Sprintf("Product %d", i),
			Description: fmt.Sprintf("Description for product %d with some random text", i),
			Price:       rand.Float64() * 1000,
			Category:    getRandomCategory(),
		}
		if err := db.Create(&p).Error; err != nil {
			log.Printf("Error creating product %d: %v", i, err)
		}
	}

	duration := time.Since(start)
	log.Printf("Index Benchmark Completed: %d records in %v (Avg: %v/record)", count, duration, duration/time.Duration(count))
}

func runSearchBenchmark(gs *gormsearch.GormSearch, requests int) {
	// Allow some time for Meilisearch to index everything asynchronously if that's the case
	// But since gormsearch default is sync or we want to test availability...
	// Let's sleep a bit to ensure potential async tasks are done
	time.Sleep(2 * time.Second)

	log.Printf("Starting Search Benchmark (%d requests)...", requests)

	start := time.Now()

	for i := 0; i < requests; i++ {
		// Search for "Product" which should match everything, or specific numbers
		query := fmt.Sprintf("Product %d", rand.Intn(100))
		_, err := gormsearch.SearchFor[Product](gs, query)
		if err != nil {
			log.Printf("Error searching: %v", err)
		}
	}

	duration := time.Since(start)
	log.Printf("Search Benchmark Completed: %d requests in %v (Avg: %v/req)", requests, duration, duration/time.Duration(requests))
}

func getRandomCategory() string {
	categories := []string{"electronics", "clothing", "books", "home", "toys"}
	return categories[rand.Intn(len(categories))]
}
