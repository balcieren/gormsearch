# 🔰 Basic GormSearch Example

This indicates a simple, dependency-free example showing how to integrate `gormsearch` with `gorm` using **SQLite**.

It demonstrates the core workflow: Registering a model, Indexing data, and Searching.

## 📋 Prerequisites

- **Go 1.18+**
- A locally running [Meilisearch](https://www.meilisearch.com/docs/learn/getting_started/quick_start) instance.

## 🚀 How to Run

### 1. Start Meilisearch

If you don't have a local Meilisearch instance running, start one quickly with Docker:

```bash
docker run -p 7700:7700 getmeili/meilisearch:latest
```

### 2. Run the Example

Execute the Go program:

```bash
go run main.go
```

The program will:
1. Connect to a local SQLite database (`test.db`).
2. Connect to your local Meilisearch instance at `http://localhost:7700`.
3. **Register** the `User` model with `gormsearch`.
4. **Index** a new user ("John Doe") automatically via GORM hooks.
5. **Search** for "John" and print the typed results.

## 💡 Code Highlights

- **Model Definition**:
  ```go
  type User struct {
      Name  string `meili:"searchable"`
      Email string `meili:"filterable"`
  }
  ```
  Struct tags control which fields are indexed.

- **Registration**:
  ```go
  gs.Register(User{})
  ```
  This single line sets up the index settings and hooks.

- **Typed Search**:
  ```go
  results, err := gormsearch.SearchFor[User](gs, "John")
  ```
  Returns `*TypedSearchResult[User]` containing `Hits` as `[]User`.
