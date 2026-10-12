package dbsync

import (
	"webtyp.com/changelog"
	"webtyp.com/model"
)

// Routes under DefaultPrefix, their query keys and the JSON keys of their answers.
const (
	routeRegister = "/register"
	routeHead     = "/head"
	routeSnapshot = "/snapshot"
	routePull     = "/pull"
	routeRow      = "/row"

	queryTable  = "table"
	queryAfter  = "after"
	queryLimit  = "limit"
	queryCursor = "cursor"
	queryID     = "id"

	keyReplica = "replica"
	keyEpoch   = "epoch"
	keyHead    = "head"
	keyCursor  = "cursor"
	keyRows    = "rows"
	keyNext    = "next"
	keyChanges = "changes"
	keyTable   = "table"
	keyRowID   = "row_id"
	keyOp      = "op"
	keyRow     = "row"

	opUpsert = string(changelog.OpUpsert)
	opDelete = string(changelog.OpDelete)
)

// replicaWire answers /register.
type replicaWire struct{ replica int64 }

func (w *replicaWire) IsNil() bool                      { return w == nil }
func (w *replicaWire) EncodeFields(o model.FieldWriter) { o.Int(keyReplica, w.replica) }
func (w *replicaWire) DecodeFields(r model.FieldReader) {
	if v, ok := r.Int(keyReplica); ok {
		w.replica = v
	}
}

// headWire answers /head.
type headWire struct {
	epoch string
	head  int64
}

func (w *headWire) IsNil() bool { return w == nil }
func (w *headWire) EncodeFields(o model.FieldWriter) {
	o.String(keyEpoch, w.epoch)
	o.Int(keyHead, w.head)
}
func (w *headWire) DecodeFields(r model.FieldReader) {
	if v, ok := r.String(keyEpoch); ok {
		w.epoch = v
	}
	if v, ok := r.Int(keyHead); ok {
		w.head = v
	}
}

// snapshotOut is what /snapshot writes.
type snapshotOut struct {
	rows []model.Model
	next string
}

func (w *snapshotOut) IsNil() bool { return w == nil }
func (w *snapshotOut) EncodeFields(o model.FieldWriter) {
	a := o.Array(keyRows, len(w.rows))
	for _, r := range w.rows {
		a.Object(r)
	}
	a.Close()
	o.String(keyNext, w.next)
}

// snapshotIn is what the client reads from /snapshot: rows of one known table.
type snapshotIn struct {
	factory func() model.Model
	rows    []model.Model
	next    string
}

func (w *snapshotIn) IsNil() bool { return w == nil }
func (w *snapshotIn) DecodeFields(r model.FieldReader) {
	if a, ok := r.Array(keyRows); ok {
		for i := 0; i < a.Len(); i++ {
			m := w.factory()
			if a.Object(i, m) {
				w.rows = append(w.rows, m)
			}
		}
	}
	if v, ok := r.String(keyNext); ok {
		w.next = v
	}
}

// changeWire is one entry of /pull. row is nil for a delete, and for a table the client does not keep.
type changeWire struct {
	tables []func() model.Model // client side: picks the model of table
	table  string
	rowID  string
	op     string
	row    model.Model
}

func (w *changeWire) IsNil() bool { return w == nil }
func (w *changeWire) EncodeFields(o model.FieldWriter) {
	o.String(keyTable, w.table)
	o.String(keyRowID, w.rowID)
	o.String(keyOp, w.op)
	if w.row != nil {
		o.Object(keyRow, w.row)
	}
}
func (w *changeWire) DecodeFields(r model.FieldReader) {
	w.table, _ = r.String(keyTable)
	w.rowID, _ = r.String(keyRowID)
	w.op, _ = r.String(keyOp)
	if w.op != opUpsert {
		return
	}
	if f := factoryOf(w.tables, w.table); f != nil {
		m := f()
		if r.Object(keyRow, m) {
			w.row = m
		}
	}
}

// pullWire answers /pull.
type pullWire struct {
	tables  []func() model.Model
	epoch   string
	head    int64
	cursor  int64
	changes []*changeWire
}

func (w *pullWire) IsNil() bool { return w == nil }
func (w *pullWire) EncodeFields(o model.FieldWriter) {
	o.String(keyEpoch, w.epoch)
	o.Int(keyHead, w.head)
	o.Int(keyCursor, w.cursor)
	a := o.Array(keyChanges, len(w.changes))
	for _, c := range w.changes {
		a.Object(c)
	}
	a.Close()
}
func (w *pullWire) DecodeFields(r model.FieldReader) {
	w.epoch, _ = r.String(keyEpoch)
	w.head, _ = r.Int(keyHead)
	w.cursor, _ = r.Int(keyCursor)
	if a, ok := r.Array(keyChanges); ok {
		for i := 0; i < a.Len(); i++ {
			c := &changeWire{tables: w.tables}
			if a.Object(i, c) {
				w.changes = append(w.changes, c)
			}
		}
	}
}

// rowWire answers /row.
type rowWire struct{ row model.Model }

func (w *rowWire) IsNil() bool                      { return w == nil }
func (w *rowWire) EncodeFields(o model.FieldWriter) { o.Object(keyRow, w.row) }
func (w *rowWire) DecodeFields(r model.FieldReader) { r.Object(keyRow, w.row) }

// factoryOf returns the factory of tables whose model is named table, or nil.
func factoryOf(tables []func() model.Model, table string) func() model.Model {
	for _, f := range tables {
		if f().ModelName() == table {
			return f
		}
	}
	return nil
}
