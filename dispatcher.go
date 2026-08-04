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
	async      bool
}

// NewDefaultDispatcher creates a new in-memory dispatcher.
// The dispatcher runs asynchronously by default; GormSearch.New overrides
// this based on the WithAsync option.
func NewDefaultDispatcher(client meilisearch.ServiceManager, maxWorkers, maxRetries int, onError func(op string, err error)) *DefaultDispatcher {
	return &DefaultDispatcher{
		client:     client,
		pool:       newWorkerPool(maxWorkers),
		maxRetries: maxRetries,
		onError:    onError,
		async:      true,
	}
}

// Dispatch executes the job with context support.
// The context is used for cancellation - if cancelled before the job starts,
// the job will not be executed.
//
// In async mode the job runs in a goroutine. A worker slot is acquired
// BEFORE the goroutine is spawned, so the number of in-flight jobs (and
// goroutines) is bounded by the pool size: under sustained load Dispatch
// blocks until a slot frees up, applying natural backpressure instead of
// piling up unbounded goroutines.
//
// In sync mode (WithAsync(false)) the job is executed inline and any
// error is returned to the caller.
func (d *DefaultDispatcher) Dispatch(ctx context.Context, job Job) error {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Synchronous mode: execute inline without goroutine overhead
	if !d.async {
		if err := d.run(ctx, job); err != nil {
			if d.onError != nil {
				d.onError(job.Operation, err)
			}
			return err
		}
		return nil
	}

	// Acquire a worker slot before spawning the goroutine (see doc above).
	if err := d.pool.acquireCtx(ctx); err != nil {
		return err
	}

	go func() {
		defer d.pool.release()
		d.safeGo(ctx, job, func() error {
			return d.retryWithContext(ctx, func() error {
				return d.executeWithContext(ctx, job)
			})
		})
	}()
	return nil
}

// ExecuteWithContext applies a job to Meilisearch with context support.
// Use this function in your external worker (Consumer) to process jobs received from the queue.
func ExecuteWithContext(ctx context.Context, client meilisearch.ServiceManager, job Job) error {
	if client == nil {
		return nil // No-op if client is nil
	}

	index := client.Index(job.IndexName)

	switch job.Operation {
	case "create":
		_, err := index.AddDocumentsWithContext(ctx, []any{job.Document}, nil)
		return err
	case "update":
		_, err := index.UpdateDocumentsWithContext(ctx, []any{job.Document}, nil)
		return err
	case "delete":
		_, err := index.DeleteDocumentWithContext(ctx, job.ID, nil)
		return err
	}
	return nil
}

// Execute applies a job to Meilisearch (backward compatible, no context).
// Use this function in your external worker (Consumer) to process jobs received from the queue.
func Execute(client meilisearch.ServiceManager, job Job) error {
	return ExecuteWithContext(context.Background(), client, job)
}

func (d *DefaultDispatcher) executeWithContext(ctx context.Context, job Job) error {
	return ExecuteWithContext(ctx, d.client, job)
}

// safeGo executes a function with panic recovery, reporting errors via onError.
// The caller is responsible for worker pool acquisition.
func (d *DefaultDispatcher) safeGo(ctx context.Context, job Job, fn func() error) {
	defer func() {
		if r := recover(); r != nil && d.onError != nil {
			d.onError(job.Operation, &panicError{value: r})
		}
	}()

	if err := fn(); err != nil && d.onError != nil {
		d.onError(job.Operation, err)
	}
}

// run executes a job with retries and panic recovery, returning any error.
// Used by the synchronous dispatch path.
func (d *DefaultDispatcher) run(ctx context.Context, job Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &panicError{value: r}
		}
	}()
	return d.retryWithContext(ctx, func() error {
		return d.executeWithContext(ctx, job)
	})
}

// retryWithContext executes a function with exponential backoff and context support.
func (d *DefaultDispatcher) retryWithContext(ctx context.Context, fn func() error) error {
	maxRetries := d.maxRetries
	if maxRetries <= 0 {
		maxRetries = 3 // Default
	}

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		// Check context before each attempt
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return lastErr
			}
			return ctx.Err()
		default:
		}

		if err := fn(); err != nil {
			lastErr = err
			// Exponential backoff: 100ms, 200ms, 400ms...
			backoff := time.Duration(100*(1<<i)) * time.Millisecond

			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return lastErr
			case <-timer.C:
			}
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

// Consume deserializes and converts a job from an external queue.
// This is a helper for consumers.
func Consume(client meilisearch.ServiceManager, data []byte) error {
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return err
	}
	return Execute(client, job)
}
