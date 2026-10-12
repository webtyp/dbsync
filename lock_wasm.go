//go:build wasm
// +build wasm

package dbsync

import (
	"syscall/js"
)

// TabLock lets one tab of a browser profile own the local database.
type TabLock interface {
	TryLock(name string) (bool, error)
}

type browserLock struct{}

func (b browserLock) TryLock(name string) (bool, error) {
	if js.Global().Get("navigator").IsUndefined() {
		return false, nil
	}
	locks := js.Global().Get("navigator").Get("locks")
	if locks.IsUndefined() {
		return false, nil
	}

	done := make(chan bool)

	opts := js.Global().Get("Object").New()
	opts.Set("ifAvailable", true)

	cb := js.FuncOf(func(this js.Value, args []js.Value) any {
		lock := args[0]
		if lock.IsNull() || lock.IsUndefined() {
			done <- false
			return nil
		}

		done <- true

		// Return a promise that never resolves so we hold the lock forever
		promise := js.Global().Get("Promise").New(js.FuncOf(func(pThis js.Value, pArgs []js.Value) any {
			// Do nothing, never resolve or reject
			return nil
		}))
		return promise
	})

	locks.Call("request", name, opts, cb)

	ok := <-done
	return ok, nil
}

func defaultTabLock() TabLock {
	return browserLock{}
}
