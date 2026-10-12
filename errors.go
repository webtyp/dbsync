package dbsync

import "webtyp.com/fmt"

type syncError string

func (e syncError) Error() string { return string(e) }

const (
	// ErrAnotherTab: another tab of this browser profile already owns the local database.
	ErrAnotherTab syncError = "dbsync: the app is already open in another tab of this browser"
	// ErrFirstRunNeedsServer: a fresh device must reach the server once (replica number, first copy).
	ErrFirstRunNeedsServer syncError = "dbsync: this browser must reach the server once before working offline"
)

const (
	errTabLockRequired syncError = "dbsync: TabLock is required outside the browser"
	errNoWebLocks      syncError = "dbsync: this browser has no Web Locks (navigator.locks)"
	errLocalNeedsTx    syncError = "dbsync: the local database must support transactions (BeginTx)"
	errLocalRequired   syncError = "dbsync: ClientConfig.Local is required"
	errRemoteRequired  syncError = "dbsync: ClientConfig.Remote is required"
	errModulesRequired syncError = "dbsync: ClientConfig.Modules is required"
	errNowRequired     syncError = "dbsync: Now is required"
	errTablesRequired  syncError = "dbsync: ClientConfig.Tables is empty"
	errUserRequired    syncError = "dbsync: ClientConfig.UserID is required"
	errDBRequired      syncError = "dbsync: ServerConfig.DB is required"
	errLogRequired     syncError = "dbsync: ServerConfig.Log is required"
	errAuthzRequired   syncError = "dbsync: ServerConfig.Authorize is required"
	errNoRules         syncError = "dbsync: ServerConfig.Rules is empty"
	errRuleModel       syncError = "dbsync: a rule has no Model"
	errRuleResource    syncError = "dbsync: a rule has no Resource"
	errScopeOr         syncError = "dbsync: a scope condition with OR logic is not supported"

	msgNotOfflineOp   = "dbsync: operation %s is not an offline operation; call it through rpc"
	msgScopeOperator  = "dbsync: scope operator %s is not supported"
	msgDuplicateRule  = "dbsync: two rules for table %s"
	msgNoPK           = "dbsync: table %s has no primary key field"
	msgUnknownField   = "dbsync: table %s has no field %s"
	msgServerAnswered = "dbsync: server answered %d to %s: %s"
)

func errNotOfflineOp(op string) error { return syncError(fmt.Sprintf(msgNotOfflineOp, op)) }

func errScopeOperator(op string) error { return syncError(fmt.Sprintf(msgScopeOperator, op)) }

func errDuplicateRule(table string) error { return syncError(fmt.Sprintf(msgDuplicateRule, table)) }

func errUnknownField(table, field string) error {
	return syncError(fmt.Sprintf(msgUnknownField, table, field))
}

func errNoPK(table string) error { return syncError(fmt.Sprintf(msgNoPK, table)) }

func errServerAnswered(status int, route, body string) error {
	return syncError(fmt.Sprintf(msgServerAnswered, status, route, body))
}
