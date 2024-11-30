package main

import (
	"context"
	"time"

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
		qdb.Info("[MessageBroker::setupTcpTransport] Transport %s is disabled", transportEntity.GetId())
		return
	}

	msgHandler := qdp.MessageHandlerFunc{
		OnMessageRxFunc: func(msg *qdp.Message) {
			w.taskCh <- func() {
				qdb.Debug("[MessageBroker::setupTcpTransport] Transport %s received message on topic '%s' with %d bytes",
					transportEntity.GetId(), msg.Topic, len(msg.Payload))

				totalReceived.PushInt(totalReceived.PullInt() + 1)
			}
		},

		OnMessageTxFunc: func(msg *qdp.Message) {
			w.taskCh <- func() {
				qdb.Debug("[MessageBroker::setupTcpTransport] Transport %s sent message on topic '%s' with %d bytes",
					transportEntity.GetId(), msg.Topic, len(msg.Payload))

				totalSent.PushInt(totalSent.PullInt() + 1)
			}
		},
	}

	connectionHandler := qdp.ConnectionHandlerFunc{
		OnConnectFunc: func(transport qdp.ITransport) {
			w.taskCh <- func() {
				qdb.Info("[MessageBroker::setupTcpTransport] Transport %s connected successfully", transportEntity.GetId())

				isConnected.PushBool(true)

				protocol := qdp.NewProtocol(transport, nil, msgHandler)
				w.protocolsByEntity[transportEntity.GetId()] = protocol

				protocol.StartReceiving(w.ctx)

				topics := qdb.NewEntityFinder(w.db).Find(qdb.SearchCriteria{
					EntityType: "QdpTopic",
				})

				for _, topicEntity := range topics {
					topic := topicEntity.GetField("Topic").PullString()

					transportReference := topicEntity.GetField("TransportReference").PullEntityReference()
					if transportReference != transportEntity.GetId() {
						continue
					}

					rxMessage := topicEntity.GetField("RxMessage")
					rxMessageFn := topicEntity.GetField("RxMessageFn")

					protocol.Subscribe(topic, qdp.MessageRxHandlerFunc(func(m *qdp.Message) {
						rxMessage.PushString(string(m.Payload))
						rxMessageFn.PushString(string(m.Payload))
					}))
				}
			}
		},

		OnDisconnectFunc: func(transport qdp.ITransport, err error) {
			w.taskCh <- func() {
				if err != nil {
					qdb.Error("[MessageBroker::setupTcpTransport] Transport %s disconnected with error: %v", transportEntity.GetId(), err)
				} else {
					qdb.Info("[MessageBroker::setupTcpTransport] Transport %s disconnected gracefully", transportEntity.GetId())
				}

				isConnected.PushBool(false)

				// Safely handle protocol cleanup
				if protocol, exists := w.protocolsByEntity[transportEntity.GetId()]; exists {
					protocol.Close()
					delete(w.protocolsByEntity, transportEntity.GetId())
				}
			}
		},
	}

	addr := transportEntity.GetField("Address").PullString()
	isClient := transportEntity.GetField("IsClient").PullBool()

	if isClient {
		_, err := qdp.NewTCPClientTransport(addr, connectionHandler)

		if err != nil {
			qdb.Error("[MessageBroker::setupTcpTransport] Failed to create client transport %s at %s: %v",
				transportEntity.GetId(), addr, err)
			return
		}
		qdb.Info("[MessageBroker::setupTcpTransport] Created client transport %s connecting to %s",
			transportEntity.GetId(), addr)
	} else {
		_, err := qdp.NewTCPServerTransport(addr, connectionHandler)

		if err != nil {
			qdb.Error("[MessageBroker::setupTcpTransport] Failed to create server transport %s listening on %s: %v",
				transportEntity.GetId(), addr, err)
			return
		}
		qdb.Info("[MessageBroker::setupTcpTransport] Created server transport %s listening on %s",
			transportEntity.GetId(), addr)
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
}

func (w *MessageBroker) teardown() {
	// Make a copy of protocol IDs to avoid map mutation during iteration
	ids := make([]string, 0, len(w.protocolsByEntity))
	for id := range w.protocolsByEntity {
		ids = append(ids, id)
	}

	// Close each protocol
	for _, id := range ids {
		if protocol, exists := w.protocolsByEntity[id]; exists {
			protocol.Close()
			delete(w.protocolsByEntity, id)
		}
	}

	// Clean up notification tokens
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
	w.cancel() // Cancel context first

	// Drain pending tasks with a timeout
	timeout := time.After(5 * time.Second)
	for {
		select {
		case task := <-w.taskCh:
			task()
		case <-timeout:
			return
		default:
			w.teardown()
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
	transportEntity := qdb.NewEntity(w.db, notification.Current.Id)
	isEnabled := qdb.ValueCast[*qdb.Bool](notification.Current.Value).Raw

	// Get existing protocol if any
	existingProtocol := w.protocolsByEntity[transportEntity.GetId()]

	// If disabled, cleanup existing protocol
	if !isEnabled && existingProtocol != nil {
		qdb.Info("[MessageBroker::onTcpTransportIsEnabledChanged] Disabling transport %s", transportEntity.GetId())
		existingProtocol.Close()
		delete(w.protocolsByEntity, transportEntity.GetId())
		return
	}

	// If already enabled with protocol, nothing to do
	if isEnabled && existingProtocol != nil {
		qdb.Debug("[MessageBroker::onTcpTransportIsEnabledChanged] Transport %s is already enabled", transportEntity.GetId())
		return
	}

	if isEnabled {
		qdb.Info("[MessageBroker::onTcpTransportIsEnabledChanged] Enabling transport %s", transportEntity.GetId())
		w.setupTcpTransport(transportEntity)
	}
}

func (w *MessageBroker) onTxMessage(notification *qdb.DatabaseNotification) {
	txMessage := qdb.ValueCast[*qdb.String](notification.Current.Value).Raw
	topic := qdb.ValueCast[*qdb.String](notification.Context[0].Value).Raw
	transportEntity := qdb.ValueCast[*qdb.EntityReference](notification.Context[1].Value).Raw

	protocol := w.protocolsByEntity[transportEntity]

	if protocol == nil {
		qdb.Error("[MessageBroker::onTxMessage] Cannot send message: transport %s not found or not connected", transportEntity)
		return
	}

	err := protocol.SendMessage(&qdp.Message{
		Topic:   topic,
		Payload: []byte(txMessage),
	})
	if err != nil {
		qdb.Error("[MessageBroker::onTxMessage] Failed to send message on topic '%s' to transport %s: %v",
			topic, transportEntity, err)
		return
	}
	qdb.Debug("[MessageBroker::onTxMessage] Successfully sent message on topic '%s' to transport %s",
		topic, transportEntity)
}
