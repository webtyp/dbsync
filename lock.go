//go:build !wasm

package dbsync

// defaultTabLock: outside the browser there is no Web Locks; ClientConfig.Lock is required.
func defaultTabLock() TabLock { return nil }
