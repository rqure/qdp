package qdp

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"time"
)

// Common errors
var (
	ErrInvalidMessage = errors.New("invalid message format")
	ErrCRCMismatch    = errors.New("CRC checksum mismatch")
)

// IConnectionHandler defines callbacks for transport connection status
type IConnectionHandler interface {
	OnConnect(transport ITransport)
	OnDisconnect(transport ITransport, err error)
}

// ConnectionHandlerFunc is a function type that implements IConnectionHandler
type ConnectionHandlerFunc struct {
	OnConnectFunc    func(transport ITransport)
	OnDisconnectFunc func(transport ITransport, err error)
}

func (h ConnectionHandlerFunc) OnConnect(transport ITransport) {
	if h.OnConnectFunc != nil {
		h.OnConnectFunc(transport)
	}
}

func (h ConnectionHandlerFunc) OnDisconnect(transport ITransport, err error) {
	if h.OnDisconnectFunc != nil {
		h.OnDisconnectFunc(transport, err)
	}
}

// ITransport defines the interface for different transport mechanisms
type ITransport interface {
	ReadMessage() (*Message, error)
	WriteMessage(*Message) error
	Close() error
}

// Message represents a QDP protocol message
type Message struct {
	Topic   string
	Payload []byte
}

// Encode serializes a Message into a byte slice with CRC
func (m *Message) Encode() ([]byte, error) {
	if m.Topic == "" {
		return nil, ErrInvalidMessage
	}

	topicLen := uint32(len(m.Topic))
	payloadLen := uint32(len(m.Payload))

	// Calculate total message size
	totalSize := 8 + // TopicLength + PayloadLength fields
		topicLen +
		payloadLen +
		4 // CRC32

	buf := make([]byte, totalSize)
	offset := 0

	// Write header
	binary.LittleEndian.PutUint32(buf[offset:], topicLen)
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], payloadLen)
	offset += 4

	// Write topic
	copy(buf[offset:], m.Topic)
	offset += int(topicLen)

	// Write payload
	copy(buf[offset:], m.Payload)
	offset += int(payloadLen)

	// Calculate and write CRC
	crc := calculateCRC32(buf[:offset])
	binary.LittleEndian.PutUint32(buf[offset:], crc)

	return buf, nil
}

// Decode deserializes a byte slice into a Message, verifying CRC
func Decode(data []byte) (*Message, error) {
	if len(data) < 12 { // minimum size: 4 + 4 + 0 + 0 + 4 (lengths + CRC)
		return nil, ErrInvalidMessage
	}

	// Read header
	topicLen := binary.LittleEndian.Uint32(data[0:4])
	payloadLen := binary.LittleEndian.Uint32(data[4:8])

	totalLen := 8 + topicLen + payloadLen + 4
	if uint32(len(data)) != totalLen {
		return nil, ErrInvalidMessage
	}

	// Verify CRC
	receivedCRC := binary.LittleEndian.Uint32(data[totalLen-4:])
	calculatedCRC := calculateCRC32(data[:totalLen-4])
	if receivedCRC != calculatedCRC {
		return nil, ErrCRCMismatch
	}

	// Extract topic and payload
	topic := string(data[8 : 8+topicLen])
	payload := data[8+topicLen : totalLen-4]

	return &Message{
		Topic:   topic,
		Payload: payload,
	}, nil
}

// IMessageRxHandler defines callback interface for message handlers
type IMessageRxHandler interface {
	OnMessageRx(msg *Message)
}

// IMessageTxHandler defines callback interface for message handlers
type IMessageTxHandler interface {
	OnMessageTx(msg *Message)
}

// IMessageHandler defines the interface for message handlers
type IMessageHandler interface {
	IMessageRxHandler
	IMessageTxHandler
}

// MessageRxHandlerFunc is a function type that implements IMessageRxHandler
type MessageRxHandlerFunc func(msg *Message)

func (f MessageRxHandlerFunc) OnMessageRx(msg *Message) {
	f(msg)
}

// MessageTxHandlerFunc is a function type that implements IMessageTxHandler
type MessageTxHandlerFunc func(msg *Message)

func (f MessageTxHandlerFunc) OnMessageTx(msg *Message) {
	f(msg)
}

type MessageHandlerFunc struct {
	OnMessageRxFunc func(msg *Message)
	OnMessageTxFunc func(msg *Message)
}

func (f MessageHandlerFunc) OnMessageRx(msg *Message) {
	if f.OnMessageRxFunc != nil {
		f.OnMessageRxFunc(msg)
	}
}

func (f MessageHandlerFunc) OnMessageTx(msg *Message) {
	if f.OnMessageTxFunc != nil {
		f.OnMessageTxFunc(msg)
	}
}

// Subscription represents a topic subscription
type subscription struct {
	topic   string
	handler IMessageRxHandler
}

// ISubscriptionManager defines the interface for managing subscriptions
type ISubscriptionManager interface {
	Subscribe(topic string, handler IMessageRxHandler)
	Unsubscribe(topic string, handler IMessageRxHandler)
	OnMessageRx(msg *Message)
}

// DefaultSubscriptionManager implements ISubscriptionManager
type DefaultSubscriptionManager struct {
	subscriptions []subscription
	mu            sync.RWMutex
}

func NewSubscriptionManager() ISubscriptionManager {
	return &DefaultSubscriptionManager{
		subscriptions: make([]subscription, 0),
	}
}

func (sm *DefaultSubscriptionManager) Subscribe(topic string, handler IMessageRxHandler) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.subscriptions = append(sm.subscriptions, subscription{topic, handler})
}

func (sm *DefaultSubscriptionManager) Unsubscribe(topic string, handler IMessageRxHandler) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for i := len(sm.subscriptions) - 1; i >= 0; i-- {
		sub := sm.subscriptions[i]
		if sub.topic == topic && sub.handler == handler {
			sm.subscriptions = append(sm.subscriptions[:i], sm.subscriptions[i+1:]...)
		}
	}
}

func (sm *DefaultSubscriptionManager) OnMessageRx(msg *Message) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for _, sub := range sm.subscriptions {
		if matchTopic(sub.topic, msg.Topic) {
			sub.handler.OnMessageRx(msg)
		}
	}
}

// matchTopic checks if a topic matches a pattern with wildcards
func matchTopic(pattern, topic string) bool {
	if pattern == "#" {
		return true
	}

	patternParts := strings.Split(pattern, "/")
	topicParts := strings.Split(topic, "/")

	if len(patternParts) > len(topicParts) && patternParts[len(patternParts)-1] != "#" {
		return false
	}

	for i := 0; i < len(patternParts); i++ {
		if i >= len(topicParts) {
			return false
		}
		if patternParts[i] == "#" {
			return true
		}
		if patternParts[i] != "+" && patternParts[i] != topicParts[i] {
			return false
		}
	}

	return len(topicParts) == len(patternParts)
}

// IProtocol defines the interface for the QDP protocol implementation
type IProtocol interface {
	SendMessage(msg *Message) error
	ReceiveMessage() (*Message, error)
	Subscribe(topic string, handler IMessageRxHandler)
	Unsubscribe(topic string, handler IMessageRxHandler)
	PublishMessage(topic string, payload []byte) error
	StartReceiving(ctx context.Context)
	Close() error
}

// DefaultProtocol handles reading and writing messages over a transport
type DefaultProtocol struct {
	transport   ITransport
	subscribers ISubscriptionManager
	msgCh       chan *Message
	msgHandler  IMessageHandler
	ctx         context.Context
	cancel      context.CancelFunc
}

func NewProtocol(transport ITransport, subscribers ISubscriptionManager, msgHandler IMessageHandler) IProtocol {
	if subscribers == nil {
		subscribers = NewSubscriptionManager()
	}

	if msgHandler == nil {
		msgHandler = &MessageHandlerFunc{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &DefaultProtocol{
		transport:   transport,
		subscribers: subscribers,
		msgHandler:  msgHandler,
		msgCh:       make(chan *Message, 100),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// SendMessage sends a message over the transport
func (p *DefaultProtocol) SendMessage(msg *Message) error {
	err := p.transport.WriteMessage(msg)

	if err == nil {
		p.msgHandler.OnMessageTx(msg)
	}

	return err
}

// ReceiveMessage reads a message from the transport
func (p *DefaultProtocol) ReceiveMessage() (*Message, error) {
	return p.transport.ReadMessage()
}

// Subscribe adds a message handler for a topic pattern
func (p *DefaultProtocol) Subscribe(topic string, handler IMessageRxHandler) {
	p.subscribers.Subscribe(topic, handler)
}

// Unsubscribe removes a message handler for a topic pattern
func (p *DefaultProtocol) Unsubscribe(topic string, handler IMessageRxHandler) {
	p.subscribers.Unsubscribe(topic, handler)
}

// PublishMessage publishes a message to a topic
func (p *DefaultProtocol) PublishMessage(topic string, payload []byte) error {
	msg := &Message{
		Topic:   topic,
		Payload: payload,
	}
	return p.SendMessage(msg)
}

// handleMessage routes received messages to subscribers
func (p *DefaultProtocol) handleMessage(msg *Message) {
	p.msgHandler.OnMessageRx(msg)
	p.subscribers.OnMessageRx(msg)
}

// StartReceiving starts message processing loop
func (p *DefaultProtocol) StartReceiving(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)

	// Start receive loop
	go func() {
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				msg, err := p.ReceiveMessage()
				if err != nil {
					select {
					case <-ctx.Done():
						return
					default:
						time.Sleep(100 * time.Millisecond)
						continue
					}
				}
				select {
				case p.msgCh <- msg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	// Start process loop
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-p.msgCh:
				p.handleMessage(msg)
			}
		}
	}()
}

// Close cancels the context and closes the transport
func (p *DefaultProtocol) Close() error {
	p.cancel() // Cancel our own context first
	return p.transport.Close()
}

var crc32Table [256]uint32

func init() {
	// Initialize CRC32 table
	polynomial := uint32(0xEDB88320)
	for i := uint32(0); i < 256; i++ {
		crc := i
		for j := 0; j < 8; j++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ polynomial
			} else {
				crc >>= 1
			}
		}
		crc32Table[i] = crc
	}
}

func calculateCRC32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc = (crc >> 8) ^ crc32Table[byte(crc)^b]
	}
	return crc ^ 0xFFFFFFFF
}
