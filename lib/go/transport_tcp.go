package qdp

import (
	"context"
	"io"
	"net"
	"sync"
)

type TCPServerTransport struct {
	addr     string
	listener net.Listener
	clients  sync.Map
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

type TCPClientTransport struct {
	conn   net.Conn
	ctx    context.Context
	cancel context.CancelFunc
}

func NewTCPServerTransport(addr string) *TCPServerTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &TCPServerTransport{
		addr:   addr,
		ctx:    ctx,
		cancel: cancel,
	}
}

func NewTCPClientTransport(addr string) (*TCPClientTransport, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	client := &TCPClientTransport{
		conn:   conn,
		ctx:    ctx,
		cancel: cancel,
	}

	return client, nil
}

func (s *TCPServerTransport) Start() error {
	l, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = l

	s.wg.Add(1)
	go s.acceptLoop()
	return nil
}

func (s *TCPServerTransport) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		s.listener.Close()
	}
	s.wg.Wait()
}

func (s *TCPServerTransport) acceptLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				continue
			}

			client := &TCPClientTransport{
				conn: conn,
			}

			s.clients.Store(conn.RemoteAddr().String(), client)
			s.wg.Add(1)
			go s.handleClient(client)
		}
	}
}

func (s *TCPServerTransport) handleClient(client *TCPClientTransport) {
	defer s.wg.Done()
	defer s.clients.Delete(client.conn.RemoteAddr().String())
	defer client.conn.Close()

	stream := Stream{
		buffer: Buffer{
			data:     make([]byte, MaxBufferCapacity),
			capacity: MaxBufferCapacity,
		},
	}

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			stream.buffer.Reset()
			if err := readQDPMessage(client.conn, &stream.buffer); err != nil {
				return
			}

			// Echo back to all connected clients
			s.clients.Range(func(_, value interface{}) bool {
				c := value.(*TCPClientTransport)
				writeQDPMessage(c.conn, &stream.buffer)
				return true
			})
		}
	}
}

func readQDPMessage(conn net.Conn, buf *Buffer) error {
	header := make([]byte, 8)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}

	topicLen := int(readUint32(header[0:]))
	payloadLen := int(readUint32(header[4:]))

	if topicLen > MaxTopicLength || payloadLen > MaxPayloadSize {
		return ErrPayloadTooLarge
	}

	// Copy header to buffer
	copy(buf.data[buf.size:], header)
	buf.size += 8

	// Read message data
	totalLen := topicLen + payloadLen + 4 // +4 for checksum
	if _, err := io.ReadFull(conn, buf.data[buf.size:buf.size+totalLen]); err != nil {
		return err
	}
	buf.size += totalLen

	return nil
}

func writeQDPMessage(conn net.Conn, buf *Buffer) error {
	_, err := conn.Write(buf.data[:buf.size])
	return err
}

func (c *TCPClientTransport) Send(buf *Buffer, _ interface{}) error {
	return writeQDPMessage(c.conn, buf)
}

func (c *TCPClientTransport) Receive(buf *Buffer, _ interface{}) error {
	return readQDPMessage(c.conn, buf)
}

func (c *TCPClientTransport) Close() {
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		c.conn.Close()
	}
}
