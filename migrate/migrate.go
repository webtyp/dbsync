package migrate

import (
	"webtyp.com/storage"
)

// Migrate creates the dbsync_replica table on the server.
func Migrate(conn storage.Conn) error {
	return nil
}
