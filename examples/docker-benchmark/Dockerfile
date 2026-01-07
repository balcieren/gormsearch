FROM golang:1.23-alpine

WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the benchmark application
RUN go build -o benchmark ./examples/docker-benchmark

# Set the entrypoint
ENTRYPOINT ["./benchmark"]
