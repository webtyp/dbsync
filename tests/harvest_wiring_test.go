//go:build !wasm

package tests

import (
	"testing"

	"webtyp.com/crud"
	"webtyp.com/dbsync"
	"webtyp.com/model"
	"webtyp.com/orm"
	"webtyp.com/storage/mem"
)

type badMod struct{}

func (badMod) ModelName() string { return "bad" }
func (badMod) MountOperations(reg crud.Registry) {
	reg.Update(nil, nil)
}

func TestHarvest_CrudWiringErrorStopsClient(t *testing.T) {
	s := newServer(t)
	local := mem.New()
	cfg := config(s, local, fakeLock{free: true})
	cfg.Modules = func(db *orm.DB, ids model.IDGenerator) ([]crud.Module, error) {
		return []crud.Module{badMod{}}, nil
	}
	_, err := dbsync.Open(cfg)
	if err != crud.ErrNilFunction {
		t.Fatalf("expected crud.ErrNilFunction, got %v", err)
	}
}
