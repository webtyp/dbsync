package migrate

import (
	"webtyp.com/dbsync"
	"webtyp.com/ddl"
)

// Migrate creates dbsync's server table (dbsync_replica).
func Migrate(conn ddl.Execer, compiler ddl.Compiler) error {
	return ddl.New(conn, compiler).CreateTable(&dbsync.Replica{})
}
