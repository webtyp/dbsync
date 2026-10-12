//go:build !wasm
// +build !wasm

package dbsync

// TabLock lets one tab of a browser profile own the local database.
type TabLock interface {
	TryLock(name string) (bool, error)
}

func defaultTabLock() TabLock {
	return nil
}
