package dbsync

import (
	"webtyp.com/model"
)

//go:generate ormc

// State keeps the device state. Local only, dbsync_state.
var StateModel = model.Definition{
	Name: "dbsync_state",
	Fields: model.Fields{
		{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}}, // always "state"
		{Name: "replica", Type: model.Int()},
		{Name: "cursor", Type: model.Int()}, // using Int for int64
		{Name: "epoch", Type: model.Text()},
		{Name: "last_minted", Type: model.Int()}, // using Int for int64
	},
}

// Touched logs which rows a mutation touched. Local only, dbsync_touched.
var TouchedModel = model.Definition{
	Name: "dbsync_touched",
	Fields: model.Fields{
		{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "mutation_id", Type: model.Text()},
		{Name: "table_name", Type: model.Text()},
		{Name: "row_id", Type: model.Text()},
		{Name: "created_at", Type: model.NanoTime()},
	},
}

// Replica registers devices on the server. Server only, dbsync_replica.
var ReplicaModel = model.Definition{
	Name: "dbsync_replica",
	Fields: model.Fields{
		{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}}, // decimal representation of Number
		{Name: "number", Type: model.Int()},
		{Name: "user_id", Type: model.Text()},
		{Name: "created_at", Type: model.NanoTime()},
	},
}
