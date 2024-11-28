package qdp

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"
)

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
	client, err := NewTCPClientTransport(listener.Addr().String())
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

	// Create server
	server, err := NewTCPServerTransport("127.0.0.1:0", func(transport ITransport) {
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
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Get server address
	addr := server.listener.Addr().String()

	// Create client and send data
	client, err := NewTCPClientTransport(addr)
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

	// Create server
	server, err := NewTCPServerTransport("127.0.0.1:0", func(transport ITransport) {
		clientsConnected.Done()
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Create multiple clients
	addr := server.listener.Addr().String()
	for i := 0; i < numClients; i++ {
		go func(id int) {
			client, err := NewTCPClientTransport(addr)
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
