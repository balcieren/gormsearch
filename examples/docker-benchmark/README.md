# 🐳 Dockerized Benchmark Example

This example demonstrates how to use `gormsearch` in a high-performance environment with **PostgreSQL** and **Meilisearch**, orchestrated via **Docker Compose**.

It serves as both an integration test and a performance benchmark.

## 📋 Prerequisites

Before running the benchmark, ensure you have the following installed:

- [Docker](https://docs.docker.com/get-docker/)
- [Docker Compose](https://docs.docker.com/compose/install/)

## 🚀 How to Run

### 1. Start the Environment

Run the following command to build the containers and start the benchmark:

```bash
docker-compose up --build
```

This command will:
- ✅ Start a **Meilisearch** container (latest) on port `7700`.
- ✅ Start a **PostgreSQL** container (latest) on port `5432`.
- ✅ Build and run the **Go benchmark application**.

### 2. Observe Results

The benchmark runner will automatically execute and print the results to the console.

#### 📊 Sample Output (MacBook M1, 16GB RAM)

```text
Database and Search Engine connected. Starting benchmarks...
Starting Index Benchmark (Create 1000 records)...
Index Benchmark Completed: 1000 records in 3.948s (Avg: 3.94ms/record)

Starting Search Benchmark (1000 requests)...
Search Benchmark Completed: 1000 requests in 389ms (Avg: 0.39ms/req)
```

## ⚙️ Configuration

The environment is pre-configured in `docker-compose.yml`. Key variables include:

| Variable | Description | Value |
| :--- | :--- | :--- |
| `MEILISEARCH_HOST` | URL of Meilisearch | `http://meilisearch:7700` |
| `MEILISEARCH_API_KEY` | Master API Key | `masterKey` |
| `POSTGRES_DSN` | GORM Connection String | `host=postgres user=user ...` |
