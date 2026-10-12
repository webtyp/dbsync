package dbsync

import (
	"webtyp.com/fmt"
	"webtyp.com/model"
	"webtyp.com/orm"
	"webtyp.com/storage"
)

// DefaultPrefix is where the server side is mounted.
const DefaultPrefix = "/api/dbsync"

// Rule declares one table that devices keep locally, and which of its rows a user receives.
type Rule struct {
	// Model returns a fresh instance of the table's model (the same factory given to changelog.New).
	Model func() model.Model
	// Resource: the user must hold model.Read on it (via ServerConfig.Authorize) or the table is
	// skipped for that user. Required (closed by default).
	Resource model.Resource
	// Scope returns the conditions selecting the rows userID receives. nil = all rows of the table.
	// The conditions are joined with AND; a condition built with storage.Or is refused.
	Scope func(userID string) ([]storage.Condition, error)
	// Fields returns the columns userID receives (schema names; the primary key is always sent).
	// nil, or a nil answer = every column. The device stores the other columns empty.
	Fields func(userID string) ([]string, error)
}

const (
	logicOr = "OR"
	opEq    = "="
	opNeq   = "!="
	opGt    = ">"
	opGte   = ">="
	opLt    = "<"
	opLte   = "<="
	opLike  = "LIKE"
	opIn    = "IN"
)

// where adds conds to qb. OR is refused: orm joins conditions flat, so "pk = x OR owner = u"
// would hand out rows outside the user's scope.
func where(qb *orm.QB, conds []storage.Condition) (*orm.QB, error) {
	for _, c := range conds {
		if c.Logic() == logicOr {
			return nil, errScopeOr
		}
		switch c.Operator() {
		case opEq:
			qb = qb.Where(c.Field()).Eq(c.Value())
		case opNeq:
			qb = qb.Where(c.Field()).Neq(c.Value())
		case opGt:
			qb = qb.Where(c.Field()).Gt(c.Value())
		case opGte:
			qb = qb.Where(c.Field()).Gte(c.Value())
		case opLt:
			qb = qb.Where(c.Field()).Lt(c.Value())
		case opLte:
			qb = qb.Where(c.Field()).Lte(c.Value())
		case opLike:
			qb = qb.Where(c.Field()).Like(c.Value())
		case opIn:
			qb = qb.Where(c.Field()).In(c.Value())
		default:
			return nil, errScopeOperator(c.Operator())
		}
	}
	return qb, nil
}

// pkName is the column of m's primary key.
func pkName(m model.Model) (string, error) {
	for _, f := range m.Schema() {
		if f.IsPK() {
			return f.Name, nil
		}
	}
	return "", errNoPK(m.ModelName())
}

// pkValue is m's primary key as text.
func pkValue(m model.Model) (string, error) {
	schema := m.Schema()
	for i, f := range schema {
		if f.IsPK() {
			return fmt.Convert(model.ReadValues(schema, m.Pointers())[i]).String(), nil
		}
	}
	return "", errNoPK(m.ModelName())
}

// columns resolves rule.Fields for userID against proto's schema: nil = every column.
func columns(rule Rule, userID string, proto model.Model) ([]string, error) {
	if rule.Fields == nil {
		return nil, nil
	}
	fields, err := rule.Fields(userID)
	if err != nil || fields == nil {
		return nil, err
	}
	pk, err := pkName(proto)
	if err != nil {
		return nil, err
	}
	out := []string{pk}
	for _, f := range fields {
		known := false
		for _, sf := range proto.Schema() {
			if sf.Name == f {
				known = true
				break
			}
		}
		if !known {
			return nil, errUnknownField(proto.ModelName(), f)
		}
		if f != pk {
			out = append(out, f)
		}
	}
	return out, nil
}

// project sends only cols of m (all of them when cols is nil).
func project(m model.Model, cols []string) model.Encodable {
	if cols == nil {
		return m
	}
	return projected{m: m, cols: cols}
}

type projected struct {
	m    model.Model
	cols []string
}

func (p projected) IsNil() bool { return p.m == nil }
func (p projected) EncodeFields(w model.FieldWriter) {
	p.m.EncodeFields(columnFilter{w: w, keep: p.cols})
}

// columnFilter forwards only the kept fields.
type columnFilter struct {
	w    model.FieldWriter
	keep []string
}

func (f columnFilter) kept(name string) bool {
	for _, k := range f.keep {
		if k == name {
			return true
		}
	}
	return false
}

func (f columnFilter) String(name, val string) {
	if f.kept(name) {
		f.w.String(name, val)
	}
}
func (f columnFilter) Int(name string, val int64) {
	if f.kept(name) {
		f.w.Int(name, val)
	}
}
func (f columnFilter) Float(name string, val float64) {
	if f.kept(name) {
		f.w.Float(name, val)
	}
}
func (f columnFilter) Bool(name string, val bool) {
	if f.kept(name) {
		f.w.Bool(name, val)
	}
}
func (f columnFilter) Bytes(name string, val []byte) {
	if f.kept(name) {
		f.w.Bytes(name, val)
	}
}
func (f columnFilter) Null(name string) {
	if f.kept(name) {
		f.w.Null(name)
	}
}
func (f columnFilter) Raw(name, val string) {
	if f.kept(name) {
		f.w.Raw(name, val)
	}
}
func (f columnFilter) Object(name string, val model.Encodable) {
	if f.kept(name) {
		f.w.Object(name, val)
	}
}
func (f columnFilter) Array(name string, n int) model.ArrayWriter {
	if f.kept(name) {
		return f.w.Array(name, n)
	}
	return discardArray{}
}

type discardArray struct{}

func (discardArray) String(string)          {}
func (discardArray) Int(int64)              {}
func (discardArray) Float(float64)          {}
func (discardArray) Bool(bool)              {}
func (discardArray) Bytes([]byte)           {}
func (discardArray) Object(model.Encodable) {}
func (discardArray) Close()                 {}
