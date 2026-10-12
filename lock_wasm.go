//go:build wasm

package dbsync

import "syscall/js"

// browserLock holds a Web Lock for the page's whole life, so a second tab of the same profile
// cannot open the local database.
type browserLock struct{}

func defaultTabLock() TabLock { return browserLock{} }

// TryLock blocks until the browser answers: never call it from a JavaScript event callback.
func (browserLock) TryLock(name string) (bool, error) {
	locks := js.Global().Get("navigator").Get("locks")
	if locks.IsUndefined() || locks.IsNull() {
		return false, errNoWebLocks
	}
	got := make(chan bool, 1) // buffered: the JS callback must never block
	opts := js.Global().Get("Object").New()
	opts.Set("ifAvailable", true)
	var cb js.Func
	cb = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || args[0].IsNull() || args[0].IsUndefined() {
			got <- false
			return nil
		}
		got <- true
		// A promise that never resolves keeps the lock until the page closes.
		return js.Global().Get("Promise").New(js.FuncOf(func(js.Value, []js.Value) any { return nil }))
	})
	locks.Call("request", name, opts, cb)
	return <-got, nil
}
