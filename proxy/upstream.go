//go:build linux

package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	. "tproxy/common"
)

// UpstreamClient 上游连接客户端（连接到 relay 服务器）
type UpstreamClient struct {
	serverAddr string
	connMgr    *ConnectionManager
	eventBus   *MessageBus

	mu     sync.RWMutex
	conn   net.Conn
	reader *MessageReader
	writer *MessageWriter
	status int
}

// NewUpstreamClient 创建上游客户端
func NewUpstreamClient(serverAddr string, connMgr *ConnectionManager, eventBus *MessageBus) *UpstreamClient {
	return &UpstreamClient{
		serverAddr: serverAddr,
		connMgr:    connMgr,
		eventBus:   eventBus,
		status:     StatusDisconnected,
	}
}

// Start 启动上游客户端
func (c *UpstreamClient) Start(ctx context.Context) error {
	LOGI("[upstream] Starting client, connecting to: ", c.serverAddr)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := c.connect(); err != nil {
				LOGE("[upstream] Connect failed: ", err)
				continue
			}

			// 连接成功，开始接收消息
			if err := c.receiveLoop(ctx); err != nil {
				LOGE("[upstream] Receive loop error: ", err)
			}

			// 连接断开，清理资源
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

// receiveLoop 接收消息循环
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

			// 设置消息来源为上游
			msg.Source = MsgSourceRelay

			// 发送到事件总线
			c.eventBus.SendMessage(*msg)

			LOGD("[upstream] Received message: ", msg.MsgType, " UUID: ", msg.UUID)
		}
	}
}

// SendMessage 发送消息到上游服务器
func (c *UpstreamClient) SendMessage(msg *Message) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.status != StatusConnected || c.writer == nil {
		return fmt.Errorf("not connected")
	}

	// 设置消息来源
	msg.Source = MsgSourceRelay

	if err := c.writer.WriteMessage(msg); err != nil {
		return fmt.Errorf("write message failed: %w", err)
	}

	LOGD("[upstream] Sent message: ", msg.MsgType, " UUID: ", msg.UUID)
	return nil
}

// cleanup 清理连接资源
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

// Close 关闭上游连接
func (c *UpstreamClient) Close() {
	c.cleanup()
}

// IsConnected 检查是否已连接
func (c *UpstreamClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status == StatusConnected
}