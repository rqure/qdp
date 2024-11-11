package main

import (
	"bytes"
	"context"
	"log"
	"net"
	"sync"

	qdb "github.com/rqure/qdb/src"
)

type TCPServerGateway struct {
	addr string
	quit chan interface{}
	tx   chan *QdpMessage
	rx   chan *QdpMessage
}

type TCPClientGateway struct {
	ctx     context.Context
	conn    net.Conn
	tx      chan *QdpMessage
	rx      chan *QdpMessage
	OnClose func()
}

func (s *TCPServerGateway) Start() {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		log.Printf("Failed to listen on %s: %v", s.addr, err)
		return
	}

	qdb.Info("[TCPServerGateway::Start] Listening on %s", s.addr)

	go s.doListen(listener)
}

func NewTCPServerGateway(addr string) *TCPServerGateway {
	return &TCPServerGateway{
		addr: addr,
		quit: make(chan interface{}, 1),
		tx:   make(chan *QdpMessage, 100),
		rx:   make(chan *QdpMessage, 100),
	}
}

func NewTCPClientGateway(addr string) *TCPClientGateway {
	qdb.Info("[NewTCPClientGateway] Connecting to %s", addr)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		qdb.Error("[NewTCPClientGateway] Failed to connect to %s: %v", addr, err)
		return nil
	}

	return &TCPClientGateway{
		conn: conn,
		ctx:  context.Background(),
		tx:   make(chan *QdpMessage, 10),
		rx:   make(chan *QdpMessage, 10),
	}
}

func (s *TCPServerGateway) doListen(l net.Listener) {
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
					qdb.Error("[TCPServerGateway::doListen] Failed to accept connection: %v", err)
					continue
				}

				client := &TCPClientGateway{
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
				client := key.(*TCPClientGateway)
				client.tx <- msg
				return true
			})
		}
	}
}

func (c *TCPClientGateway) doHandleConnection() {
	qdb.Info("[TCPClientGateway::doHandleConnection] Handling new connection: %v", c.conn.RemoteAddr())

	_, cancel := context.WithCancel(c.ctx)
	defer cancel()

	defer func() {
		c.conn.Close()

		if c.OnClose != nil {
			c.OnClose()
		}

		qdb.Info("[TCPClientGateway::doHandleConnection] Closed connection: %v", c.conn.RemoteAddr())
	}()

	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case msg := <-c.tx:
				if msg.Validity != MESSAGE_VALIDITY_VALID {
					qdb.Error("[TCPClientGateway::doHandleConnection] Attempted to send invalid message")
					return
				}

				qdb.Trace("[TCPClientGateway::doHandleConnection] Sending message: %v", msg)
				_, err := c.conn.Write(msg.ToBytes())
				if err != nil {
					qdb.Error("[TCPClientGateway::doHandleConnection] Failed to write to connection: %v", err)
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
				qdb.Error("[TCPClientGateway::doHandleConnection] Failed to read from connection: %v", err)
				return
			}

			buffer.Write(tempBuf[:n])

			msg := &QdpMessage{}
			remaining := msg.FromBytes(buffer.Bytes())
			qdb.Trace("[TCPClientGateway::doHandleConnection] Received %d bytes: %s", n, msg)

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

func (s *TCPServerGateway) Stop() {
	qdb.Info("[TCPServerGateway::Stop] Stopping server: %s", s.addr)

	s.quit <- nil
}

func (s *TCPServerGateway) Send(m *QdpMessage) {
	m.Complete()

	s.tx <- m
}

func (s *TCPServerGateway) Recv() *QdpMessage {
	select {
	case m := <-s.rx:
		return m
	default:
		return nil
	}
}
