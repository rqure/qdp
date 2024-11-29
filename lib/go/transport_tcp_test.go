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
	sentCount       atomic.Int32
	receivedCount   atomic.Int32
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

func (h *mockConnectionHandler) OnMessageSent(transport ITransport, msg *Message) {
	h.sentCount.Add(1)
}

func (h *mockConnectionHandler) OnMessageReceived(transport ITransport, msg *Message) {
	h.receivedCount.Add(1)
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
	serverHandler := &mockConnectionHandler{}

	// Create server
	server, err := NewTCPServerTransport("127.0.0.1:0", serverHandler)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Verify server got connect callback
	time.Sleep(100 * time.Millisecond)
	if got := serverHandler.connectCount.Load(); got != 1 {
		t.Errorf("Expected 1 server connect callback, got %d", got)
	}

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

	// Read from server transport
	readBuf := make([]byte, len(testData))
	n, err := server.Read(readBuf)
	if err != nil {
		t.Fatalf("Server read failed: %v", err)
	}
	if !bytes.Equal(readBuf[:n], testData) {
		t.Errorf("Data mismatch: got %v, want %v", readBuf[:n], testData)
	}
}

func TestMultipleClientConnections(t *testing.T) {
	const numClients = 3
	var clientsConnected sync.WaitGroup
	clientsConnected.Add(numClients)

	serverHandler := &mockConnectionHandler{}

	// Create server
	server, err := NewTCPServerTransport("127.0.0.1:0", serverHandler)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Create multiple clients and wait for connects
	addr := server.listener.Addr().String()
	clients := make([]*TCPClientTransport, numClients)
	for i := 0; i < numClients; i++ {
		go func(id int) {
			client, err := NewTCPClientTransport(addr, nil)
			if err != nil {
				t.Errorf("Client %d failed to connect: %v", id, err)
				clientsConnected.Done()
				return
			}
			clients[id] = client
			clientsConnected.Done()
		}(i)
	}

	// Wait for all clients to connect
	clientsConnected.Wait()

	// Cleanup
	for _, client := range clients {
		if client != nil {
			client.Close()
		}
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

	// Create client with handler too
	clientHandler := &mockConnectionHandler{}
	client, err := NewTCPClientTransport(server.listener.Addr().String(), clientHandler)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Wait for connect callback
	time.Sleep(100 * time.Millisecond)

	// Close client and wait for disconnect callbacks
	client.Close()

	time.Sleep(100 * time.Millisecond)
	if got := clientHandler.disconnectCount.Load(); got != 1 {
		t.Errorf("Expected 1 client disconnect callback, got %d", got)
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

	// Create client with same handler
	client, err := NewTCPClientTransport(server.listener.Addr().String(), handler)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Wait for connect callback
	time.Sleep(100 * time.Millisecond)

	// Close client and wait for disconnect callback
	client.Close()

	time.Sleep(100 * time.Millisecond)
	if got := disconnectCount.Load(); got != 1 {
		t.Errorf("Expected 1 disconnect callback, got %d", got)
	}
}

func TestTCPServerTransportAsTransport(t *testing.T) {
	handler := &mockConnectionHandler{}

	// Create server
	server, err := NewTCPServerTransport("127.0.0.1:0", handler)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Connect some clients
	addr := server.listener.Addr().String()
	testData := []byte("broadcast test")

	// Create and connect clients
	for i := 0; i < 3; i++ {
		client, err := NewTCPClientTransport(addr, nil)
		if err != nil {
			t.Fatalf("Client %d failed to connect: %v", i, err)
		}
		defer client.Close()
	}

	// Wait for clients to connect
	time.Sleep(100 * time.Millisecond)

	// Test broadcast write
	n, err := server.Write(testData)
	if err != nil {
		t.Fatalf("Server write failed: %v", err)
	}
	if n != len(testData) {
		t.Errorf("Write length mismatch: got %v, want %v", n, len(testData))
	}

	// Verify server implements ITransport
	var _ ITransport = server
}
