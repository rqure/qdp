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
