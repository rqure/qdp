package qdp

import (
	"bytes"
	"testing"
)

// Mock transport for testing
type mockTransport struct {
	data []byte
	size int
}

func (m *mockTransport) send(buf *Buffer, _ interface{}) error {
	if m.size+buf.size > len(m.data) {
		return ErrBufferFull
	}
	copy(m.data[m.size:], buf.data[:buf.size])
	m.size = buf.size
	buf.size = 0
	return nil
}

func (m *mockTransport) recv(buf *Buffer, _ interface{}) error {
	if m.size == 0 {
		return ErrBufferFull
	}
	copy(buf.data, m.data[:m.size])
	buf.size = m.size
	buf.position = 0
	m.size = 0
	return nil
}

// Buffer tests
func TestBufferOperations(t *testing.T) {
	data := make([]byte, 128)
	buf := Buffer{
		data:     data,
		size:     0,
		capacity: len(data),
		position: 0,
	}

	if !buf.CanWrite(64) {
		t.Error("Should be able to write 64 bytes")
	}
	if !buf.CanWrite(128) {
		t.Error("Should be able to write 128 bytes")
	}
	if buf.CanWrite(129) {
		t.Error("Should not be able to write 129 bytes")
	}

	buf.size = 100
	if !buf.CanRead(50) {
		t.Error("Should be able to read 50 bytes")
	}
	if !buf.CanRead(100) {
		t.Error("Should be able to read 100 bytes")
	}
	if buf.CanRead(101) {
		t.Error("Should not be able to read 101 bytes")
	}

	buf.Reset()
	if buf.size != 0 || buf.position != 0 {
		t.Error("Buffer reset failed")
	}
}

// Message tests
func TestMessageOperations(t *testing.T) {
	buffer := make([]byte, 1024)
	stream := Stream{
		buffer: Buffer{
			data:     buffer,
			capacity: len(buffer),
		},
	}

	topic := "test/topic"
	payload := []byte("test payload")

	msg := Message{
		Header: Header{
			TopicLen:   uint32(len(topic)),
			PayloadLen: uint32(len(payload)),
			Topic:      topic,
		},
		Payload: Payload{
			Size: uint32(len(payload)),
			Data: make([]byte, len(payload)),
		},
	}
	copy(msg.Payload.Data, payload)

	err := stream.WriteMessage(&msg)
	if err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	var readMsg Message
	readMsg.Payload.Data = make([]byte, MaxPayloadSize) // Pre-allocate payload buffer
	stream.buffer.position = 0
	if !stream.ReadMessage(&readMsg) {
		t.Fatal("Failed to read message")
	}

	if msg.Header.Topic != readMsg.Header.Topic {
		t.Errorf("Topic mismatch: got %q, want %q", readMsg.Header.Topic, msg.Header.Topic)
	}
	if !bytes.Equal(msg.Payload.Data[:msg.Payload.Size], readMsg.Payload.Data[:readMsg.Payload.Size]) {
		t.Errorf("Payload mismatch: got %q, want %q",
			readMsg.Payload.Data[:readMsg.Payload.Size],
			msg.Payload.Data[:msg.Payload.Size])
	}
}

// Protocol tests
func TestProtocolPubSub(t *testing.T) {
	q := New()
	mock := &mockTransport{
		data: make([]byte, 1024),
	}

	var receivedMsg Message
	callbackCalled := false

	callback := func(msg *Message, _ interface{}) {
		receivedMsg = *msg
		callbackCalled = true
	}

	q.SetTransport(mock.send, mock.recv, nil)
	if !q.Subscribe("test/topic", callback, nil) {
		t.Fatal("Failed to subscribe")
	}

	if err := q.PublishString("test/topic", "Hello"); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	if err := q.Process(); err != nil {
		t.Fatalf("Failed to process: %v", err)
	}

	if !callbackCalled {
		t.Error("Callback was not called")
	}

	payload := string(receivedMsg.Payload.Data[:receivedMsg.Payload.Size])
	if payload != "Hello" {
		t.Errorf("Expected 'Hello', got '%s'", payload)
	}
}

// Topic pattern matching tests
func TestTopicMatching(t *testing.T) {
	tests := []struct {
		pattern string
		topic   string
		matches bool
	}{
		// Exact matches
		{"a/b/c", "a/b/c", true},
		{"a/b/c", "a/b/d", false},

		// Single-level wildcard
		{"a/+/c", "a/b/c", true},
		{"a/+/+", "a/b/c", true},
		{"a/+/c", "a/b/d", false},
		{"a/+/c", "a/b/c/d", false},

		// Multi-level wildcard
		{"a/#", "a/b/c", true},
		{"a/b/#", "a/b/c/d", true},
		{"a/#/c", "a/b/c", false}, // Invalid pattern

		// Mixed wildcards
		{"a/+/+/#", "a/b/c/d/e", true},
		{"a/+/+/#", "a/b", false},

		// Edge cases
		{"", "a/b/c", false},
		{"a/b/c", "", false},
		{"#", "a/b/c", true},
		{"+", "a", true},
	}

	for _, tt := range tests {
		result := TopicMatches(tt.pattern, tt.topic)
		if result != tt.matches {
			t.Errorf("TopicMatches(%q, %q) = %v; want %v",
				tt.pattern, tt.topic, result, tt.matches)
		}
	}
}

// Advanced protocol tests
func TestProtocolAdvanced(t *testing.T) {
	q := New()
	mock := &mockTransport{
		data: make([]byte, 1024*4),
	}

	var lastTopic string
	callbackCount := 0

	callback := func(msg *Message, _ interface{}) {
		callbackCount++
		lastTopic = msg.Header.Topic
	}

	q.SetTransport(mock.send, mock.recv, nil)

	// Multiple subscriptions
	if !q.Subscribe("sensor/+/temp", callback, nil) {
		t.Fatal("Failed to subscribe to sensor/+/temp")
	}
	if !q.Subscribe("control/#", callback, nil) {
		t.Fatal("Failed to subscribe to control/#")
	}

	// Test first message
	mock.size = 0
	if err := q.PublishString("sensor/1/temp", "25.5"); err != nil {
		t.Fatal(err)
	}
	if err := q.Process(); err != nil {
		t.Fatal(err)
	}
	if callbackCount != 1 {
		t.Errorf("Expected callback count 1, got %d", callbackCount)
	}
	if lastTopic != "sensor/1/temp" {
		t.Errorf("Expected topic sensor/1/temp, got %s", lastTopic)
	}

	// Test second message
	mock.size = 0
	if err := q.PublishString("control/pump/on", "1"); err != nil {
		t.Fatal(err)
	}
	if err := q.Process(); err != nil {
		t.Fatal(err)
	}
	if callbackCount != 2 {
		t.Errorf("Expected callback count 2, got %d", callbackCount)
	}
	if lastTopic != "control/pump/on" {
		t.Errorf("Expected topic control/pump/on, got %s", lastTopic)
	}

	// Test unsubscribe
	q.Unsubscribe("sensor/+/temp")
	mock.size = 0
	if err := q.PublishString("sensor/1/temp", "26.5"); err != nil {
		t.Fatal(err)
	}
	if err := q.Process(); err != nil {
		t.Fatal(err)
	}
	if callbackCount != 2 {
		t.Errorf("Expected callback count to remain at 2, got %d", callbackCount)
	}
}
