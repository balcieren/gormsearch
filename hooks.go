package gormsearch

import (
	"context"
	"fmt"
	"reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// maxResolvedDeleteIDs caps how many primary keys a conditional delete resolves
// for index removal, so `DELETE FROM huge_table` cannot pull the whole key
// space into memory. Hitting the cap is reported through OnError.
const maxResolvedDeleteIDs = 10000

// deleteIDsKey names the statement-scoped slot holding the primary keys a
// delete is about to remove. It is filled before the delete runs (while the
// rows are still visible) and consumed afterwards.
const deleteIDsKey = "gormsearch:delete_ids"

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

// registerCallbacks installs the GORM callbacks that drive synchronization.
//
// Callbacks are installed once per GormSearch instance rather than once per
// registered model: each one resolves the model's config from the registry at
// call time, so the per-write cost stays constant no matter how many models
// are registered.
func (gs *GormSearch) registerCallbacks() {
	gs.mu.Lock()
	if gs.callbacksRegistered {
		gs.mu.Unlock()
		return
	}
	gs.callbacksRegistered = true
	gs.mu.Unlock()

	// Name callbacks per instance so that several GormSearch instances can
	// share one *gorm.DB (e.g. one per tenant index prefix) without
	// overwriting each other's callbacks.
	prefix := fmt.Sprintf("gormsearch:%p", gs)

	// register installs fn, falling back to Replace so re-registering is
	// idempotent instead of stacking duplicate handlers.
	register := func(add, replace func(string, func(*gorm.DB)) error, name string, fn func(*gorm.DB)) {
		if err := add(name, fn); err != nil {
			_ = replace(name, fn)
		}
	}

	createCB := gs.db.Callback().Create().After("gorm:create")
	register(createCB.Register, createCB.Replace, prefix+":create", func(db *gorm.DB) {
		gs.syncWrite(db, opCreate)
	})

	updateCB := gs.db.Callback().Update().After("gorm:update")
	register(updateCB.Register, updateCB.Replace, prefix+":update", func(db *gorm.DB) {
		gs.syncWrite(db, opUpdate)
	})

	// Resolving the affected keys has to happen before the rows go away.
	beforeDeleteCB := gs.db.Callback().Delete().Before("gorm:delete")
	register(beforeDeleteCB.Register, beforeDeleteCB.Replace, prefix+":before_delete", gs.resolveDeleteIDs)

	deleteCB := gs.db.Callback().Delete().After("gorm:delete")
	register(deleteCB.Register, deleteCB.Replace, prefix+":delete", gs.syncDelete)
}

// syncWrite dispatches create/update jobs for every model touched by the
// statement. Batch writes (`db.Create(&[]Product{...})`) fan out to one job
// per element.
func (gs *GormSearch) syncWrite(db *gorm.DB, op operation) {
	config, ok := gs.configForStatement(db)
	if !ok {
		return
	}

	eachStructValue(db.Statement.Model, func(v reflect.Value) {
		gs.syncOne(db, config, op, v)
	})
}

// syncOne reloads a single record and dispatches the resulting job.
func (gs *GormSearch) syncOne(db *gorm.DB, config *IndexConfig, op operation, v reflect.Value) {
	idVal, id := idFromValue(v, config)
	if id == "" {
		// A zero primary key means the statement targets rows by condition
		// (e.g. db.Model(&Product{}).Where(...).Update(...)); there is no way
		// to tell which documents to refresh, so report rather than guess.
		gs.reportError(op.String(), ErrUnresolvedPrimaryKey)
		return
	}

	// Reload the record so the document reflects every column, not just the
	// ones this statement happened to write.
	//
	// NewDB:true is essential: a plain Session() inherits the statement that is
	// mid-flight, including its already-built SQL, which makes the "reload"
	// re-execute the INSERT/UPDATE. The fresh statement keeps the same
	// ConnPool, so the read still happens inside the caller's transaction.
	loaded := reflect.New(v.Type()).Interface()
	err := db.Session(&gorm.Session{NewDB: true}).
		Unscoped().
		Where(clause.Eq{Column: clause.PrimaryColumn, Value: idVal}).
		First(loaded).Error
	if err != nil {
		gs.reportError("reload", err)
		return
	}

	job := Job{IndexName: config.IndexName, ID: id}

	// A soft-deleted row must leave the index even though GORM reports the
	// statement as an update.
	if isSoftDeleted(loaded, config) {
		job.Operation = opDelete.String()
		gs.dispatch(job)
		return
	}

	doc, err := gs.encodeDocument(loaded, config)
	if err != nil {
		gs.reportError("encode", err)
		return
	}
	if doc == nil {
		return
	}

	job.Operation = op.String()
	job.Document = doc
	gs.dispatch(job)
}

// resolveDeleteIDs records which documents the pending delete will remove.
//
// `db.Delete(&Product{}, 42)` and conditional deletes leave the destination
// struct zeroed, so the primary key has to be read from the table while the
// rows are still there. When the destination already carries keys (the common
// `db.Delete(&product)` case) no extra query is issued.
func (gs *GormSearch) resolveDeleteIDs(db *gorm.DB) {
	config, ok := gs.configForStatement(db)
	if !ok {
		return
	}

	var ids []string
	eachStructValue(db.Statement.Model, func(v reflect.Value) {
		if _, id := idFromValue(v, config); id != "" {
			ids = append(ids, id)
		}
	})

	if len(ids) == 0 {
		ids = gs.queryDeleteIDs(db)
	}

	if len(ids) > 0 {
		db.InstanceSet(deleteIDsKey, ids)
	}
}

// queryDeleteIDs selects the primary keys matching the pending delete's
// conditions. It runs before the delete is built, so Session() safely inherits
// those conditions (there is no statement SQL to re-execute yet).
func (gs *GormSearch) queryDeleteIDs(db *gorm.DB) []string {
	if db.Statement.Schema == nil {
		return nil
	}
	pk := db.Statement.Schema.PrioritizedPrimaryField
	if pk == nil {
		return nil
	}

	var ids []string
	if err := db.Session(&gorm.Session{}).
		Limit(maxResolvedDeleteIDs+1).
		Pluck(pk.DBName, &ids).Error; err != nil {
		gs.reportError("resolve_delete", err)
		return nil
	}

	if len(ids) > maxResolvedDeleteIDs {
		gs.reportError("resolve_delete", ErrDeleteFanoutTooLarge)
		return ids[:maxResolvedDeleteIDs]
	}
	return ids
}

// syncDelete removes the documents recorded by resolveDeleteIDs.
func (gs *GormSearch) syncDelete(db *gorm.DB) {
	config, ok := gs.configForStatement(db)
	if !ok {
		return
	}

	raw, ok := db.InstanceGet(deleteIDsKey)
	if !ok {
		return
	}
	ids, ok := raw.([]string)
	if !ok {
		return
	}

	for _, id := range ids {
		gs.dispatch(Job{
			IndexName: config.IndexName,
			Operation: opDelete.String(),
			ID:        id,
		})
	}
}

// configForStatement resolves the registered config for the statement's model,
// reporting false when the statement errored or the model is not registered.
func (gs *GormSearch) configForStatement(db *gorm.DB) (*IndexConfig, bool) {
	if db.Error != nil || db.Statement == nil || db.Statement.Model == nil {
		return nil, false
	}

	config := gs.configForType(reflect.TypeOf(db.Statement.Model))
	return config, config != nil
}

// dispatch hands a job to the configured dispatcher and reports failures.
func (gs *GormSearch) dispatch(job Job) {
	if gs.config == nil || gs.config.Dispatcher == nil {
		return
	}

	ctx := gs.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if err := gs.config.Dispatcher.Dispatch(ctx, job); err != nil {
		gs.reportError("dispatch", err)
	}
}

// reportError forwards an error to the configured OnError callback, if any.
func (gs *GormSearch) reportError(op string, err error) {
	if gs.config != nil && gs.config.OnError != nil {
		gs.config.OnError(op, err)
	}
}
