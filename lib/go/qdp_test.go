package qdp

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
)

// mockTransport implements ITransport for testing
type mockTransport struct {
	readCh  chan *Message
	writeCh chan *Message
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		readCh:  make(chan *Message, 10),
		writeCh: make(chan *Message, 10),
	}
}

func (m *mockTransport) ReadMessage() (*Message, error) {
	msg := <-m.readCh
	return msg, nil
}

func (m *mockTransport) WriteMessage(msg *Message) error {
	m.writeCh <- msg
	// Simulate echo back for testing
	m.readCh <- msg
	return nil
}

func (m *mockTransport) Close() error {
	close(m.readCh)
	close(m.writeCh)
	return nil
}

func TestMessageEncodeDecode(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		wantErr bool
	}{
		{
			name: "basic message",
			message: Message{
				Topic:   "temperature",
				Payload: []byte("25.5"),
			},
			wantErr: false,
		},
		{
			name: "empty payload",
			message: Message{
				Topic:   "status",
				Payload: []byte{},
			},
			wantErr: false,
		},
		{
			name: "empty topic",
			message: Message{
				Topic:   "",
				Payload: []byte("data"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := tt.message.Encode()
			if (err != nil) != tt.wantErr {
				t.Errorf("Message.Encode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			decoded, err := Decode(encoded)
			if err != nil {
				t.Errorf("Decode() error = %v", err)
				return
			}

			if decoded.Topic != tt.message.Topic {
				t.Errorf("Topic mismatch: got %v, want %v", decoded.Topic, tt.message.Topic)
			}
			if !bytes.Equal(decoded.Payload, tt.message.Payload) {
				t.Errorf("Payload mismatch: got %v, want %v", decoded.Payload, tt.message.Payload)
			}
		})
	}
}

func TestProtocolSendReceive(t *testing.T) {
	transport := newMockTransport()
	protocol := NewProtocol(transport, nil, nil)

	// Test message
	msg := &Message{
		Topic:   "test",
		Payload: []byte("hello"),
	}

	// Send message
	if err := protocol.SendMessage(msg); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	// Receive message
	received, err := protocol.ReceiveMessage()
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}

	// Verify received message
	if received.Topic != msg.Topic {
		t.Errorf("Topic mismatch: got %v, want %v", received.Topic, msg.Topic)
	}
	if !bytes.Equal(received.Payload, msg.Payload) {
		t.Errorf("Payload mismatch: got %v, want %v", received.Payload, msg.Payload)
	}
}

func TestTopicMatching(t *testing.T) {
	tests := []struct {
		pattern string
		topic   string
		matches bool
	}{
		{"sensor/temp", "sensor/temp", true},
		{"sensor/+/temp", "sensor/1/temp", true},
		{"sensor/+/temp", "sensor/2/temp", true},
		{"sensor/+/temp", "sensor/temp", false},
		{"sensor/#", "sensor/temp", true},
		{"sensor/#", "sensor/temp/value", true},
		{"sensor/+/temp", "sensor/1/humidity", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s-%s", tt.pattern, tt.topic), func(t *testing.T) {
			if got := matchTopic(tt.pattern, tt.topic); got != tt.matches {
				t.Errorf("matchTopic(%q, %q) = %v, want %v", tt.pattern, tt.topic, got, tt.matches)
			}
		})
	}
}

func TestSubscription(t *testing.T) {
	transport := newMockTransport()
	protocol := NewProtocol(transport, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan *Message, 1)
	handler := MessageRxHandlerFunc(func(msg *Message) {
		received <- msg
	})

	// Subscribe to temperature topics
	protocol.Subscribe("sensor/+/temperature", handler)

	// Start receiving messages
	protocol.StartReceiving(ctx)

	// Test message
	msg := &Message{
		Topic:   "sensor/1/temperature",
		Payload: []byte("25.5"),
	}

	// Simulate sending message
	if err := protocol.SendMessage(msg); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	transport.readCh <- msg

	// Wait for message
	select {
	case receivedMsg := <-received:
		if receivedMsg.Topic != msg.Topic {
			t.Errorf("Topic mismatch: got %v, want %v", receivedMsg.Topic, msg.Topic)
		}
		if !bytes.Equal(receivedMsg.Payload, msg.Payload) {
			t.Errorf("Payload mismatch: got %v, want %v", receivedMsg.Payload, msg.Payload)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for message")
	}
}

func TestSharedSubscriptionManager(t *testing.T) {
	sharedManager := NewSubscriptionManager()

	transport1 := newMockTransport()
	protocol1 := NewProtocol(transport1, sharedManager, nil)

	transport2 := newMockTransport()
	protocol2 := NewProtocol(transport2, sharedManager, nil)

	received := make(chan *Message, 2)
	handler := MessageRxHandlerFunc(func(msg *Message) {
		received <- msg
	})

	// Subscribe using first protocol
	protocol1.Subscribe("test/+", handler)

	// Send message through second protocol
	msg := &Message{
		Topic:   "test/shared",
		Payload: []byte("hello"),
	}

	if err := protocol2.SendMessage(msg); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	transport2.readCh <- msg

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	protocol2.StartReceiving(ctx)

	// Wait for message
	select {
	case receivedMsg := <-received:
		if receivedMsg.Topic != msg.Topic {
			t.Errorf("Topic mismatch: got %v, want %v", receivedMsg.Topic, msg.Topic)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for message")
	}
}

func TestProtocolGracefulClose(t *testing.T) {
	transport := newMockTransport()
	protocol := NewProtocol(transport, nil, nil)

	ctx := context.Background()
	protocol.StartReceiving(ctx)

	// Add a small delay to ensure goroutine is running
	time.Sleep(100 * time.Millisecond)

	// Close should complete without blocking
	done := make(chan struct{})
	go func() {
		err := protocol.Close()
		if err != nil {
			t.Errorf("Close() error = %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
		// Success - close completed
	case <-time.After(time.Second):
		t.Error("Close() timed out")
	}
}

func TestMultipleMessagesInSingleBuffer(t *testing.T) {
	// Create a buffer with two messages back-to-back
	msg1 := &Message{
		Topic:   "fan/intake/analog/current",
		Payload: []byte{0x31, 0x34, 0x30, 0x35},
	}
	msg2 := &Message{
		Topic:   "fan/intake/status",
		Payload: []byte{0x30},
	}

	// Encode both messages
	data1, err := msg1.Encode()
	if err != nil {
		t.Fatalf("Failed to encode first message: %v", err)
	}
	data2, err := msg2.Encode()
	if err != nil {
		t.Fatalf("Failed to encode second message: %v", err)
	}

	// Combine messages into a single buffer
	combinedData := append(data1, data2...)

	// Create a reader with the combined data
	reader := NewMessageReader(bytes.NewReader(combinedData))

	// Read first message
	received1, err := reader.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read first message: %v", err)
	}

	// Verify first message
	if received1.Topic != msg1.Topic {
		t.Errorf("First message topic mismatch: got %v, want %v", received1.Topic, msg1.Topic)
	}
	if !bytes.Equal(received1.Payload, msg1.Payload) {
		t.Errorf("First message payload mismatch: got %x, want %x", received1.Payload, msg1.Payload)
	}

	// Read second message
	received2, err := reader.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read second message: %v", err)
	}

	// Verify second message
	if received2.Topic != msg2.Topic {
		t.Errorf("Second message topic mismatch: got %v, want %v", received2.Topic, msg2.Topic)
	}
	if !bytes.Equal(received2.Payload, msg2.Payload) {
		t.Errorf("Second message payload mismatch: got %x, want %x", received2.Payload, msg2.Payload)
	}
}

func TestReadMessageWithRealDataCapture(t *testing.T) {
	// Real captured data containing two messages
	capturedData := []byte{
		0x01, 0x60, 0x19, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00,
		0x66, 0x61, 0x6e, 0x2f, 0x69, 0x6e, 0x74, 0x61, 0x6b, 0x65,
		0x2f, 0x61, 0x6e, 0x61, 0x6c, 0x6f, 0x67, 0x2f, 0x63, 0x75,
		0x72, 0x72, 0x65, 0x6e, 0x74, 0x31, 0x34, 0x30, 0x35, 0x33,
		0xf3, 0x09, 0x7a, 0x11, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x66, 0x61, 0x6e, 0x2f, 0x69, 0x6e, 0x74, 0x61, 0x6b,
		0x65, 0x2f, 0x73, 0x74, 0x01, 0x60, 0x61, 0x74, 0x75, 0x73,
		0x30, 0xbd, 0x0b, 0x11, 0x2f,
	}

	// Create a reader with a timeout to catch potential hangs
	reader := NewMessageReader(bytes.NewReader(capturedData))

	// Create a channel to catch timeout
	done := make(chan struct{})
	var messages []*Message
	var err error

	// Run the message reading in a goroutine
	go func() {
		defer close(done)

		// Try to read first message
		msg1, err1 := reader.ReadMessage()
		if err1 != nil {
			err = err1
			return
		}
		messages = append(messages, msg1)
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		if err != nil {
			t.Fatalf("Failed to read messages: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Test timed out - possible hang in ReadMessage")
	}

	// Verify we got exactly 1 messages; the second message has an invalid CRC
	if len(messages) != 1 {
		t.Fatalf("Expected 2 messages, got %d", len(messages))
	}

	// Verify first message
	expectedTopic1 := "fan/intake/analog/current"
	expectedPayload1 := []byte("1405")
	if messages[0].Topic != expectedTopic1 {
		t.Errorf("First message topic mismatch: got %v, want %v",
			messages[0].Topic, expectedTopic1)
	}
	if !bytes.Equal(messages[0].Payload, expectedPayload1) {
		t.Errorf("First message payload mismatch: got %x, want %x",
			messages[0].Payload, expectedPayload1)
	}
}
