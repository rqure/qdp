package main

import (
	"context"

	qdb "github.com/rqure/qdb/src"
	qdp "github.com/rqure/qdp/lib/go"
)

type MessageBroker struct {
	db                qdb.IDatabase
	isLeader          bool
	ctx               context.Context
	cancel            context.CancelFunc
	subscribers       qdp.ISubscriptionManager
	protocolsByEntity map[string]qdp.IProtocol
	taskCh            chan func()
}

func NewMessageBroker(db qdb.IDatabase) *MessageBroker {
	ctx, cancel := context.WithCancel(context.Background())

	return &MessageBroker{
		db:          db,
		isLeader:    false,
		ctx:         ctx,
		cancel:      cancel,
		subscribers: qdp.NewSubscriptionManager(),
		taskCh:      make(chan func(), 1024),
	}
}

func (w *MessageBroker) OnBecameLeader() {
	w.isLeader = true

	w.db.Notify(&qdb.DatabaseNotificationConfig{
		Type:  "QdpTcpTransport",
		Field: "IsEnabled",
	}, qdb.NewNotificationCallback(w.onTcpTransportIsEnabledChanged))

	tcpTransports := qdb.NewEntityFinder(w.db).Find(qdb.SearchCriteria{
		EntityType: "QdpTcpTransport",
	})

	for _, transportEntity := range tcpTransports {
		activeConnections := transportEntity.GetField("ActiveConnections")
		activeConnections.PushInt(0)

		enabled := transportEntity.GetField("IsEnabled").PullBool()

		if !enabled {
			qdb.Info("[MessageBroker::OnBecameLeader] TCP Transport %v is disabled", transportEntity.GetId())
			continue
		}

		connectionHandler := qdp.ConnectionHandlerFunc{
			OnConnectFunc: func(transport qdp.ITransport) {
				w.taskCh <- func() {
					qdb.Info("[MessageBroker::OnBecameLeader] TCP Transport connected: %v", transport)

					activeConnections.PushInt(activeConnections.PullInt() + 1)

					protocol := qdp.NewProtocol(transport, w.subscribers)
					w.protocolsByEntity[transportEntity.GetId()] = protocol

					protocol.StartReceiving(w.ctx)
				}
			},

			OnDisconnectFunc: func(transport qdp.ITransport, err error) {
				w.taskCh <- func() {
					qdb.Info("[MessageBroker::OnBecameLeader] TCP Transport disconnected: %v, error: %v", transport, err)

					activeConnections := qdb.NewEntity(w.db, transportEntity.GetId()).GetField("ActiveConnections")
					activeConnections.PushInt(activeConnections.PullInt() - 1)

					protocol := w.protocolsByEntity[transportEntity.GetId()]
					protocol.Close()

					delete(w.protocolsByEntity, transportEntity.GetId())
				}
			},
		}

		addr := transportEntity.GetField("Address").PullString()
		isClient := transportEntity.GetField("IsClient").PullBool()

		if isClient {
			_, err := qdp.NewTCPClientTransport(addr, connectionHandler)

			if err != nil {
				qdb.Error("[MessageBroker::OnBecameLeader] Failed to create TCP client transport: %v", err)
				continue
			}
		} else {
			_, err := qdp.NewTCPServerTransport(addr, connectionHandler)

			if err != nil {
				qdb.Error("[MessageBroker::OnBecameLeader] Failed to create TCP server transport: %v", err)
				continue
			}
		}
	}
}

func (w *MessageBroker) OnLostLeadership() {
	w.isLeader = false
}

func (w *MessageBroker) OnSchemaUpdated() {

}

func (w *MessageBroker) Init() {
}

func (w *MessageBroker) Deinit() {

}

func (w *MessageBroker) DoWork() {
	if !w.isLeader {
		return
	}

}

func (w *MessageBroker) onTcpTransportIsEnabledChanged(notification *qdb.DatabaseNotification) {

}
