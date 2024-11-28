package qdp

import (
	"fmt"
	"io"
	"net"
	"sync"
)

// TCPClientTransport implements ITransport for TCP client connections
type TCPClientTransport struct {
	conn              net.Conn
	mu                sync.Mutex
	connectionHandler IConnectionHandler
}

// NewTCPClientTransport creates a new TCP client transport by dialing a server
func NewTCPClientTransport(address string, connectionHandler IConnectionHandler) (*TCPClientTransport, error) {
	t := &TCPClientTransport{
		connectionHandler: connectionHandler,
	}
	if err := t.connect(address); err != nil {
		return nil, err
	}
	return t, nil
}

// NewTCPClientTransportFromConn creates a new TCP client transport from existing connection
func NewTCPClientTransportFromConn(conn net.Conn, connectionHandler IConnectionHandler) *TCPClientTransport {
	return &TCPClientTransport{conn: conn, connectionHandler: connectionHandler}
}

func (t *TCPClientTransport) connect(address string) error {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}

	t.mu.Lock()
	t.conn = conn
	handler := t.connectionHandler
	t.mu.Unlock()

	if handler != nil {
		handler.OnConnect(t)
	}
	return nil
}

func (t *TCPClientTransport) SetConnectionHandler(handler IConnectionHandler) {
	t.mu.Lock()
	t.connectionHandler = handler
	t.mu.Unlock()
}

func (t *TCPClientTransport) Read(p []byte) (n int, err error) {
	t.mu.Lock()
	conn := t.conn
	handler := t.connectionHandler
	t.mu.Unlock()

	n, err = conn.Read(p)
	if err != nil && !isClosedError(err) && handler != nil {
		handler.OnDisconnect(t, err)
	}
	return n, err
}

func (t *TCPClientTransport) Write(p []byte) (n int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn.Write(p)
}

func (t *TCPClientTransport) Close() error {
	t.mu.Lock()
	conn := t.conn
	handler := t.connectionHandler
	t.mu.Unlock()

	if conn != nil {
		err := conn.Close()
		if handler != nil {
			handler.OnDisconnect(t, err)
		}
		return err
	}
	return nil
}

// TCPServerTransport implements a TCP server that creates client transports
type TCPServerTransport struct {
	listener          net.Listener
	connectionHandler IConnectionHandler
	done              chan struct{}
	wg                sync.WaitGroup
}

// NewTCPServerTransport creates a new TCP server transport
func NewTCPServerTransport(address string, clientConnectionHandler IConnectionHandler) (*TCPServerTransport, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	server := &TCPServerTransport{
		listener:          listener,
		done:              make(chan struct{}),
		connectionHandler: clientConnectionHandler,
	}

	server.wg.Add(1)
	go server.acceptLoop()

	return server, nil
}

func (t *TCPServerTransport) SetConnectionHandler(handler IConnectionHandler) {
	t.connectionHandler = handler
}

func (t *TCPServerTransport) acceptLoop() {
	defer t.wg.Done()

	for {
		select {
		case <-t.done:
			return
		default:
			conn, err := t.listener.Accept()
			if err != nil {
				if !isClosedError(err) {
					// Log error if needed
				}
				continue
			}

			transport := NewTCPClientTransportFromConn(conn, t.connectionHandler)
			t.connectionHandler.OnConnect(transport)
		}
	}
}

func (t *TCPServerTransport) Close() error {
	close(t.done)
	err := t.listener.Close()
	t.wg.Wait()
	return err
}

// isClosedError checks if the error is due to closed connection/listener
func isClosedError(err error) bool {
	return err == io.EOF || err == net.ErrClosed
}
