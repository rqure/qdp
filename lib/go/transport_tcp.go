package qdp

import (
	"context"
	"encoding/binary"
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

func (t *TCPClientTransport) ReadMessage() (*Message, error) {
	select {
	case <-t.ctx.Done():
		return nil, io.EOF
	default:
		msg, err := readMessage(t.conn)
		if err != nil && t.connectionHandler != nil && t.disconnected.CompareAndSwap(false, true) {
			t.connectionHandler.OnDisconnect(t, err)
		}
		return msg, err
	}
}

func (t *TCPClientTransport) WriteMessage(msg *Message) error {
	select {
	case <-t.ctx.Done():
		return io.EOF
	default:
		return writeMessage(t.conn, msg)
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
	clients           sync.Map    // Map of connected clients
	disconnected      atomic.Bool // Add atomic flag for server disconnect state
	msgCh             chan *Message
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
		msgCh:             make(chan *Message, 100), // Buffered channel for messages
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

func (t *TCPServerTransport) handleClientReads(client *TCPClientTransport) {
	defer t.wg.Done()

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
			msg, err := client.ReadMessage()
			if err != nil {
				t.clients.Delete(client)
				return
			}

			// broadcast to other clients
			var errors []error
			t.clients.Range(func(key, _ interface{}) bool {
				client2 := key.(*TCPClientTransport)
				if client2 == client {
					return true // continue iteration
				}

				if err := client2.WriteMessage(msg); err != nil {
					errors = append(errors, err)
					// Remove failed client
					t.clients.Delete(client2)
					client2.Close()
				}

				return true // continue iteration
			})

			select {
			case t.msgCh <- msg:
			case <-t.ctx.Done():
				return
			}
		}
	}
}

func (t *TCPServerTransport) ReadMessage() (*Message, error) {
	select {
	case <-t.ctx.Done():
		return nil, io.EOF
	case msg := <-t.msgCh:
		return msg, nil
	}
}

func (t *TCPServerTransport) WriteMessage(msg *Message) error {
	var errors []error
	t.clients.Range(func(key, _ interface{}) bool {
		client := key.(*TCPClientTransport)
		if err := client.WriteMessage(msg); err != nil {
			errors = append(errors, err)
			// Remove failed client
			t.clients.Delete(client)
			client.Close()
		}
		return true // continue iteration
	})
	if len(errors) > 0 {
		return fmt.Errorf("broadcast errors: %v", errors)
	}
	return nil
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

// Helper functions for message encoding/decoding
func readMessage(r io.Reader) (*Message, error) {
	// Read header (8 bytes: topic length + payload length)
	header := make([]byte, 8)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	topicLen := binary.LittleEndian.Uint32(header[0:4])
	payloadLen := binary.LittleEndian.Uint32(header[4:8])

	// Read topic
	topic := make([]byte, topicLen)
	if _, err := io.ReadFull(r, topic); err != nil {
		return nil, err
	}

	// Read payload
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	return &Message{
		Topic:   string(topic),
		Payload: payload,
	}, nil
}

func writeMessage(w io.Writer, msg *Message) error {
	// Write header (topic length + payload length)
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(msg.Topic)))
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(msg.Payload)))

	if _, err := w.Write(header); err != nil {
		return err
	}

	// Write topic
	if _, err := w.Write([]byte(msg.Topic)); err != nil {
		return err
	}

	// Write payload
	if _, err := w.Write(msg.Payload); err != nil {
		return err
	}

	return nil
}
