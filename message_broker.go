package main

import (
	"context"

	qdp "github.com/rqure/qdp/lib/go"
	"github.com/rqure/qlib/pkg/app"
	"github.com/rqure/qlib/pkg/data"
	"github.com/rqure/qlib/pkg/data/binding"
	"github.com/rqure/qlib/pkg/data/notification"
	"github.com/rqure/qlib/pkg/data/query"
	"github.com/rqure/qlib/pkg/log"
)

type MessageBroker struct {
	store             data.Store
	isLeader          bool
	ctx               context.Context
	cancel            context.CancelFunc
	protocolsByEntity map[string]qdp.IProtocol
	tokens            []data.NotificationToken
	handle            app.Handle
}

func NewMessageBroker(store data.Store) *MessageBroker {
	ctx, cancel := context.WithCancel(context.Background())

	return &MessageBroker{
		store:    store,
		isLeader: false,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (w *MessageBroker) OnBecameLeader(ctx context.Context) {
	w.isLeader = true
	w.teardown(ctx)
	w.setup(ctx)
}

func (w *MessageBroker) OnLosingLeadership(ctx context.Context) {
	w.isLeader = false
	w.teardown(ctx)
}

func (w *MessageBroker) setupTcpTransport(ctx context.Context, transportEntity data.EntityBinding) {
	isConnected := transportEntity.GetField("IsConnected")
	isConnected.WriteBool(ctx, false)

	totalReceived := transportEntity.GetField("TotalReceived")
	totalSent := transportEntity.GetField("TotalSent")

	enabled := transportEntity.GetField("IsEnabled").ReadBool(ctx)

	if !enabled {
		log.Info("Transport %s is disabled", transportEntity.GetId())
		return
	}

	msgHandler := qdp.MessageHandlerFunc{
		OnMessageRxFunc: func(msg *qdp.Message) {
			w.handle.DoInMainThread(func(ctx context.Context) {
				if !w.isLeader {
					return
				}

				log.Debug("Transport %s received message on topic '%s' with %d bytes",
					transportEntity.GetId(), msg.Topic, len(msg.Payload))

				totalReceived.WriteInt(ctx, totalReceived.ReadInt(ctx)+1)
			})
		},

		OnMessageTxFunc: func(msg *qdp.Message) {
			w.handle.DoInMainThread(func(ctx context.Context) {
				if !w.isLeader {
					return
				}
				log.Debug("Transport %s sent message on topic '%s' with %d bytes",
					transportEntity.GetId(), msg.Topic, len(msg.Payload))

				totalSent.WriteInt(ctx, totalSent.ReadInt(ctx)+1)
			})
		},
	}

	connectionHandler := qdp.ConnectionHandlerFunc{
		OnConnectFunc: func(transport qdp.ITransport) {
			w.handle.DoInMainThread(func(ctx context.Context) {
				if !w.isLeader {
					return
				}
				log.Info("Transport %s connected successfully", transportEntity.GetId())

				isConnected.WriteBool(ctx, true)

				protocol := qdp.NewProtocol(transport, nil, msgHandler)
				w.protocolsByEntity[transportEntity.GetId()] = protocol

				protocol.StartReceiving(w.ctx)

				topics := query.New(w.store).ForType("QdpTopic").Execute(ctx)

				for _, topicEntity := range topics {
					topic := topicEntity.GetField("Topic").ReadString(ctx)

					transportReference := topicEntity.GetField("TransportReference").ReadEntityReference(ctx)
					if transportReference != transportEntity.GetId() {
						continue
					}

					rxMessage := topicEntity.GetField("RxMessage")
					rxMessageFn := topicEntity.GetField("RxMessageFn")

					protocol.Subscribe(topic, qdp.MessageRxHandlerFunc(func(m *qdp.Message) {
						rxMessage.WriteString(ctx, string(m.Payload))
						rxMessageFn.WriteString(ctx, string(m.Payload))
					}))
				}
			})
		},

		OnDisconnectFunc: func(transport qdp.ITransport, err error) {
			w.handle.DoInMainThread(func(ctx context.Context) {
				if !w.isLeader {
					return
				}
				if err != nil {
					log.Error("Transport %s disconnected with error: %v", transportEntity.GetId(), err)
				} else {
					log.Info("Transport %s disconnected gracefully", transportEntity.GetId())
				}

				isConnected.WriteBool(ctx, false)

				// Safely handle protocol cleanup
				if protocol, exists := w.protocolsByEntity[transportEntity.GetId()]; exists {
					protocol.Close()
					delete(w.protocolsByEntity, transportEntity.GetId())
				}
			})
		},
	}

	addr := transportEntity.GetField("Address").ReadString(ctx)
	isClient := transportEntity.GetField("IsClient").ReadBool(ctx)

	if isClient {
		_, err := qdp.NewTCPClientTransport(addr, connectionHandler)

		if err != nil {
			log.Error("Failed to create client transport %s at %s: %v",
				transportEntity.GetId(), addr, err)
			return
		}
		log.Info("Created client transport %s connecting to %s",
			transportEntity.GetId(), addr)
	} else {
		_, err := qdp.NewTCPServerTransport(addr, connectionHandler)

		if err != nil {
			log.Error("Failed to create server transport %s listening on %s: %v",
				transportEntity.GetId(), addr, err)
			return
		}
		log.Info("Created server transport %s listening on %s",
			transportEntity.GetId(), addr)
	}
}

func (w *MessageBroker) setupFtdiTransport(ctx context.Context, transportEntity data.EntityBinding) {
	isConnected := transportEntity.GetField("IsConnected")
	isConnected.WriteBool(ctx, false)

	totalReceived := transportEntity.GetField("TotalReceived")
	totalSent := transportEntity.GetField("TotalSent")

	enabled := transportEntity.GetField("IsEnabled").ReadBool(ctx)

	if !enabled {
		log.Info("Transport %s is disabled", transportEntity.GetId())
		return
	}

	msgHandler := qdp.MessageHandlerFunc{
		OnMessageRxFunc: func(msg *qdp.Message) {
			w.handle.DoInMainThread(func(ctx context.Context) {
				if !w.isLeader {
					return
				}
				log.Debug("Transport %s received message on topic '%s' with %d bytes",
					transportEntity.GetId(), msg.Topic, len(msg.Payload))
				totalReceived.WriteInt(ctx, totalReceived.ReadInt(ctx)+1)
			})
		},
		OnMessageTxFunc: func(msg *qdp.Message) {
			w.handle.DoInMainThread(func(ctx context.Context) {
				if !w.isLeader {
					return
				}
				log.Debug("Transport %s sent message on topic '%s' with %d bytes",
					transportEntity.GetId(), msg.Topic, len(msg.Payload))
				totalSent.WriteInt(ctx, totalSent.ReadInt(ctx)+1)
			})
		},
	}

	connectionHandler := qdp.ConnectionHandlerFunc{
		OnConnectFunc: func(transport qdp.ITransport) {
			w.handle.DoInMainThread(func(ctx context.Context) {
				if !w.isLeader {
					return
				}
				log.Info("Transport %s connected successfully", transportEntity.GetId())
				isConnected.WriteBool(ctx, true)

				protocol := qdp.NewProtocol(transport, nil, msgHandler)
				w.protocolsByEntity[transportEntity.GetId()] = protocol
				protocol.StartReceiving(w.ctx)

				// Subscribe to topics
				topics := query.New(w.store).ForType("QdpTopic").Execute(ctx)

				for _, topicEntity := range topics {
					topic := topicEntity.GetField("Topic").ReadString(ctx)
					transportReference := topicEntity.GetField("TransportReference").ReadEntityReference(ctx)
					if transportReference != transportEntity.GetId() {
						continue
					}

					rxMessage := topicEntity.GetField("RxMessage")
					rxMessageFn := topicEntity.GetField("RxMessageFn")

					protocol.Subscribe(topic, qdp.MessageRxHandlerFunc(func(m *qdp.Message) {
						rxMessage.WriteString(ctx, string(m.Payload))
						rxMessageFn.WriteString(ctx, string(m.Payload))
					}))
				}
			})
		},
		OnDisconnectFunc: func(transport qdp.ITransport, err error) {
			w.handle.DoInMainThread(func(ctx context.Context) {
				if !w.isLeader {
					return
				}
				if err != nil {
					log.Error("Transport %s disconnected with error: %v", transportEntity.GetId(), err)
				} else {
					log.Info("Transport %s disconnected gracefully", transportEntity.GetId())
				}
				isConnected.WriteBool(ctx, false)

				if protocol, exists := w.protocolsByEntity[transportEntity.GetId()]; exists {
					protocol.Close()
					delete(w.protocolsByEntity, transportEntity.GetId())
				}
			})
		},
	}

	// Get FTDI configuration
	vid := uint16(transportEntity.GetField("VendorID").ReadInt(ctx))
	pid := uint16(transportEntity.GetField("ProductID").ReadInt(ctx))
	iface := transportEntity.GetField("Interface").ReadInt(ctx)
	readEp := transportEntity.GetField("ReadEndpoint").ReadInt(ctx)
	writeEp := transportEntity.GetField("WriteEndpoint").ReadInt(ctx)

	// Get UART configuration
	baudRate := transportEntity.GetField("BaudRate").ReadInt(ctx)
	dataBits := transportEntity.GetField("DataBits").ReadInt(ctx)
	stopBits := transportEntity.GetField("StopBits").ReadInt(ctx)
	parity := transportEntity.GetField("Parity").ReadInt(ctx)
	flowControl := transportEntity.GetField("FlowControl").ReadInt(ctx)

	config := qdp.FTDIConfig{
		VID:         vid,
		PID:         pid,
		Interface:   int(iface),
		ReadEP:      int(readEp),
		WriteEP:     int(writeEp),
		BaudRate:    int(baudRate),
		DataBits:    int(dataBits),
		StopBits:    int(stopBits),
		Parity:      int(parity),
		FlowControl: int(flowControl),
	}

	_, err := qdp.NewFTDITransport(config, connectionHandler)
	if err != nil {
		log.Error("Failed to create FTDI transport %s: %v",
			transportEntity.GetId(), err)
		return
	}

	log.Info("Created FTDI transport %s with VID:PID %04x:%04x",
		transportEntity.GetId(), vid, pid)
}

func (w *MessageBroker) setup(ctx context.Context) {
	w.tokens = append(w.tokens, w.store.Notify(
		ctx,
		notification.NewConfig().
			SetEntityType("Root").
			SetFieldName("SchemaUpdateTrigger"),
		notification.NewCallback(w.OnSchemaUpdated)))

	w.tokens = append(w.tokens, w.store.Notify(
		ctx,
		notification.NewConfig().
			SetEntityType("QdpTcpTransport").
			SetFieldName("IsEnabled"),
		notification.NewCallback(w.onTcpTransportIsEnabledChanged)))

	w.tokens = append(w.tokens, w.store.Notify(
		ctx,
		notification.NewConfig().
			SetEntityType("QdpTopic").
			SetFieldName("TxMessage").
			SetContextFields([]string{
				"Topic",
				"TransportReference",
			}),
		notification.NewCallback(w.onTxMessage)))

	w.tokens = append(w.tokens, w.store.Notify(
		ctx,
		notification.NewConfig().
			SetEntityType("QdpFtdiTransport").
			SetFieldName("IsEnabled"),
		notification.NewCallback(w.onFtdiTransportIsEnabledChanged)))

	tcpTransports := query.New(w.store).ForType("QdpTcpTransport").Execute(ctx)

	for _, transportEntity := range tcpTransports {
		w.setupTcpTransport(ctx, transportEntity)
	}

	// Setup FTDI transports
	ftdiTransports := query.New(w.store).ForType("QdpFtdiTransport").Execute(ctx)
	for _, transportEntity := range ftdiTransports {
		w.setupFtdiTransport(ctx, transportEntity)
	}
}

func (w *MessageBroker) teardown(ctx context.Context) {
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
		token.Unbind(ctx)
	}
	w.tokens = make([]data.NotificationToken, 0)
}

func (w *MessageBroker) OnSchemaUpdated(ctx context.Context, _ data.Notification) {
	if !w.isLeader {
		return
	}

	w.teardown(ctx)
	w.setup(ctx)
}

func (w *MessageBroker) Init(ctx context.Context, h app.Handle) {
	w.handle = h
	w.protocolsByEntity = make(map[string]qdp.IProtocol)
}

func (w *MessageBroker) Deinit(ctx context.Context) {
	w.cancel()
	w.teardown(ctx)
}

func (w *MessageBroker) DoWork(context.Context) {

}

func (w *MessageBroker) onTcpTransportIsEnabledChanged(ctx context.Context, notification data.Notification) {
	transportEntity := binding.NewEntity(ctx, w.store, notification.GetCurrent().GetEntityId())
	isEnabled := notification.GetCurrent().GetValue().GetBool()

	// Get existing protocol if any
	existingProtocol := w.protocolsByEntity[transportEntity.GetId()]

	// If disabled, cleanup existing protocol
	if !isEnabled && existingProtocol != nil {
		log.Info("Disabling transport %s", transportEntity.GetId())
		existingProtocol.Close()
		delete(w.protocolsByEntity, transportEntity.GetId())
		return
	}

	// If already enabled with protocol, nothing to do
	if isEnabled && existingProtocol != nil {
		log.Debug("Transport %s is already enabled", transportEntity.GetId())
		return
	}

	if isEnabled {
		log.Info("Enabling transport %s", transportEntity.GetId())
		w.setupTcpTransport(ctx, transportEntity)
	}
}

func (w *MessageBroker) onFtdiTransportIsEnabledChanged(ctx context.Context, notification data.Notification) {
	transportEntity := binding.NewEntity(ctx, w.store, notification.GetCurrent().GetEntityId())
	isEnabled := notification.GetCurrent().GetValue().GetBool()

	// Get existing protocol
	existingProtocol := w.protocolsByEntity[transportEntity.GetId()]

	// If disabled, cleanup existing protocol
	if !isEnabled && existingProtocol != nil {
		log.Info("Disabling FTDI transport %s", transportEntity.GetId())
		existingProtocol.Close()
		delete(w.protocolsByEntity, transportEntity.GetId())
		return
	}

	// If already enabled with protocol, nothing to do
	if isEnabled && existingProtocol != nil {
		log.Debug("FTDI transport %s is already enabled", transportEntity.GetId())
		return
	}

	if isEnabled {
		log.Info("Enabling FTDI transport %s", transportEntity.GetId())
		w.setupFtdiTransport(ctx, transportEntity)
	}
}

func (w *MessageBroker) onTxMessage(ctx context.Context, notification data.Notification) {
	txMessage := notification.GetCurrent().GetValue().GetString()
	topic := notification.GetContext(0).GetValue().GetString()
	transportEntity := notification.GetContext(1).GetValue().GetEntityReference()

	protocol := w.protocolsByEntity[transportEntity]

	if protocol == nil {
		log.Error("Cannot send message: transport %s not found or not connected", transportEntity)
		return
	}

	err := protocol.SendMessage(&qdp.Message{
		Topic:   topic,
		Payload: []byte(txMessage),
	})
	if err != nil {
		log.Error("Failed to send message on topic '%s' to transport %s: %v",
			topic, transportEntity, err)
		return
	}
	log.Debug("Successfully sent message on topic '%s' to transport %s",
		topic, transportEntity)
}
