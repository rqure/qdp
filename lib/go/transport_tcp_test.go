package qdp

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockConnectionHandler struct {
	connectCount    atomic.Int32
	disconnectCount atomic.Int32
}

func (h *mockConnectionHandler) OnConnect(transport ITransport) {
	h.connectCount.Add(1)
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

		// Read message and echo back
		msg, err := readMessage(conn)
		if err != nil {
			return
		}
		writeMessage(conn, msg)
	}()

	// Create client transport
	client, err := NewTCPClientTransport(listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Test message
	testMsg := &Message{
		Topic:   "test/topic",
		Payload: []byte("Hello, TCP!"),
	}

	// Write test
	err = client.WriteMessage(testMsg)
	if err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}

	// Read test
	readMsg, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	// Compare messages
	if readMsg.Topic != testMsg.Topic {
		t.Errorf("Topic mismatch: got %v, want %v", readMsg.Topic, testMsg.Topic)
	}
	if string(readMsg.Payload) != string(testMsg.Payload) {
		t.Errorf("Payload mismatch: got %v, want %v", string(readMsg.Payload), string(testMsg.Payload))
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

	// Create client and send message
	client, err := NewTCPClientTransport(addr, nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	testMsg := &Message{
		Topic:   "test/topic",
		Payload: []byte("Hello, Server!"),
	}

	if err := client.WriteMessage(testMsg); err != nil {
		t.Fatalf("Failed to write message to server: %v", err)
	}

	// Read from server transport
	readMsg, err := server.ReadMessage()
	if err != nil {
		t.Fatalf("Server ReadMessage failed: %v", err)
	}

	if readMsg.Topic != testMsg.Topic {
		t.Errorf("Topic mismatch: got %v, want %v", readMsg.Topic, testMsg.Topic)
	}
	if string(readMsg.Payload) != string(testMsg.Payload) {
		t.Errorf("Payload mismatch: got %v, want %v", string(readMsg.Payload), string(testMsg.Payload))
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
			// Only count disconnects from client transports
			if _, isServer := transport.(*TCPServerTransport); !isServer {
				disconnectCount.Add(1)
			}
		},
	}

	// Start server with handler
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

	// Wait for connect callbacks
	time.Sleep(100 * time.Millisecond)
	if got := connectCount.Load(); got != 2 {
		t.Errorf("Expected 2 connect callbacks, got %d", got)
	}

	// Close client and wait for disconnect callbacks
	client.Close()

	time.Sleep(100 * time.Millisecond)
	if got := disconnectCount.Load(); got != 1 {
		t.Errorf("Expected 1 disconnect callback, got %d", got)
	}
}

func TestTCPServerTransportBroadcast(t *testing.T) {
	handler := &mockConnectionHandler{}

	// Create server
	server, err := NewTCPServerTransport("127.0.0.1:0", handler)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	addr := server.listener.Addr().String()
	testMsg := &Message{
		Topic:   "broadcast/test",
		Payload: []byte("broadcast test"),
	}

	// Create multiple clients with synchronized setup
	const numClients = 3
	var connectWg sync.WaitGroup
	var readWg sync.WaitGroup
	var clients []*TCPClientTransport
	var clientErrors []error
	var mu sync.Mutex // protect clients and errors

	// Setup connection synchronization
	connectWg.Add(numClients)
	readWg.Add(numClients)

	// Setup all clients first
	for i := 0; i < numClients; i++ {
		go func(id int) {
			client, err := NewTCPClientTransport(addr, nil)
			if err != nil {
				mu.Lock()
				clientErrors = append(clientErrors, fmt.Errorf("client %d connect error: %v", id, err))
				mu.Unlock()
				connectWg.Done()
				readWg.Done()
				return
			}

			mu.Lock()
			clients = append(clients, client)
			mu.Unlock()
			connectWg.Done()

			// Start reading in blocking mode
			msg, err := client.ReadMessage()
			if err != nil {
				mu.Lock()
				clientErrors = append(clientErrors, fmt.Errorf("client %d read error: %v", id, err))
				mu.Unlock()
				readWg.Done()
				return
			}

			if msg.Topic != testMsg.Topic {
				mu.Lock()
				clientErrors = append(clientErrors, fmt.Errorf("client %d topic mismatch: got %v, want %v",
					id, msg.Topic, testMsg.Topic))
				mu.Unlock()
			}
			if string(msg.Payload) != string(testMsg.Payload) {
				mu.Lock()
				clientErrors = append(clientErrors, fmt.Errorf("client %d payload mismatch: got %v, want %v",
					id, string(msg.Payload), string(testMsg.Payload)))
				mu.Unlock()
			}
			readWg.Done()
		}(i)
	}

	// Wait for all clients to connect with timeout
	if !waitWithTimeout(&connectWg, 5*time.Second) {
		t.Fatal("Timeout waiting for clients to connect")
	}

	// Check for connection errors
	mu.Lock()
	if len(clientErrors) > 0 {
		for _, err := range clientErrors {
			t.Error(err)
		}
		mu.Unlock()
		t.FailNow()
	}
	mu.Unlock()

	// Ensure server has registered all clients
	time.Sleep(100 * time.Millisecond)

	// Broadcast message
	if err := server.WriteMessage(testMsg); err != nil {
		t.Fatalf("Server WriteMessage failed: %v", err)
	}

	// Wait for all reads with timeout
	if !waitWithTimeout(&readWg, 5*time.Second) {
		t.Fatal("Timeout waiting for clients to receive message")
	}

	// Check for read errors
	mu.Lock()
	if len(clientErrors) > 0 {
		for _, err := range clientErrors {
			t.Error(err)
		}
		mu.Unlock()
		t.FailNow()
	}
	mu.Unlock()

	// Cleanup
	for _, client := range clients {
		client.Close()
	}
}

// Helper function for WaitGroup timeout
func waitWithTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
