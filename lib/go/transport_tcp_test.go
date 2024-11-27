package qdp

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

func TestTCPTransportBasic(t *testing.T) {
	// Start server
	server := NewTCPServerTransport(":12345")
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Connect client
	client, err := NewTCPClientTransport("localhost:12345")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Test sending data
	testData := []byte("Hello, World!")
	txBuf := Buffer{
		data:     testData,
		size:     len(testData),
		capacity: len(testData),
	}

	if err := client.Send(&txBuf, nil); err != nil {
		t.Fatalf("Failed to send data: %v", err)
	}
}

func TestTCPTransportMultipleClients(t *testing.T) {
	server := NewTCPServerTransport(":12346")
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	var wg sync.WaitGroup
	clientCount := 5

	for i := 0; i < clientCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			client, err := NewTCPClientTransport("localhost:12346")
			if err != nil {
				t.Errorf("Client %d failed to connect: %v", id, err)
				return
			}
			defer client.Close()

			// Send unique message from this client
			msg := fmt.Sprintf("Message from client %d", id)
			txBuf := Buffer{
				data:     []byte(msg),
				size:     len(msg),
				capacity: len(msg),
			}

			if err := client.Send(&txBuf, nil); err != nil {
				t.Errorf("Client %d failed to send: %v", id, err)
			}
		}(i)
	}

	wg.Wait()
}

func TestTCPTransportLargeMessage(t *testing.T) {
	server := NewTCPServerTransport(":12347")
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	client, err := NewTCPClientTransport("localhost:12347")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Create large message
	largeData := make([]byte, MaxPayloadSize)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	txBuf := Buffer{
		data:     largeData,
		size:     len(largeData),
		capacity: len(largeData),
	}

	if err := client.Send(&txBuf, nil); err != nil {
		t.Fatalf("Failed to send large data: %v", err)
	}
}

func TestTCPTransportConnectionClose(t *testing.T) {
	// Set up a test network that we can control
	listener, err := net.Listen("tcp", ":0") // Let system pick port
	if err != nil {
		t.Fatalf("Failed to create test listener: %v", err)
	}
	addr := listener.Addr().String()

	server := &TCPServerTransport{
		addr:   addr,
		ctx:    context.Background(),
		cancel: func() {}, // No-op cancel for test
	}
	server.listener = listener

	// Start server accept loop
	go server.acceptLoop()

	// Connect client
	client, err := NewTCPClientTransport(addr)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Test initial connection works
	txBuf := Buffer{
		data:     []byte("Test message"),
		size:     11,
		capacity: 11,
	}
	if err := client.Send(&txBuf, nil); err != nil {
		t.Fatalf("Initial send failed: %v", err)
	}

	// Close server
	server.Stop()
	listener.Close()

	// Try to send after server is closed - should fail
	time.Sleep(10 * time.Millisecond) // Brief pause for closure to propagate

	txBuf.data = []byte("Should fail")
	txBuf.size = 10
	err = client.Send(&txBuf, nil)
	if err == nil {
		t.Error("Expected send to fail after server close")
	}

	client.Close()
}

func TestTCPTransportFullProtocol(t *testing.T) {
	server := NewTCPServerTransport(":12349")
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	client, err := NewTCPClientTransport("localhost:12349")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Create QDP instance with TCP transport
	qdp := New()
	qdp.SetTransport(
		func(buf *Buffer, ctx interface{}) error { return client.Send(buf, ctx) },
		func(buf *Buffer, ctx interface{}) error { return client.Receive(buf, ctx) },
		nil,
	)

	// Test publish/subscribe
	var received bool
	var wg sync.WaitGroup
	wg.Add(1)

	callback := func(msg *Message, _ interface{}) {
		received = true
		wg.Done()
	}

	qdp.Subscribe("test/topic", callback, nil)
	if err := qdp.PublishString("test/topic", "Test message"); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Process messages
	if err := qdp.Process(); err != nil {
		t.Fatalf("Failed to process messages: %v", err)
	}

	// Wait for callback with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if !received {
			t.Error("Message not received")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for message")
	}
}
