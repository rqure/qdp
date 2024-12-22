package qdp

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestFTDITransportCreation(t *testing.T) {
	// Skip test if running in CI or without hardware
	t.Skip("Skipping FTDI test - requires physical hardware")

	handler := &mockFtdiConnectionHandler{}
	config := DefaultFTDIConfig

	transport, err := NewFTDITransport(config, handler)
	if err != nil {
		t.Fatalf("Failed to create FTDI transport: %v", err)
	}
	defer transport.Close()

	// Verify connection handler was called
	time.Sleep(100 * time.Millisecond)
	if got := handler.connectCount.Load(); got != 1 {
		t.Errorf("Expected 1 connect callback, got %d", got)
	}
}

type mockFtdiConnectionHandler struct {
	connectCount    atomic.Int32
	disconnectCount atomic.Int32
}

func (h *mockFtdiConnectionHandler) OnConnect(transport ITransport) {
	h.connectCount.Add(1)
}

func (h *mockFtdiConnectionHandler) OnDisconnect(transport ITransport, err error) {
	h.disconnectCount.Add(1)
}

func TestFTDITransportReadWrite(t *testing.T) {
	// Skip test if running in CI or without hardware
	t.Skip("Skipping FTDI test - requires physical hardware")

	config := DefaultFTDIConfig
	transport, err := NewFTDITransport(config, nil)
	if err != nil {
		t.Fatalf("Failed to create FTDI transport: %v", err)
	}
	defer transport.Close()

	// Test message
	msg := &Message{
		Topic:   "test/ftdi",
		Payload: []byte("Hello FTDI!"),
	}

	// Write test
	if err := transport.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}

	// Read test (requires loopback)
	readMsg, err := transport.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	if readMsg.Topic != msg.Topic {
		t.Errorf("Topic mismatch: got %v, want %v", readMsg.Topic, msg.Topic)
	}
	if string(readMsg.Payload) != string(msg.Payload) {
		t.Errorf("Payload mismatch: got %v, want %v", string(readMsg.Payload), string(msg.Payload))
	}
}

type mockFTDIReader struct {
	data []byte
	pos  int
}

func (m *mockFTDIReader) ReadContext(ctx context.Context, p []byte) (int, error) {
	if m.pos >= len(m.data) {
		return 0, io.EOF
	}
	n := copy(p, m.data[m.pos:])
	m.pos += n
	return n, nil
}

func TestFTDIStatusBytesHandling(t *testing.T) {
	// Create test data with FTDI status bytes (0x01, 0x60) between messages
	testData := []byte{
		// First message with status bytes
		0x01, 0x60, // FTDI status bytes
		0x0e, 0x00, 0x00, 0x00, // Topic length (14)
		0x04, 0x00, 0x00, 0x00, // Payload length (4)
		0x66, 0x61, 0x72, 0x6d, 0x2f, 0x68, 0x65, 0x61, 0x72, 0x74, 0x62, 0x65, 0x61, 0x74, // Topic: "farm/heartbeat"
		0x33, 0x31, 0x32, 0x35, // Payload: "3125"
		0x7d, 0xd2, 0xae, 0x2b, // CRC32

		// Second message with status bytes
		0x19, 0x00, 0x00, 0x00, // Topic length (25)
		0x04, 0x00, 0x00, 0x00, // Payload length (4)
		0x66, 0x61, 0x6e, 0x2f, 0x69, 0x6e, 0x74, 0x61, 0x6b, 0x65, 0x2f, 0x61, 0x6e, 0x61,
		0x6c, 0x6f, 0x67, 0x2f, 0x63, 0x75, 0x72, 0x72, 0x65, 0x6e, // Topic: "fan/intake/analog/curren"
		0x01, 0x60, // FTDI status bytes
		0x74,                   // Topic continued: "t"
		0x31, 0x33, 0x33, 0x35, // Payload: "1335"
		0x75, 0xb6, 0x6b, 0x54, // CRC32

		// Third message with status bytes
		0x11, 0x00, 0x00, 0x00, // Topic length (17)
		0x01, 0x00, 0x00, 0x00, // Payload length (1)
		0x66, 0x61, 0x6e, 0x2f, 0x69, 0x6e, 0x74, 0x61, 0x6b, 0x65, 0x2f, 0x73, 0x74, 0x61,
		0x74, 0x75, 0x73, // Topic: "fan/intake/status"
		0x30,                   // Payload: "0"
		0xbd, 0x0b, 0x11, 0x2f, // CRC32
	}

	// Create mock reader with test data
	mock := &mockFTDIReader{data: testData}
	reader := NewMessageReader(&endpointReader{
		ep:  mock,
		ctx: context.Background(),
	})

	// Expected messages
	expectedMessages := []struct {
		topic   string
		payload string
	}{
		{
			topic:   "farm/heartbeat",
			payload: "3125",
		},
		{
			topic:   "fan/intake/analog/current",
			payload: "1335",
		},
		{
			topic:   "fan/intake/status",
			payload: "0",
		},
	}

	// Read and verify each message
	for i, expected := range expectedMessages {
		msg, err := reader.ReadMessage()
		if err != nil {
			t.Fatalf("Failed to read message %d: %v", i+1, err)
		}

		if msg.Topic != expected.topic {
			t.Errorf("Message %d: wrong topic: got %q, want %q",
				i+1, msg.Topic, expected.topic)
		}

		if string(msg.Payload) != expected.payload {
			t.Errorf("Message %d: wrong payload: got %q, want %q",
				i+1, string(msg.Payload), expected.payload)
		}
	}

	// Verify no more messages
	_, err := reader.ReadMessage()
	if err == nil {
		t.Error("Expected error reading past end of data, got nil")
	}
}
