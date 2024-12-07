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
	messageBroker := NewMessageBroker(db)

	schemaValidator := qdb.NewSchemaValidator(db)

	schemaValidator.AddEntity("QdpController") // entity type

	schemaValidator.AddEntity("QdpTcpTransport", // entity type
		"Address", "IsClient", "IsEnabled", "IsConnected", "TotalReceived", "TotalSent") // entity fields

	schemaValidator.AddEntity("QdpFtdiTransport", // entity type
		"VendorID", "ProductID", "Interface", "ReadEndpoint", "WriteEndpoint",
		"IsEnabled", "IsConnected", "TotalReceived", "TotalSent") // entity fields

	schemaValidator.AddEntity("QdpTopic", // entity type
		"Topic", "TransportReference", "TxMessage", "RxMessage", "RxMessageFn") // entity fields

	dbWorker.Signals.SchemaUpdated.Connect(qdb.Slot(schemaValidator.ValidationRequired))
	dbWorker.Signals.Connected.Connect(qdb.Slot(schemaValidator.ValidationRequired))
	leaderElectionWorker.AddAvailabilityCriteria(func() bool {
		return dbWorker.IsConnected() && schemaValidator.IsValid()
	})

	dbWorker.Signals.Connected.Connect(qdb.Slot(leaderElectionWorker.OnDatabaseConnected))
	dbWorker.Signals.Disconnected.Connect(qdb.Slot(leaderElectionWorker.OnDatabaseDisconnected))
	dbWorker.Signals.SchemaUpdated.Connect(qdb.Slot(messageBroker.OnSchemaUpdated))

	leaderElectionWorker.Signals.BecameLeader.Connect(qdb.Slot(messageBroker.OnBecameLeader))
	leaderElectionWorker.Signals.LosingLeadership.Connect(qdb.Slot(messageBroker.OnLosingLeadership))

	// Create a new application configuration
	config := qdb.ApplicationConfig{
		Name: "qdp",
		Workers: []qdb.IWorker{
			dbWorker,
			leaderElectionWorker,
			messageBroker,
		},
	}

	// Create a new application
	app := qdb.NewApplication(config)

	// Execute the application
	app.Execute()
}
