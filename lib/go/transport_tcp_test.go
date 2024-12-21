package qdp

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockTcpConnectionHandler struct {
	connectCount    atomic.Int32
	disconnectCount atomic.Int32
}

func (h *mockTcpConnectionHandler) OnConnect(transport ITransport) {
	h.connectCount.Add(1)
}

func (h *mockTcpConnectionHandler) OnDisconnect(transport ITransport, err error) {
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

func TestTCPClientTransportBufferedReading(t *testing.T) {
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

		// Send multiple messages in quick succession
		msgs := []*Message{
			{Topic: "test/1", Payload: []byte("Hello 1")},
			{Topic: "test/2", Payload: []byte("Hello 2")},
			{Topic: "test/3", Payload: []byte("Hello 3")},
		}

		for _, msg := range msgs {
			if err := writeMessage(conn, msg); err != nil {
				t.Errorf("Failed to write message: %v", err)
				return
			}
		}
	}()

	// Create client transport
	client, err := NewTCPClientTransport(listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Read all messages
	for i := 1; i <= 3; i++ {
		msg, err := client.ReadMessage()
		if err != nil {
			t.Fatalf("Failed to read message %d: %v", i, err)
		}

		expectedTopic := fmt.Sprintf("test/%d", i)
		expectedPayload := fmt.Sprintf("Hello %d", i)

		if msg.Topic != expectedTopic {
			t.Errorf("Message %d: wrong topic: got %s, want %s", i, msg.Topic, expectedTopic)
		}
		if string(msg.Payload) != expectedPayload {
			t.Errorf("Message %d: wrong payload: got %s, want %s", i, string(msg.Payload), expectedPayload)
		}
	}
}

func TestTCPServerTransport(t *testing.T) {
	serverHandler := &mockTcpConnectionHandler{}

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

	serverHandler := &mockTcpConnectionHandler{}

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
	handler := &mockTcpConnectionHandler{}

	// Start server
	server, err := NewTCPServerTransport("127.0.0.1:0", handler)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Create client with handler too
	clientHandler := &mockTcpConnectionHandler{}
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
	// Create server
	server, err := NewTCPServerTransport("127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	addr := server.listener.Addr().String()
	const numClients = 3

	// Create and store clients
	clients := make([]*TCPClientTransport, numClients)
	for i := 0; i < numClients; i++ {
		client, err := NewTCPClientTransport(addr, nil)
		if err != nil {
			t.Fatalf("Failed to create client %d: %v", i, err)
		}
		defer client.Close()
		clients[i] = client
	}

	// Give server time to register clients
	time.Sleep(100 * time.Millisecond)

	// Prepare test message
	testMsg := &Message{
		Topic:   "broadcast/test",
		Payload: []byte("broadcast test"),
	}

	// Start client reading goroutines
	var wg sync.WaitGroup
	wg.Add(numClients)

	for i, client := range clients {
		go func(id int, c *TCPClientTransport) {
			defer wg.Done()
			msg, err := c.ReadMessage()
			if err != nil {
				t.Errorf("Client %d failed to read: %v", id, err)
				return
			}
			if msg.Topic != testMsg.Topic {
				t.Errorf("Client %d topic mismatch: got %v, want %v", id, msg.Topic, testMsg.Topic)
			}
			if string(msg.Payload) != string(testMsg.Payload) {
				t.Errorf("Client %d payload mismatch: got %v, want %v", id, string(msg.Payload), string(testMsg.Payload))
			}
		}(i, client)
	}

	// Broadcast message
	if err := server.WriteMessage(testMsg); err != nil {
		t.Fatalf("Failed to broadcast message: %v", err)
	}

	// Wait for all clients with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for clients to receive message")
	}
}
