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
