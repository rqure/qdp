package qdp

import (
	"errors"
	"strings"
)

const (
	MaxBufferCapacity = 1024 * 10
	MaxSubscriptions  = 64
	MaxTopicLength    = 256
	MaxPayloadSize    = 512
)

// Topic pattern matching special characters
const (
	SingleLevelWildcard = '+'
	MultiLevelWildcard  = '#'
	TopicLevelSeparator = '/'
)

// Buffer management
type Buffer struct {
	data     []byte
	size     int
	capacity int
	position int
}

// Message components
type Header struct {
	TopicLen   uint32
	PayloadLen uint32
	Topic      string
}

type Payload struct {
	Size uint32
	Data []byte
}

type Message struct {
	Header   Header
	Payload  Payload
	Checksum uint32
}

// Stream context
type Stream struct {
	buffer Buffer
}

// Callback system
type Callback struct {
	Fn  func(*Message, interface{})
	Ctx interface{}
}

type Subscription struct {
	Topic    string
	Callback Callback
}

// Protocol errors
var (
	ErrBufferFull      = errors.New("buffer full")
	ErrPayloadTooLarge = errors.New("payload too large")
)

// Main context
type QDP struct {
	transport struct {
		send func(*Buffer, interface{}) error
		recv func(*Buffer, interface{}) error
		ctx  interface{}
	}

	subs   []Subscription
	rx     Stream
	tx     Stream
	rxData []byte
	txData []byte
}

// Create new QDP instance
func New() *QDP {
	q := &QDP{
		subs:   make([]Subscription, 0, MaxSubscriptions),
		rxData: make([]byte, MaxBufferCapacity),
		txData: make([]byte, MaxBufferCapacity),
	}
	q.rx.buffer.data = q.rxData
	q.rx.buffer.capacity = MaxBufferCapacity
	q.tx.buffer.data = q.txData
	q.tx.buffer.capacity = MaxBufferCapacity
	return q
}

// Set transport functions
func (q *QDP) SetTransport(send func(*Buffer, interface{}) error,
	recv func(*Buffer, interface{}) error,
	ctx interface{}) {
	q.transport.send = send
	q.transport.recv = recv
	q.transport.ctx = ctx
}

// Subscribe to a topic pattern
func (q *QDP) Subscribe(pattern string, fn func(*Message, interface{}), ctx interface{}) bool {
	if len(q.subs) >= MaxSubscriptions {
		return false
	}
	q.subs = append(q.subs, Subscription{
		Topic:    pattern,
		Callback: Callback{Fn: fn, Ctx: ctx},
	})
	return true
}

// Unsubscribe from a topic pattern
func (q *QDP) Unsubscribe(pattern string) {
	for i := 0; i < len(q.subs); i++ {
		if q.subs[i].Topic == pattern {
			q.subs = append(q.subs[:i], q.subs[i+1:]...)
			i--
		}
	}
}

// Publish string message
func (q *QDP) PublishString(topic string, str string) error {
	if len(str) > MaxPayloadSize {
		return ErrPayloadTooLarge
	}

	msg := Message{
		Header: Header{
			TopicLen:   uint32(len(topic)),
			PayloadLen: uint32(len(str)),
			Topic:      topic,
		},
		Payload: Payload{
			Size: uint32(len(str)),
			Data: []byte(str),
		},
	}

	q.tx.buffer.Reset()
	if err := q.tx.WriteMessage(&msg); err != nil {
		return err
	}

	if q.transport.send != nil {
		return q.transport.send(&q.tx.buffer, q.transport.ctx)
	}
	return nil
}

// Process incoming messages
func (q *QDP) Process() error {
	if q.transport.recv == nil {
		return nil
	}

	q.rx.buffer.Reset()
	if err := q.transport.recv(&q.rx.buffer, q.transport.ctx); err != nil {
		return err
	}

	var msg Message
	for q.rx.NextMessage(&msg) {
		// Match against subscriptions
		for _, sub := range q.subs {
			if TopicMatches(sub.Topic, msg.Header.Topic) {
				sub.Callback.Fn(&msg, sub.Callback.Ctx)
			}
		}
	}
	return nil
}

// Topic pattern matching
func TopicMatches(pattern, topic string) bool {
	if pattern == "#" {
		return true
	}

	parts := strings.Split(pattern, string(TopicLevelSeparator))
	topics := strings.Split(topic, string(TopicLevelSeparator))

	if len(parts) > len(topics) && parts[len(parts)-1] != "#" {
		return false
	}

	for i := 0; i < len(parts); i++ {
		if i >= len(topics) {
			return false
		}

		if parts[i] == "#" {
			return i == len(parts)-1
		}

		if parts[i] != "+" && parts[i] != topics[i] {
			return false
		}
	}

	return len(parts) == len(topics) ||
		(len(parts) == len(topics)-1 && parts[len(parts)-1] == "#")
}

// Buffer methods
func (b *Buffer) Reset() {
	b.size = 0
	b.position = 0
}

func (b *Buffer) CanRead(bytes int) bool {
	return b.position+bytes <= b.size
}

func (b *Buffer) CanWrite(bytes int) bool {
	return b.size+bytes <= b.capacity
}

// Stream methods
func (s *Stream) WriteMessage(msg *Message) error {
	totalSize := 4 + 4 + len(msg.Header.Topic) + len(msg.Payload.Data) + 4
	if !s.buffer.CanWrite(totalSize) {
		return ErrBufferFull
	}

	// Write message
	writeUint32(s.buffer.data[s.buffer.size:], msg.Header.TopicLen)
	s.buffer.size += 4
	writeUint32(s.buffer.data[s.buffer.size:], uint32(len(msg.Payload.Data)))
	s.buffer.size += 4
	copy(s.buffer.data[s.buffer.size:], msg.Header.Topic)
	s.buffer.size += len(msg.Header.Topic)
	copy(s.buffer.data[s.buffer.size:], msg.Payload.Data)
	s.buffer.size += len(msg.Payload.Data)

	// Calculate and write checksum
	checksum := crc32(s.buffer.data[:s.buffer.size])
	writeUint32(s.buffer.data[s.buffer.size:], checksum)
	s.buffer.size += 4

	return nil
}

func (s *Stream) NextMessage(msg *Message) bool {
	if s.buffer.position >= s.buffer.size {
		return false
	}
	return s.ReadMessage(msg)
}

// Helper functions for binary operations
func writeUint32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func readUint32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// CRC32 calculation
func crc32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc = (crc >> 8) ^ qdp_crc32_table[byte(crc)^b]
	}
	return crc ^ 0xFFFFFFFF
}

// Add to Stream type
func (s *Stream) ReadMessage(msg *Message) bool {
	if s.buffer.position >= s.buffer.size {
		return false
	}

	// Read header
	if !s.buffer.CanRead(8) { // size of two uint32s
		return false
	}

	msg.Header.TopicLen = readUint32(s.buffer.data[s.buffer.position:])
	s.buffer.position += 4
	msg.Header.PayloadLen = readUint32(s.buffer.data[s.buffer.position:])
	s.buffer.position += 4

	// Read topic
	if !s.buffer.CanRead(int(msg.Header.TopicLen)) {
		return false
	}
	msg.Header.Topic = string(s.buffer.data[s.buffer.position : s.buffer.position+int(msg.Header.TopicLen)])
	s.buffer.position += int(msg.Header.TopicLen)

	// Read payload
	if !s.buffer.CanRead(int(msg.Header.PayloadLen) + 4) { // payload + checksum
		return false
	}
	msg.Payload.Data = make([]byte, msg.Header.PayloadLen)
	copy(msg.Payload.Data, s.buffer.data[s.buffer.position:s.buffer.position+int(msg.Header.PayloadLen)])
	msg.Payload.Size = msg.Header.PayloadLen
	s.buffer.position += int(msg.Header.PayloadLen)

	// Read and verify checksum
	msg.Checksum = readUint32(s.buffer.data[s.buffer.position:])
	s.buffer.position += 4

	return true
}

// Initialize CRC table
var qdp_crc32_table [256]uint32

func init() {
	for i := 0; i < 256; i++ {
		crc := uint32(i)
		for j := 0; j < 8; j++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xEDB88320
			} else {
				crc >>= 1
			}
		}
		qdp_crc32_table[i] = crc
	}
}
