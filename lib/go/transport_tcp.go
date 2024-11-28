package qdp

import (
	"fmt"
	"io"
	"net"
	"sync"
)

// TCPClientTransport implements ITransport for TCP client connections
type TCPClientTransport struct {
	conn net.Conn
	mu   sync.Mutex
}

// NewTCPClientTransport creates a new TCP client transport by dialing a server
func NewTCPClientTransport(address string) (*TCPClientTransport, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %w", err)
	}
	return &TCPClientTransport{conn: conn}, nil
}

// NewTCPClientTransportFromConn creates a new TCP client transport from existing connection
func NewTCPClientTransportFromConn(conn net.Conn) *TCPClientTransport {
	return &TCPClientTransport{conn: conn}
}

func (t *TCPClientTransport) Read(p []byte) (n int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn.Read(p)
}

func (t *TCPClientTransport) Write(p []byte) (n int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn.Write(p)
}

func (t *TCPClientTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn.Close()
}

// TCPServerTransport implements a TCP server that creates client transports
type TCPServerTransport struct {
	listener    net.Listener
	onNewClient func(ITransport)
	done        chan struct{}
	wg          sync.WaitGroup
}

// NewTCPServerTransport creates a new TCP server transport
func NewTCPServerTransport(address string, onNewClient func(ITransport)) (*TCPServerTransport, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	server := &TCPServerTransport{
		listener:    listener,
		onNewClient: onNewClient,
		done:        make(chan struct{}),
	}

	server.wg.Add(1)
	go server.acceptLoop()

	return server, nil
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

			transport := NewTCPClientTransportFromConn(conn)
			if t.onNewClient != nil {
				t.onNewClient(transport)
			}
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
