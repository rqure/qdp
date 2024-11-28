package qdp

import (
	"bytes"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockConnectionHandler struct {
	connectCount    atomic.Int32
	disconnectCount atomic.Int32
	handleClient    func(ITransport)
}

func (h *mockConnectionHandler) OnConnect(transport ITransport) {
	h.connectCount.Add(1)
	if h.handleClient != nil {
		h.handleClient(transport)
	}
}

func (h *mockConnectionHandler) OnDisconnect(transport ITransport, err error) {
	h.disconnectCount.Add(1)
}

func TestTCPClientTransport(t *testing.T) {
	// Start test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}
	defer listener.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		conn.Write(buf[:n]) // Echo back
	}()

	// Create client transport
	client, err := NewTCPClientTransport(listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Test data
	testData := []byte("Hello, TCP!")

	// Write test
	n, err := client.Write(testData)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(testData) {
		t.Errorf("Write length mismatch: got %v, want %v", n, len(testData))
	}

	// Read test
	readBuf := make([]byte, len(testData))
	n, err = client.Read(readBuf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if n != len(testData) {
		t.Errorf("Read length mismatch: got %v, want %v", n, len(testData))
	}
	if !bytes.Equal(readBuf, testData) {
		t.Errorf("Data mismatch: got %v, want %v", readBuf, testData)
	}
}

func TestTCPServerTransport(t *testing.T) {
	var wg sync.WaitGroup
	receivedData := make(chan []byte, 1)

	handler := &mockConnectionHandler{}
	handler.handleClient = func(transport ITransport) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 1024)
			n, err := transport.Read(buf)
			if err != nil {
				return
			}
			receivedData <- buf[:n]
			transport.Write(buf[:n]) // Echo back
		}()
	}

	// Create server
	server, err := NewTCPServerTransport("127.0.0.1:0", handler)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Get server address
	addr := server.listener.Addr().String()

	// Create client and send data
	client, err := NewTCPClientTransport(addr, nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	testData := []byte("Hello, Server!")
	if _, err := client.Write(testData); err != nil {
		t.Fatalf("Failed to write to server: %v", err)
	}

	// Wait for data to be received
	select {
	case data := <-receivedData:
		if !bytes.Equal(data, testData) {
			t.Errorf("Data mismatch: got %v, want %v", data, testData)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for data")
	}

	// Test echo
	readBuf := make([]byte, len(testData))
	n, err := client.Read(readBuf)
	if err != nil {
		t.Fatalf("Failed to read echo: %v", err)
	}
	if !bytes.Equal(readBuf[:n], testData) {
		t.Errorf("Echo data mismatch: got %v, want %v", readBuf[:n], testData)
	}

	wg.Wait()
}

func TestMultipleClientConnections(t *testing.T) {
	const numClients = 3
	var clientsConnected sync.WaitGroup
	clientsConnected.Add(numClients)

	handler := &mockConnectionHandler{
		handleClient: func(transport ITransport) {
			clientsConnected.Done()
		},
	}

	// Create server
	server, err := NewTCPServerTransport("127.0.0.1:0", handler)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Create multiple clients
	addr := server.listener.Addr().String()
	for i := 0; i < numClients; i++ {
		go func(id int) {
			client, err := NewTCPClientTransport(addr, nil)
			if err != nil {
				t.Errorf("Client %d failed to connect: %v", id, err)
				return
			}
			defer client.Close()
		}(i)
	}

	// Wait for all clients to connect with timeout
	done := make(chan struct{})
	go func() {
		clientsConnected.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for clients to connect")
	}
}

func TestConnectionCallbacks(t *testing.T) {
	handler := &mockConnectionHandler{}

	// Start server
	server, err := NewTCPServerTransport("127.0.0.1:0", handler)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Create client
	client, err := NewTCPClientTransport(server.listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Wait for connect callback
	time.Sleep(100 * time.Millisecond)
	if got := handler.connectCount.Load(); got != 1 {
		t.Errorf("Expected 1 connect callback, got %d", got)
	}

	// Close client and wait for disconnect
	client.Close()
	time.Sleep(100 * time.Millisecond)
	if got := handler.disconnectCount.Load(); got != 1 {
		t.Errorf("Expected 1 disconnect callback, got %d", got)
	}
}

func TestConnectionHandlerFunc(t *testing.T) {
	var connectCount, disconnectCount atomic.Int32

	handler := ConnectionHandlerFunc{
		OnConnectFunc: func(transport ITransport) {
			connectCount.Add(1)
		},
		OnDisconnectFunc: func(transport ITransport, err error) {
			disconnectCount.Add(1)
		},
	}

	// Start server
	server, err := NewTCPServerTransport("127.0.0.1:0", handler)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Create client
	client, err := NewTCPClientTransport(server.listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Wait for connect callback
	time.Sleep(100 * time.Millisecond)
	if got := connectCount.Load(); got != 1 {
		t.Errorf("Expected 1 connect callback, got %d", got)
	}

	// Close client and wait for disconnect
	client.Close()
	time.Sleep(100 * time.Millisecond)
	if got := disconnectCount.Load(); got != 1 {
		t.Errorf("Expected 1 disconnect callback, got %d", got)
	}
}

// ...existing code...
