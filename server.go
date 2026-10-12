package dbsync

import (
	"sync"

	"webtyp.com/changelog"
	webtypfmt "webtyp.com/fmt"
	"webtyp.com/model"
	"webtyp.com/orm"
	"webtyp.com/router"
)

type ServerConfig struct {
	DB        *orm.DB
	Log       *changelog.Log
	Rules     []Rule
	Authorize model.Authorizer
	Now       func() int64
}

type Server struct {
	cfg   ServerConfig
	epoch string
	mu    sync.Mutex
}

var (
	errNilDB        = webtypfmt.Err("dbsync: DB is nil")
	errNilLog       = webtypfmt.Err("dbsync: Log is nil")
	errNilAuthorize = webtypfmt.Err("dbsync: Authorize is nil")
	errNilNow       = webtypfmt.Err("dbsync: Now is nil")
	errNoRules      = webtypfmt.Err("dbsync: Rules is empty")
	errInvalidRule  = webtypfmt.Err("dbsync: rule is invalid")
	errDuplicate    = webtypfmt.Err("dbsync: duplicate rule for table")
)

func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.DB == nil {
		return nil, errNilDB
	}
	if cfg.Log == nil {
		return nil, errNilLog
	}
	if cfg.Authorize == nil {
		return nil, errNilAuthorize
	}
	if cfg.Now == nil {
		return nil, errNilNow
	}
	if len(cfg.Rules) == 0 {
		return nil, errNoRules
	}
	seen := make(map[string]bool)
	for _, r := range cfg.Rules {
		if r.Model == nil || r.Resource == "" {
			return nil, errInvalidRule
		}
		m := r.Model()
		if seen[m.ModelName()] {
			return nil, errDuplicate
		}
		seen[m.ModelName()] = true
	}

	epoch := webtypfmt.Sprintf("%d", cfg.Now())

	return &Server{
		cfg:   cfg,
		epoch: epoch,
	}, nil
}

func (s *Server) ModelName() string {
	return "dbsync"
}

func (s *Server) MountAPI(r router.Router) {
	// mock
}
