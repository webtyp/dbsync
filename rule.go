package dbsync

import (
	webtypfmt "webtyp.com/fmt"
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

func where(qb *orm.QB, conds []storage.Condition) (*orm.QB, error) {
	for _, c := range conds {
		if c.Logic() == logicOr {
			qb = qb.Or()
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
			return nil, webtypfmt.Err(errScopeOperator, c.Operator())
		}
	}
	return qb, nil
}
