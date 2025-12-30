package gormsearch

import (
	"context"
	"reflect"
	"time"

	"gorm.io/gorm"
)

// registerCallbacks sets up GORM callbacks for a registered model.
func (gs *GormSearch) registerCallbacks(config *IndexConfig) {
	prefix := "gormsearch:" + config.IndexName

	gs.db.Callback().Create().After("gorm:create").Register(prefix+":create", func(db *gorm.DB) {
		gs.syncDocument(db, config, opCreate)
	})

	gs.db.Callback().Update().After("gorm:update").Register(prefix+":update", func(db *gorm.DB) {
		gs.syncDocument(db, config, opUpdate)
	})

	gs.db.Callback().Delete().After("gorm:delete").Register(prefix+":delete", func(db *gorm.DB) {
		gs.syncDocument(db, config, opDelete)
	})
}

type operation int

const (
	opCreate operation = iota
	opUpdate
	opDelete
)

func (o operation) String() string {
	switch o {
	case opCreate:
		return "create"
	case opUpdate:
		return "update"
	case opDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// syncDocument handles document synchronization for all operations.
func (gs *GormSearch) syncDocument(db *gorm.DB, config *IndexConfig, op operation) {
	if db.Error != nil || db.Statement.Model == nil {
		return
	}

	if !isMatchingModel(db.Statement.Model, config) {
		return
	}

	switch op {
	case opCreate, opUpdate:
		id := extractID(db.Statement.Model)
		if id == "" {
			return
		}

		// Reload model from DB to ensure all fields are present (partial update fix)
		modelType := reflect.TypeOf(db.Statement.Model)
		if modelType.Kind() == reflect.Ptr {
			modelType = modelType.Elem()
		}
		loadedModel := reflect.New(modelType).Interface()

		// Use the transaction db to find the record
		if err := db.Session(&gorm.Session{NewDB: true}).Unscoped().First(loadedModel, id).Error; err != nil {
			return
		}

		// Check for soft delete on the reloaded model
		if isSoftDeleted(loadedModel) {
			// Soft deleted - remove from Meilisearch
			// Soft deleted - remove from Meilisearch
			job := Job{
				IndexName: config.IndexName,
				Operation: "delete",
				ID:        id,
			}
			if err := gs.config.Dispatcher.Dispatch(context.Background(), job); err != nil {
				if gs.config.OnError != nil {
					gs.config.OnError("dispatch", err)
				}
			}
			return
		}

		doc, err := gs.encodeDocument(loadedModel, config)
		if err != nil {
			if gs.config.OnError != nil {
				gs.config.OnError("encode", err)
			}
			return
		}
		if doc == nil {
			return
		}

		// Dispatch job
		job := Job{
			IndexName: config.IndexName,
			Document:  doc,
			ID:        id,
		}
		if op == opCreate {
			job.Operation = "create"
		} else {
			job.Operation = "update"
		}

		if err := gs.config.Dispatcher.Dispatch(context.Background(), job); err != nil {
			if gs.config.OnError != nil {
				gs.config.OnError("dispatch", err)
			}
		}

	case opDelete:
		id := extractID(db.Statement.Model)
		if id == "" {
			return
		}

		// Dispatch job
		job := Job{
			IndexName: config.IndexName,
			Operation: "delete",
			ID:        id,
		}

		if err := gs.config.Dispatcher.Dispatch(context.Background(), job); err != nil {
			if gs.config.OnError != nil {
				gs.config.OnError("dispatch", err)
			}
		}
	}
}

// execWithRetry executes a function with retry logic.
func (gs *GormSearch) execWithRetry(op operation, fn func() error) {
	if gs.config.Async {
		go gs.safeGo(op, func() error {
			return gs.retry(fn)
		})
	} else {
		gs.retry(fn)
	}
}

// retry executes a function with exponential backoff.
func (gs *GormSearch) retry(fn func() error) error {
	maxRetries := gs.config.MaxRetries
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

	// Call error handler if all retries failed
	if lastErr != nil && gs.config.OnError != nil {
		gs.config.OnError("retry_exhausted", lastErr)
	}

	return lastErr
}

// safeGo executes a function with panic recovery and worker pool.
func (gs *GormSearch) safeGo(op operation, fn func() error) {
	gs.pool.acquire()
	defer gs.pool.release()

	defer func() {
		if r := recover(); r != nil && gs.config.OnError != nil {
			gs.config.OnError(op.String(), &panicError{value: r})
		}
	}()

	if err := fn(); err != nil && gs.config.OnError != nil {
		gs.config.OnError(op.String(), err)
	}
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
