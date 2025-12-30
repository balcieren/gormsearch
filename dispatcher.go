package gormsearch

import (
	"context"
	"encoding/json"
	"time"

	"github.com/meilisearch/meilisearch-go"
)

// DefaultDispatcher handles jobs using an in-memory worker pool.
type DefaultDispatcher struct {
	client     meilisearch.ServiceManager
	pool       *workerPool
	maxRetries int
	onError    func(op string, err error)
}

// NewDefaultDispatcher creates a new in-memory dispatcher.
func NewDefaultDispatcher(client meilisearch.ServiceManager, maxWorkers, maxRetries int, onError func(op string, err error)) *DefaultDispatcher {
	return &DefaultDispatcher{
		client:     client,
		pool:       newWorkerPool(maxWorkers),
		maxRetries: maxRetries,
		onError:    onError,
	}
}

// Dispatch executes the job asynchronously.
func (d *DefaultDispatcher) Dispatch(ctx context.Context, job Job) error {
	// In-memory dispatcher ignores context cancellation for the goroutine launch itself,
	// but could pass it down if operations supported it. Use Background for async detach.
	go d.safeGo(job, func() error {
		return d.retry(func() error {
			return d.execute(job)
		})
	})
	return nil
}

// ExecuteJob applies a job to Meilisearch.
// Use this function in your external worker (Consumer) to process jobs received from the queue.
func ExecuteJob(client meilisearch.ServiceManager, job Job) error {
	index := client.Index(job.IndexName)

	switch job.Operation {
	case "create":
		// Wrap in []any to support both map[string]any and json.RawMessage
		_, err := index.AddDocuments([]any{job.Document}, nil)
		return err
	case "update":
		_, err := index.UpdateDocuments([]any{job.Document}, nil)
		return err
	case "delete":
		_, err := index.DeleteDocument(job.ID, nil)
		return err
	}
	return nil
}

func (d *DefaultDispatcher) execute(job Job) error {
	return ExecuteJob(d.client, job)
}

// safeGo executes a function with panic recovery and worker pool.
func (d *DefaultDispatcher) safeGo(job Job, fn func() error) {
	d.pool.acquire()
	defer d.pool.release()

	defer func() {
		if r := recover(); r != nil && d.onError != nil {
			d.onError(job.Operation, &panicError{value: r})
		}
	}()

	if err := fn(); err != nil && d.onError != nil {
		d.onError(job.Operation, err)
	}
}

// retry executes a function with exponential backoff.
func (d *DefaultDispatcher) retry(fn func() error) error {
	maxRetries := d.maxRetries
	if maxRetries <= 0 {
		maxRetries = 3 // Default
	}

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if err := fn(); err != nil {
			lastErr = err
			// Exponential backoff: 100ms, 200ms, 400ms...
			time.Sleep(time.Duration(100*(1<<i)) * time.Millisecond)
			continue
		}
		return nil
	}
	return lastErr
}

// panicError wraps a panic value as an error.
type panicError struct {
	value any
}

func (e *panicError) Error() string {
	return "panic: " + toString(e.value)
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return "unknown panic"
}

// QueueDispatcher is a dispatcher that publishes jobs to an external queue.
type QueueDispatcher struct {
	encoder func(v any) ([]byte, error)
	publish func(ctx context.Context, data []byte) error
}

// Dispatch marshals the job and publishes it.
func (d *QueueDispatcher) Dispatch(ctx context.Context, job Job) error {
	data, err := d.encoder(job)
	if err != nil {
		return err
	}
	return d.publish(ctx, data)
}

// ConsumeJob deserializes and converts a job from an external queue.
// This is a helper for consumers.
func ConsumeJob(client meilisearch.ServiceManager, data []byte) error {
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return err
	}
	return ExecuteJob(client, job)
}
