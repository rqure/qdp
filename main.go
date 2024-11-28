package main

import (
	"os"

	qdb "github.com/rqure/qdb/src"
)

func getDatabaseAddress() string {
	addr := os.Getenv("QDB_ADDR")
	if addr == "" {
		addr = "redis:6379"
	}

	return addr
}

func main() {
	db := qdb.NewRedisDatabase(qdb.RedisDatabaseConfig{
		Address: getDatabaseAddress(),
	})

	dbWorker := qdb.NewDatabaseWorker(db)
	leaderElectionWorker := qdb.NewLeaderElectionWorker(db)
	tcpTransportWorker := NewTCPTransportWorker(db)

	schemaValidator := qdb.NewSchemaValidator(db)

	schemaValidator.AddEntity("QdpController") // entity type

	schemaValidator.AddEntity("QdpTcpTransport", // entity type
		"Address", "IsClient", "IsEnabled", "IsConnected") // entity fields

	schemaValidator.AddEntity("QdpTopic", // entity type
		"Topic", "TransportReference", "TxMessage", "RxMessage", "RxMessageFn") // entity fields

	dbWorker.Signals.SchemaUpdated.Connect(qdb.Slot(schemaValidator.ValidationRequired))
	dbWorker.Signals.Connected.Connect(qdb.Slot(schemaValidator.ValidationRequired))
	leaderElectionWorker.AddAvailabilityCriteria(func() bool {
		return dbWorker.IsConnected() && schemaValidator.IsValid()
	})

	dbWorker.Signals.Connected.Connect(qdb.Slot(leaderElectionWorker.OnDatabaseConnected))
	dbWorker.Signals.Disconnected.Connect(qdb.Slot(leaderElectionWorker.OnDatabaseDisconnected))
	dbWorker.Signals.SchemaUpdated.Connect(qdb.Slot(tcpTransportWorker.OnSchemaUpdated))

	leaderElectionWorker.Signals.BecameLeader.Connect(qdb.Slot(tcpTransportWorker.OnBecameLeader))
	leaderElectionWorker.Signals.LosingLeadership.Connect(qdb.Slot(tcpTransportWorker.OnLostLeadership))

	// Create a new application configuration
	config := qdb.ApplicationConfig{
		Name: "qdp",
		Workers: []qdb.IWorker{
			dbWorker,
			leaderElectionWorker,
			tcpTransportWorker,
		},
	}

	// Create a new application
	app := qdb.NewApplication(config)

	// Execute the application
	app.Execute()
}
