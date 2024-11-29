package qdp

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
)

// TCPClientTransport implements ITransport for TCP client connections
type TCPClientTransport struct {
	conn              net.Conn
	connectionHandler IConnectionHandler
	ctx               context.Context
	cancel            context.CancelFunc
	disconnected      atomic.Bool // Add atomic flag for disconnect state
}

// NewTCPClientTransport creates a new TCP client transport by dialing a server
func NewTCPClientTransport(address string, connectionHandler IConnectionHandler) (*TCPClientTransport, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t := &TCPClientTransport{
		conn:              conn,
		connectionHandler: connectionHandler,
		ctx:               ctx,
		cancel:            cancel,
	}

	if connectionHandler != nil {
		connectionHandler.OnConnect(t)
	}

	return t, nil
}

// NewTCPClientTransportFromConn creates a new TCP client transport from existing connection
func NewTCPClientTransportFromConn(conn net.Conn, connectionHandler IConnectionHandler) *TCPClientTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &TCPClientTransport{
		conn:              conn,
		connectionHandler: connectionHandler,
		ctx:               ctx,
		cancel:            cancel,
	}
}

func (t *TCPClientTransport) SetConnectionHandler(handler IConnectionHandler) {
	t.connectionHandler = handler
}

func (t *TCPClientTransport) Read(p []byte) (n int, err error) {
	select {
	case <-t.ctx.Done():
		return 0, io.EOF
	default:
		n, err = t.conn.Read(p)
		if err != nil && t.connectionHandler != nil && t.disconnected.CompareAndSwap(false, true) {
			t.connectionHandler.OnDisconnect(t, err)
		}
		return n, err
	}
}

func (t *TCPClientTransport) Write(p []byte) (n int, err error) {
	select {
	case <-t.ctx.Done():
		return 0, io.EOF
	default:
		return t.conn.Write(p)
	}
}

func (t *TCPClientTransport) Close() error {
	t.cancel() // Cancel context first
	err := t.conn.Close()
	// Only call OnDisconnect if we haven't already disconnected
	if t.connectionHandler != nil && t.disconnected.CompareAndSwap(false, true) {
		t.connectionHandler.OnDisconnect(t, err)
	}
	return err
}

// TCPServerTransport implements both a TCP server and ITransport
type TCPServerTransport struct {
	listener          net.Listener
	connectionHandler IConnectionHandler // Server's connection handler
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	clients           sync.Map
	readChan          chan []byte
	disconnected      atomic.Bool // Add atomic flag for server disconnect state
}

// NewTCPServerTransport creates a new TCP server transport
func NewTCPServerTransport(address string, connectionHandler IConnectionHandler) (*TCPServerTransport, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	server := &TCPServerTransport{
		listener:          listener,
		connectionHandler: connectionHandler,
		ctx:               ctx,
		cancel:            cancel,
		readChan:          make(chan []byte, 100), // Buffered channel for reads
	}

	server.wg.Add(1)
	go server.acceptLoop()

	if connectionHandler != nil {
		connectionHandler.OnConnect(server)
	}

	return server, nil
}

func (t *TCPServerTransport) acceptLoop() {
	defer t.wg.Done()
	defer func() {
		// Only call OnDisconnect if we haven't already disconnected
		if t.connectionHandler != nil && t.disconnected.CompareAndSwap(false, true) {
			t.connectionHandler.OnDisconnect(t, nil)
		}
	}()

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
			conn, err := t.listener.Accept()
			if err != nil {
				return
			}

			// Create client with NO connection handler - server will handle notifications
			client := NewTCPClientTransportFromConn(conn, nil)

			t.clients.Store(client, struct{}{})

			t.wg.Add(1)
			go t.handleClientReads(client)
		}
	}
}

// handleClientReads continuously reads from a client and pushes data to readChan
func (t *TCPServerTransport) handleClientReads(client *TCPClientTransport) {
	defer t.wg.Done()
	buf := make([]byte, 4096)

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
			n, err := client.Read(buf)
			if err != nil {
				t.clients.Delete(client)
				return
			}
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				select {
				case t.readChan <- data:
				case <-t.ctx.Done():
					return
				}
			}
		}
	}
}

// Read implements ITransport by reading from the readChan
func (t *TCPServerTransport) Read(p []byte) (n int, err error) {
	select {
	case <-t.ctx.Done():
		return 0, io.EOF
	case data := <-t.readChan:
		n = copy(p, data)
		if t.connectionHandler != nil {
			msg := &Message{Payload: data} // Create basic message for callback
			t.connectionHandler.OnMessageReceived(t, msg)
		}
		return n, nil
	}
}

// Write implements ITransport by broadcasting write to all clients
func (t *TCPServerTransport) Write(p []byte) (n int, err error) {
	var lastErr error
	t.clients.Range(func(key, _ interface{}) bool {
		client := key.(*TCPClientTransport)
		n, err = client.Write(p)
		if err != nil {
			lastErr = err
			return true // continue iteration
		}
		if t.connectionHandler != nil {
			msg := &Message{Payload: p} // Create basic message for callback
			t.connectionHandler.OnMessageSent(t, msg)
		}
		return false // stop iteration on success
	})
	if lastErr != nil {
		return 0, lastErr
	}
	return len(p), nil
}

// Close implements ITransport
func (t *TCPServerTransport) Close() error {
	t.cancel() // Cancel context first

	// Close all clients
	t.clients.Range(func(key, _ interface{}) bool {
		client := key.(*TCPClientTransport)
		client.Close() // This will trigger the client's disconnect callback
		return true
	})

	err := t.listener.Close()
	// Only call OnDisconnect if we haven't already disconnected
	if t.connectionHandler != nil && t.disconnected.CompareAndSwap(false, true) {
		t.connectionHandler.OnDisconnect(t, err)
	}
	t.wg.Wait()
	return err
}
