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

// TCPServerTransport implements both a TCP server and ITransport
type TCPServerTransport struct {
	listener          net.Listener
	connectionHandler IConnectionHandler // Server's connection handler
	done              chan struct{}
	wg                sync.WaitGroup
	mu                sync.RWMutex
	clients           []*TCPClientTransport
	readChan          chan []byte
}

// NewTCPServerTransport creates a new TCP server transport
func NewTCPServerTransport(address string, connectionHandler IConnectionHandler) (*TCPServerTransport, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	server := &TCPServerTransport{
		listener:          listener,
		connectionHandler: connectionHandler,
		done:              make(chan struct{}),
		clients:           make([]*TCPClientTransport, 0),
		readChan:          make(chan []byte, 100), // Buffered channel for reads
	}

	server.wg.Add(1)
	go server.acceptLoop()

	if connectionHandler != nil {
		connectionHandler.OnConnect(server)
	}

	return server, nil
}

func (t *TCPServerTransport) SetConnectionHandler(handler IConnectionHandler) {
	t.mu.Lock()
	t.connectionHandler = handler
	t.mu.Unlock()
}

func (t *TCPServerTransport) acceptLoop() {
	defer t.wg.Done()
	defer func() {
		if t.connectionHandler != nil {
			t.connectionHandler.OnDisconnect(t, nil)
		}
	}()

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

			client := NewTCPClientTransportFromConn(conn, nil)
			t.mu.Lock()
			t.clients = append(t.clients, client)
			t.mu.Unlock()

			// Start goroutine to handle client reads
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
		case <-t.done:
			return
		default:
			n, err := client.Read(buf)
			if err != nil {
				if !isClosedError(err) {
					// Remove client from list on error
					t.mu.Lock()
					for i, c := range t.clients {
						if c == client {
							t.clients = append(t.clients[:i], t.clients[i+1:]...)
							break
						}
					}
					t.mu.Unlock()
				}
				return
			}
			if n > 0 {
				// Copy the data since buf will be reused
				data := make([]byte, n)
				copy(data, buf[:n])
				t.readChan <- data
			}
		}
	}
}

// Read implements ITransport by reading from the readChan
func (t *TCPServerTransport) Read(p []byte) (n int, err error) {
	select {
	case <-t.done:
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
	t.mu.RLock()
	defer t.mu.RUnlock()

	var lastErr error
	for _, client := range t.clients {
		n, err := client.Write(p)
		if err != nil {
			lastErr = err
		} else {
			if t.connectionHandler != nil {
				msg := &Message{Payload: p} // Create basic message for callback
				t.connectionHandler.OnMessageSent(t, msg)
			}
			return n, nil
		}
	}
	if lastErr != nil {
		return 0, lastErr
	}
	return len(p), nil
}

// Close implements ITransport
func (t *TCPServerTransport) Close() error {
	close(t.done)

	t.mu.Lock()
	for _, client := range t.clients {
		client.Close()
	}
	t.clients = nil
	t.mu.Unlock()

	err := t.listener.Close()
	if t.connectionHandler != nil {
		t.connectionHandler.OnDisconnect(t, err)
	}
	t.wg.Wait()
	return err
}

// isClosedError checks if the error is due to closed connection/listener
func isClosedError(err error) bool {
	return err == io.EOF || err == net.ErrClosed
}
