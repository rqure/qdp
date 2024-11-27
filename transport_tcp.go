package main

import (
	"bytes"
	"context"
	"log"
	"net"
	"sync"

	qdb "github.com/rqure/qdb/src"
)

type TCPServerTransport struct {
	addr string
	quit chan interface{}
	tx   chan *QdpMessage
	rx   chan *QdpMessage
}

type TCPClientTransport struct {
	ctx     context.Context
	conn    net.Conn
	tx      chan *QdpMessage
	rx      chan *QdpMessage
	OnClose func()
}

func (s *TCPServerTransport) Start() {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		log.Printf("Failed to listen on %s: %v", s.addr, err)
		return
	}

	qdb.Info("[TCPServerTransport::Start] Listening on %s", s.addr)

	go s.doListen(listener)
}

func NewTCPServerTransport(addr string) *TCPServerTransport {
	return &TCPServerTransport{
		addr: addr,
		quit: make(chan interface{}, 1),
		tx:   make(chan *QdpMessage, 100),
		rx:   make(chan *QdpMessage, 100),
	}
}

func NewTCPClientTransport(addr string) *TCPClientTransport {
	qdb.Info("[NewTCPClientTransport] Connecting to %s", addr)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		qdb.Error("[NewTCPClientTransport] Failed to connect to %s: %v", addr, err)
		return nil
	}

	return &TCPClientTransport{
		conn: conn,
		ctx:  context.Background(),
		tx:   make(chan *QdpMessage, 10),
		rx:   make(chan *QdpMessage, 10),
	}
}

func (s *TCPServerTransport) doListen(l net.Listener) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clients := sync.Map{}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				conn, err := l.Accept()
				if err != nil {
					qdb.Error("[TCPServerTransport::doListen] Failed to accept connection: %v", err)
					continue
				}

				client := &TCPClientTransport{
					ctx:  ctx,
					conn: conn,
					tx:   make(chan *QdpMessage, 10),
					rx:   s.rx,
				}

				client.OnClose = func() {
					clients.Delete(client)
				}

				clients.Store(client, nil)

				go client.doHandleConnection()
			}
		}
	}()

	for {
		select {
		case <-s.quit:
			l.Close()
			return
		case msg := <-s.tx:
			clients.Range(func(key, value interface{}) bool {
				client := key.(*TCPClientTransport)
				client.tx <- msg
				return true
			})
		}
	}
}

func (c *TCPClientTransport) doHandleConnection() {
	qdb.Info("[TCPClientTransport::doHandleConnection] Handling new connection: %v", c.conn.RemoteAddr())

	_, cancel := context.WithCancel(c.ctx)
	defer cancel()

	defer func() {
		c.conn.Close()

		if c.OnClose != nil {
			c.OnClose()
		}

		qdb.Info("[TCPClientTransport::doHandleConnection] Closed connection: %v", c.conn.RemoteAddr())
	}()

	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case msg := <-c.tx:
				if msg.Validity != MESSAGE_VALIDITY_VALID {
					qdb.Error("[TCPClientTransport::doHandleConnection] Attempted to send invalid message")
					return
				}

				qdb.Trace("[TCPClientTransport::doHandleConnection] Sending message: %v", msg)
				_, err := c.conn.Write(msg.ToBytes())
				if err != nil {
					qdb.Error("[TCPClientTransport::doHandleConnection] Failed to write to connection: %v", err)
					return
				}
			}
		}
	}()

	buffer := bytes.Buffer{}
	tempBuf := make([]byte, 1024)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			n, err := c.conn.Read(tempBuf)
			if err != nil {
				qdb.Error("[TCPClientTransport::doHandleConnection] Failed to read from connection: %v", err)
				return
			}

			buffer.Write(tempBuf[:n])

			msg := &QdpMessage{}
			remaining := msg.FromBytes(buffer.Bytes())
			qdb.Trace("[TCPClientTransport::doHandleConnection] Received %d bytes: %s", n, msg)

			buffer.Reset()
			buffer.Write(remaining)

			if msg.Validity == MESSAGE_VALIDITY_VALID {
				select {
				case c.rx <- msg:
				case <-c.ctx.Done():
					return
				}
			}
		}
	}
}

func (s *TCPServerTransport) Stop() {
	qdb.Info("[TCPServerTransport::Stop] Stopping server: %s", s.addr)

	s.quit <- nil
}

func (s *TCPServerTransport) Send(m *QdpMessage) {
	m.Complete()

	s.tx <- m
}

func (s *TCPServerTransport) Recv() *QdpMessage {
	select {
	case m := <-s.rx:
		return m
	default:
		return nil
	}
}
