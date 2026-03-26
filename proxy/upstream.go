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
	webhookURL string
	connMgr    *ConnectionManager
	msgBus     *MessageBus
	bufPool    *BufferPool
	mu         sync.RWMutex
	conn       net.Conn
	reader     *MessageReader
	writer     *MessageWriter
	status     int
	notified   bool // relay 断线通知标志
}

func NewUpstreamClient(serverAddr string, webhookURL string, connMgr *ConnectionManager, msgBus *MessageBus) *UpstreamClient {
	return &UpstreamClient{
		serverAddr: serverAddr,
		webhookURL: webhookURL,
		connMgr:    connMgr,
		msgBus:     msgBus,
		status:     StatusDisconnected,
	}
}

func (c *UpstreamClient) Start(ctx context.Context) error {
	LOGI("[upstream] Starting client, connecting to: ", c.serverAddr)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := c.connect(); err != nil {
				LOGI("[upstream] Connect failed: ", err)
				// 使用 select 等待，使 sleep 可被 ctx 取消中断
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(3 * time.Second):
				}
				continue
			}

			// 连接成功，重置通知标志
			c.notified = false

			if err := c.receiveLoop(ctx); err != nil {
				LOGE("[upstream] Receive loop error: ", err)
			}

			c.cleanup()

			// relay 断开后，清理所有本地连接，使 readLoop goroutine 立即退出
			c.cleanupAllConnections()

			// relay 断线通知（仅通知一次）
			if !c.notified {
				c.notified = true
				SendWechatNotify(c.webhookURL, "提示: 服务端掉线")
			}
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

func (c *UpstreamClient) cleanupAllConnections() {
	var uuids []string
	c.connMgr.ForEach(func(uuid string, info *ConnInfo) {
		uuids = append(uuids, uuid)
	})
	for _, uuid := range uuids {
		c.connMgr.Delete(uuid)
	}
	if len(uuids) > 0 {
		LOGI("[upstream] Upstream disconnected, cleaned up ", len(uuids), " local connections")
	}
}

func (c *UpstreamClient) Close() {
	c.cleanup()
}

func (c *UpstreamClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status == StatusConnected
}