package qdp

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"sync"
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
	OnMessageSent(transport ITransport, msg *Message)
	OnMessageReceived(transport ITransport, msg *Message)
}

// ConnectionHandlerFunc is a function type that implements IConnectionHandler
type ConnectionHandlerFunc struct {
	OnConnectFunc         func(transport ITransport)
	OnDisconnectFunc      func(transport ITransport, err error)
	OnMessageSentFunc     func(transport ITransport, msg *Message)
	OnMessageReceivedFunc func(transport ITransport, msg *Message)
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

func (h ConnectionHandlerFunc) OnMessageSent(transport ITransport, msg *Message) {
	if h.OnMessageSentFunc != nil {
		h.OnMessageSentFunc(transport, msg)
	}
}

func (h ConnectionHandlerFunc) OnMessageReceived(transport ITransport, msg *Message) {
	if h.OnMessageReceivedFunc != nil {
		h.OnMessageReceivedFunc(transport, msg)
	}
}

// ITransport defines the interface for different transport mechanisms
type ITransport interface {
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
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

// IMessageHandler defines callback interface for message handlers
type IMessageHandler interface {
	HandleMessage(msg *Message)
}

// MessageHandlerFunc is a function type that implements IMessageHandler
type MessageHandlerFunc func(msg *Message)

func (f MessageHandlerFunc) HandleMessage(msg *Message) {
	f(msg)
}

// Subscription represents a topic subscription
type subscription struct {
	topic   string
	handler IMessageHandler
}

// ISubscriptionManager defines the interface for managing subscriptions
type ISubscriptionManager interface {
	Subscribe(topic string, handler IMessageHandler)
	Unsubscribe(topic string, handler IMessageHandler)
	HandleMessage(msg *Message)
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

func (sm *DefaultSubscriptionManager) Subscribe(topic string, handler IMessageHandler) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.subscriptions = append(sm.subscriptions, subscription{topic, handler})
}

func (sm *DefaultSubscriptionManager) Unsubscribe(topic string, handler IMessageHandler) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for i := len(sm.subscriptions) - 1; i >= 0; i-- {
		sub := sm.subscriptions[i]
		if sub.topic == topic && sub.handler == handler {
			sm.subscriptions = append(sm.subscriptions[:i], sm.subscriptions[i+1:]...)
		}
	}
}

func (sm *DefaultSubscriptionManager) HandleMessage(msg *Message) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for _, sub := range sm.subscriptions {
		if matchTopic(sub.topic, msg.Topic) {
			sub.handler.HandleMessage(msg)
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
	SetSubscriptionManager(manager ISubscriptionManager)
	SendMessage(msg *Message) error
	ReceiveMessage() (*Message, error)
	Subscribe(topic string, handler IMessageHandler)
	Unsubscribe(topic string, handler IMessageHandler)
	PublishMessage(topic string, payload []byte) error
	StartReceiving(ctx context.Context)
	Close() error
	SetConnectionHandler(handler IConnectionHandler)
}

// DefaultProtocol handles reading and writing messages over a transport
type DefaultProtocol struct {
	transport         ITransport
	subscribers       ISubscriptionManager
	connectionHandler IConnectionHandler
	cancel            context.CancelFunc
	wg                sync.WaitGroup
}

// NewProtocol creates a new Protocol instance
func NewProtocol(transport ITransport, subscribers ISubscriptionManager) IProtocol {
	if subscribers == nil {
		subscribers = NewSubscriptionManager()
	}

	return &DefaultProtocol{
		transport:   transport,
		subscribers: subscribers,
	}
}

// SetSubscriptionManager sets the subscription manager
func (p *DefaultProtocol) SetSubscriptionManager(manager ISubscriptionManager) {
	p.subscribers = manager
}

// SetConnectionHandler sets the connection handler
func (p *DefaultProtocol) SetConnectionHandler(handler IConnectionHandler) {
	p.connectionHandler = handler
}

// SendMessage sends a message over the transport
func (p *DefaultProtocol) SendMessage(msg *Message) error {
	data, err := msg.Encode()
	if err != nil {
		return err
	}
	_, err = p.transport.Write(data)
	if err == nil && p.connectionHandler != nil {
		p.connectionHandler.OnMessageSent(p.transport, msg)
	}
	return err
}

// ReceiveMessage reads a message from the transport
func (p *DefaultProtocol) ReceiveMessage() (*Message, error) {
	// Read header first to determine message size
	header := make([]byte, 8)
	if _, err := io.ReadFull(p.transport, header); err != nil {
		return nil, err
	}

	topicLen := binary.LittleEndian.Uint32(header[0:4])
	payloadLen := binary.LittleEndian.Uint32(header[4:8])

	// Read the rest of the message
	remainingLen := topicLen + payloadLen + 4 // +4 for CRC
	data := make([]byte, 8+remainingLen)
	copy(data, header)

	if _, err := io.ReadFull(p.transport, data[8:]); err != nil {
		return nil, err
	}

	return Decode(data)
}

// Subscribe adds a message handler for a topic pattern
func (p *DefaultProtocol) Subscribe(topic string, handler IMessageHandler) {
	p.subscribers.Subscribe(topic, handler)
}

// Unsubscribe removes a message handler for a topic pattern
func (p *DefaultProtocol) Unsubscribe(topic string, handler IMessageHandler) {
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
	if p.connectionHandler != nil {
		p.connectionHandler.OnMessageReceived(p.transport, msg)
	}
	p.subscribers.HandleMessage(msg)
}

// StartReceiving starts a goroutine to receive and handle messages
func (p *DefaultProtocol) StartReceiving(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.wg.Add(1)

	go func() {
		defer p.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				msg, err := p.ReceiveMessage()
				if err != nil {
					continue
				}
				p.handleMessage(msg)
			}
		}
	}()
}

// Close gracefully shuts down the protocol and its transport
func (p *DefaultProtocol) Close() error {
	if p.cancel != nil {
		p.cancel()
		p.wg.Wait()
	}
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
