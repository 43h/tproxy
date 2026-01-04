package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
	. "tproxy/common"
)

type UpstreamClient struct {
	serverAddr string
	connMgr    *ConnectionManager
	msgBus     *MessageBus
	bufPool    *BufferPool
	mu         sync.RWMutex
	conn       net.Conn
	reader     *MessageReader
	writer     *MessageWriter
	status     int
}

func NewUpstreamClient(serverAddr string, connMgr *ConnectionManager, msgBus *MessageBus) *UpstreamClient {
	return &UpstreamClient{
		serverAddr: serverAddr,
		connMgr:    connMgr,
		msgBus:     msgBus,
		status:     StatusDisconnected,
	}
}

func (c *UpstreamClient) Start(ctx context.Context) {
	LOGI("[upstream] Starting client, connecting to: ", c.serverAddr)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := c.connect(); err != nil {
				time.Sleep(time.Second * 3)
				LOGI("[upstream] Connect failed: ", err)
				continue
			}

			if err := c.receiveLoop(ctx); err != nil {
				LOGE("[upstream] Receive loop error: ", err)
			}

			c.cleanup()
		}
	}
}

// connect 连接到上游服务器
func (c *UpstreamClient) connect() error {
	conn, err := net.Dial("tcp", c.serverAddr)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.reader = NewMessageReader(conn)
	c.writer = NewMessageWriter(conn)
	c.status = StatusConnected
	c.mu.Unlock()

	LOGI("[upstream] Connected to server: ", c.serverAddr)
	return nil
}

func (c *UpstreamClient) receiveLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			c.mu.RLock()
			reader := c.reader
			c.mu.RUnlock()

			if reader == nil {
				return fmt.Errorf("reader is nil")
			}

			msg, err := reader.ReadMessage()
			if err != nil {
				return fmt.Errorf("read message failed: %w", err)
			}

			LOGD("[upstream] Received message: ", msg.Header.MsgType, " UUID: ", msg.Header.UUID)
			msg.Header.Source = MsgSourceRelay
			c.msgBus.SendMessage(*msg)
		}
	}
}

func (c *UpstreamClient) SendMessage(msg *Message) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.status != StatusConnected || c.writer == nil {
		return fmt.Errorf("not connected")
	}

	msg.Header.Source = MsgSourceProxy

	if err := c.writer.WriteMessage(msg); err != nil {
		return fmt.Errorf("write message failed: %w", err)
	}

	LOGD("[upstream] Sent message: ", msg.Header.MsgType, " UUID: ", msg.Header.UUID)
	return nil
}

func (c *UpstreamClient) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	c.reader = nil
	c.writer = nil
	c.status = StatusDisconnected

	LOGI("[upstream] Connection cleaned up")
}

func (c *UpstreamClient) Close() {
	c.cleanup()
}

func (c *UpstreamClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status == StatusConnected
}
