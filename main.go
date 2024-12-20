package main

import (
	"os"

	"github.com/rqure/qlib/pkg/app"
	"github.com/rqure/qlib/pkg/app/workers"
	"github.com/rqure/qlib/pkg/data/store"
)

func getDatabaseAddress() string {
	addr := os.Getenv("Q_ADDR")
	if addr == "" {
		addr = "ws://webgateway:20000/ws"
	}

	return addr
}

func main() {
	s := store.NewWeb(store.WebConfig{
		Address: getDatabaseAddress(),
	})

	storeWorker := workers.NewStore(s)
	leadershipWorker := workers.NewLeadership(s)
	messageBroker := NewMessageBroker(s)

	schemaValidator := leadershipWorker.GetEntityFieldValidator()

	schemaValidator.RegisterEntityFields("QdpController") // entity type

	schemaValidator.RegisterEntityFields("QdpTcpTransport", // entity type
		"Address", "IsClient", "IsEnabled", "IsConnected", "TotalReceived", "TotalSent") // entity fields

	schemaValidator.RegisterEntityFields("QdpFtdiTransport", // entity type
		"VendorID", "ProductID", "Interface", "ReadEndpoint", "WriteEndpoint",
		"BaudRate", "DataBits", "StopBits", "Parity", "FlowControl",
		"IsEnabled", "IsConnected", "TotalReceived", "TotalSent") // entity fields

	schemaValidator.RegisterEntityFields("QdpTopic", // entity type
		"Topic", "TransportReference", "TxMessage", "RxMessage", "RxMessageFn") // entity fields

	storeWorker.Connected.Connect(leadershipWorker.OnStoreConnected)
	storeWorker.Disconnected.Connect(leadershipWorker.OnStoreDisconnected)

	leadershipWorker.BecameLeader().Connect(messageBroker.OnBecameLeader)
	leadershipWorker.LosingLeadership().Connect(messageBroker.OnLosingLeadership)

	app := app.NewApplication("qdp")
	app.AddWorker(storeWorker)
	app.AddWorker(leadershipWorker)
	app.AddWorker(messageBroker)
	app.Execute()
}
