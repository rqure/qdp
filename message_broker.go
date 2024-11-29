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
	protocolsByEntity map[string]qdp.IProtocol
	taskCh            chan func()
	tokens            []qdb.INotificationToken
}

func NewMessageBroker(db qdb.IDatabase) *MessageBroker {
	ctx, cancel := context.WithCancel(context.Background())

	return &MessageBroker{
		db:       db,
		isLeader: false,
		ctx:      ctx,
		cancel:   cancel,
		taskCh:   make(chan func(), 1024),
	}
}

func (w *MessageBroker) OnBecameLeader() {
	w.isLeader = true

	w.teardown()
	w.setup()
}

func (w *MessageBroker) OnLosingLeadership() {
	w.isLeader = false

	w.teardown()
}

func (w *MessageBroker) setupTcpTransport(transportEntity qdb.IEntity) {
	isConnected := transportEntity.GetField("IsConnected")
	isConnected.PushBool(false)

	totalReceived := transportEntity.GetField("TotalReceived")
	totalSent := transportEntity.GetField("TotalSent")

	enabled := transportEntity.GetField("IsEnabled").PullBool()

	if !enabled {
		qdb.Info("[MessageBroker::OnBecameLeader] TCP Transport %v is disabled", transportEntity.GetId())
		return
	}

	connectionHandler := qdp.ConnectionHandlerFunc{
		OnConnectFunc: func(transport qdp.ITransport) {
			w.taskCh <- func() {
				qdb.Info("[MessageBroker::OnBecameLeader] TCP Transport connected: %v", transport)

				isConnected.PushBool(true)

				protocol := qdp.NewProtocol(transport, nil)
				w.protocolsByEntity[transportEntity.GetId()] = protocol

				protocol.StartReceiving(w.ctx)
			}
		},

		OnDisconnectFunc: func(transport qdp.ITransport, err error) {
			w.taskCh <- func() {
				qdb.Info("[MessageBroker::OnBecameLeader] TCP Transport disconnected: %v, error: %v", transport, err)

				isConnected.PushBool(false)

				protocol := w.protocolsByEntity[transportEntity.GetId()]
				protocol.Close()

				delete(w.protocolsByEntity, transportEntity.GetId())
			}
		},

		OnMessageReceivedFunc: func(transport qdp.ITransport, msg *qdp.Message) {
			w.taskCh <- func() {
				qdb.Info("[MessageBroker::OnBecameLeader] TCP Transport message received: %v", msg)

				totalReceived.PushInt(totalReceived.PullInt() + 1)
			}
		},

		OnMessageSentFunc: func(transport qdp.ITransport, msg *qdp.Message) {
			w.taskCh <- func() {
				qdb.Info("[MessageBroker::OnBecameLeader] TCP Transport message sent: %v", msg)

				totalSent.PushInt(totalSent.PullInt() + 1)
			}
		},
	}

	addr := transportEntity.GetField("Address").PullString()
	isClient := transportEntity.GetField("IsClient").PullBool()

	if isClient {
		_, err := qdp.NewTCPClientTransport(addr, connectionHandler)

		if err != nil {
			qdb.Error("[MessageBroker::OnBecameLeader] Failed to create TCP client transport: %v", err)
			return
		}
	} else {
		_, err := qdp.NewTCPServerTransport(addr, connectionHandler)

		if err != nil {
			qdb.Error("[MessageBroker::OnBecameLeader] Failed to create TCP server transport: %v", err)
			return
		}
	}
}

func (w *MessageBroker) setup() {
	w.tokens = append(w.tokens, w.db.Notify(&qdb.DatabaseNotificationConfig{
		Type:  "QdpTcpTransport",
		Field: "IsEnabled",
	}, qdb.NewNotificationCallback(w.onTcpTransportIsEnabledChanged)))

	w.tokens = append(w.tokens, w.db.Notify(&qdb.DatabaseNotificationConfig{
		Type:  "QdpTopic",
		Field: "TxMessage",
		ContextFields: []string{
			"Topic",
			"TransportReference",
		},
	}, qdb.NewNotificationCallback(w.onTxMessage)))

	tcpTransports := qdb.NewEntityFinder(w.db).Find(qdb.SearchCriteria{
		EntityType: "QdpTcpTransport",
	})

	for _, transportEntity := range tcpTransports {
		w.setupTcpTransport(transportEntity)
	}

	topics := qdb.NewEntityFinder(w.db).Find(qdb.SearchCriteria{
		EntityType: "QdpTopic",
	})

	for _, topicEntity := range topics {
		topic := topicEntity.GetField("Topic").PullString()

		transportReference := topicEntity.GetField("TransportReference").PullEntityReference()

		rxMessage := topicEntity.GetField("RxMessage")
		rxMessageFn := topicEntity.GetField("RxMessageFn")

		protocol := w.protocolsByEntity[transportReference]

		if protocol == nil {
			continue
		}

		protocol.Subscribe(topic, qdp.MessageHandlerFunc(func(m *qdp.Message) {
			rxMessage.PushString(string(m.Payload))
			rxMessageFn.PushString(string(m.Payload))
		}))
	}
}

func (w *MessageBroker) teardown() {
	for _, protocol := range w.protocolsByEntity {
		protocol.Close()
	}

	w.protocolsByEntity = make(map[string]qdp.IProtocol)

	for _, token := range w.tokens {
		token.Unbind()
	}

	w.tokens = make([]qdb.INotificationToken, 0)
}

func (w *MessageBroker) OnSchemaUpdated() {
	if !w.isLeader {
		return
	}

	w.teardown()
	w.setup()
}

func (w *MessageBroker) Init() {
	w.protocolsByEntity = make(map[string]qdp.IProtocol)
}

func (w *MessageBroker) Deinit() {
	w.teardown()
	w.cancel()

	// Wait for any pending tasks
	for {
		select {
		case task := <-w.taskCh:
			task()
		default:
			return
		}
	}
}

func (w *MessageBroker) DoWork() {
	select {
	case task := <-w.taskCh:
		if w.isLeader {
			task()
		}
	default:
	}
}

func (w *MessageBroker) onTcpTransportIsEnabledChanged(notification *qdb.DatabaseNotification) {
	transportEntity := notification.Current.Id
	isEnabled := qdb.ValueCast[*qdb.Bool](notification.Current.Value).Raw

	// Get existing protocol if any
	existingProtocol := w.protocolsByEntity[transportEntity]

	// If disabled, cleanup existing protocol
	if !isEnabled && existingProtocol != nil {
		existingProtocol.Close()
		delete(w.protocolsByEntity, transportEntity)
		return
	}

	// If already enabled with protocol, nothing to do
	if isEnabled && existingProtocol != nil {
		return
	}

	w.setupTcpTransport(qdb.NewEntity(w.db, notification.Current.Id))
}

func (w *MessageBroker) onTxMessage(notification *qdb.DatabaseNotification) {
	txMessage := qdb.ValueCast[*qdb.String](notification.Current.Value).Raw
	topic := qdb.ValueCast[*qdb.String](notification.Context[0].Value).Raw
	transportEntity := qdb.ValueCast[*qdb.EntityReference](notification.Context[1].Value).Raw

	protocol := w.protocolsByEntity[transportEntity]

	if protocol == nil {
		qdb.Warn("[MessageBroker::onTxMessage] Protocol not found for transport entity: %v", transportEntity)
		return
	}

	protocol.SendMessage(&qdp.Message{
		Topic:   topic,
		Payload: []byte(txMessage),
	})
}
