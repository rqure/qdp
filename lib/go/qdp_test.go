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
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		readBuf:  bytes.NewBuffer(nil),
		writeBuf: bytes.NewBuffer(nil),
	}
}

func (m *mockTransport) Read(p []byte) (n int, err error)  { return m.readBuf.Read(p) }
func (m *mockTransport) Write(p []byte) (n int, err error) { return m.writeBuf.Write(p) }
func (m *mockTransport) Close() error                      { return nil }

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
	protocol := NewProtocol(transport)

	// Test message
	msg := &Message{
		Topic:   "test",
		Payload: []byte("hello"),
	}

	// Send message
	if err := protocol.SendMessage(msg); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	// Copy written data to read buffer for simulation
	transport.readBuf.Write(transport.writeBuf.Bytes())

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
	protocol := NewProtocol(transport)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan *Message, 1)
	handler := MessageHandlerFunc(func(msg *Message) {
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

	transport.readBuf.Write(transport.writeBuf.Bytes())

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
