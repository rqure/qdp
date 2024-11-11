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
	schemaValidator := qdb.NewSchemaValidator(db)
	// tcpListener := NewTCPListener(db)

	schemaValidator.AddEntity("QdpTcpTransport", "Address", "IsClient", "IsEnabled", "IsConnected")
	schemaValidator.AddEntity("QdpDevice",
		"QdpId",
		"IsIncoming", "TransportReference",
		"GetDevicesTrigger", "DeviceList",
		"LastError",
		"GetTelemetryTrigger", "TelemetryAsInt", "TelemetryAsFloat", "TelemetryAsString", "TelemetryAsArray", "TelemetryAsNull",
		"CommandAsInt", "CommandAsFloat", "CommandAsString", "CommandAsArray", "CommandAsNull",
	)

	dbWorker.Signals.SchemaUpdated.Connect(qdb.Slot(schemaValidator.ValidationRequired))
	dbWorker.Signals.Connected.Connect(qdb.Slot(schemaValidator.ValidationRequired))
	leaderElectionWorker.AddAvailabilityCriteria(func() bool {
		return dbWorker.IsConnected() && schemaValidator.IsValid()
	})

	dbWorker.Signals.Connected.Connect(qdb.Slot(leaderElectionWorker.OnDatabaseConnected))
	dbWorker.Signals.Disconnected.Connect(qdb.Slot(leaderElectionWorker.OnDatabaseDisconnected))
	// dbWorker.Signals.SchemaUpdated.Connect(qdb.Slot(tcpListener.OnSchemaUpdated))

	// leaderElectionWorker.Signals.BecameLeader.Connect(qdb.Slot(tcpListener.OnBecameLeader))
	// leaderElectionWorker.Signals.LosingLeadership.Connect(qdb.Slot(tcpListener.OnLostLeadership))

	// Create a new application configuration
	config := qdb.ApplicationConfig{
		Name: "qdp",
		Workers: []qdb.IWorker{
			dbWorker,
			leaderElectionWorker,
			// tcpListener,
		},
	}

	// Create a new application
	app := qdb.NewApplication(config)

	// Execute the application
	app.Execute()

	// gw := NewTCPServerGateway("0.0.0.0:12346")
	// gw.Start()

	// for {
	// 	<-time.After(1 * time.Second)

	// 	m := &QDPMessage{
	// 		Header: QDPHeader{
	// 			From:          1,
	// 			To:            0xDEADBEEF,
	// 			PayloadType:   PAYLOAD_TYPE_DEVICE_ID_REQUEST,
	// 			CorrelationId: 1,
	// 		},
	// 		Payload: QDPPayload{
	// 			DataType: DATA_TYPE_NULL,
	// 			Size:     0,
	// 			Data:     []byte{},
	// 		},
	// 	}

	// 	qdb.Info("Sending message: %s", m.String())
	// 	gw.Send(m)

	// 	<-time.After(1 * time.Second)

	// 	for {
	// 		m = gw.Recv()
	// 		if m == nil {
	// 			break
	// 		}
	// 		qdb.Info("Recieved: %s", m.String())
	// 	}
	// }
}
