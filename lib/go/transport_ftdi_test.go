package qdp

import (
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
