package gormsearch

import (
	"context"
	"reflect"

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

	id := extractID(db.Statement.Model)
	if id == "" {
		return
	}

	var job Job
	job.IndexName = config.IndexName
	job.ID = id

	switch op {
	case opCreate, opUpdate:
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
			job.Operation = "delete"
			break
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

		job.Document = doc
		if op == opCreate {
			job.Operation = "create"
		} else {
			job.Operation = "update"
		}

	case opDelete:
		job.Operation = "delete"
	}

	// Dispatch job
	if err := gs.config.Dispatcher.Dispatch(context.Background(), job); err != nil {
		if gs.config.OnError != nil {
			gs.config.OnError("dispatch", err)
		}
	}
}
