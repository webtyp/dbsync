package dbsync

import (
	webtypfmt "webtyp.com/fmt"
)

const (
	errAnotherTab          = "dbsync: the app is already open in another tab of this browser"
	errTabLockRequired     = "dbsync: TabLock is required outside the browser"
	errFirstRunNeedsServer = "dbsync: this browser must reach the server once before working offline"
	errLocalNeedsTx        = "dbsync: the local database must support transactions (BeginTx)"
	errNotOfflineOp        = "dbsync: operation %s is not an offline operation; call it through rpc"
	errScopeOperator       = "dbsync: scope operator %s is not supported"
)

// Exported typed error constants
var (
	ErrAnotherTab          = webtypfmt.Err(errAnotherTab)
	ErrFirstRunNeedsServer = webtypfmt.Err(errFirstRunNeedsServer)
)
