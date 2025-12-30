//go:build windows

package main

import (
	. "tproxy/common"
	"context"
	"fmt"
	"net"
	"sync"
)

// RelayServer 中继服务器（监听来自tproxy的连接）
type RelayServer struct {
	addr     string
	connMgr  *ConnectionManager
	eventBus *EventBus
	listener net.Listener

	// 下游连接（tproxy客户端）
	mu               sync.RWMutex
	downstreamConn   net.Conn
	downstreamReader *MessageReader
	downstreamWriter *MessageWriter
	downstreamStatus int
}

// NewRelayServer 创建中继服务器
func NewRelayServer(addr string, connMgr *ConnectionManager, eventBus *EventBus) *RelayServer {
	return &RelayServer{
		addr:             addr,
		connMgr:          connMgr,
		eventBus:         eventBus,
		downstreamStatus: StatusDisconnected,
	}
}

// Start 启动中继服务器
func (s *RelayServer) Start(ctx context.Context) error {
	LOGI("[relay] Starting server on: ", s.addr)

	// 创建监听器
	tmpListener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}

	s.listener = tmpListener
	LOGI("[relay] Server listening on ", s.addr)

	// 接受连接循环
	return s.acceptLoop(ctx)
}

// acceptLoop 接受连接循环
func (s *RelayServer) acceptLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				LOGE("[relay] Accept failed: ", err)
				continue
			}

			// 检查是否已有连接（只允许一个tproxy客户端）
			s.mu.Lock()
			if s.downstreamConn != nil || s.downstreamStatus == StatusConnected {
				LOGI("[relay] Only one client allowed, rejecting new connection")
				conn.Close()
				s.mu.Unlock()
				continue
			}

			// 接受新连接
			s.downstreamConn = conn
			s.downstreamReader = NewMessageReader(conn)
			s.downstreamWriter = NewMessageWriter(conn)
			s.downstreamStatus = StatusConnected
			s.mu.Unlock()

			LOGI("[relay] Downstream client connected from: ", conn.RemoteAddr())

			// 启动接收循环
			go s.receiveLoop(ctx, conn)
		}
	}
}

// receiveLoop 接收来自下游（tproxy）的消息
func (s *RelayServer) receiveLoop(ctx context.Context, conn net.Conn) {
	defer func() {
		s.mu.Lock()
		s.downstreamConn = nil
		s.downstreamReader = nil
		s.downstreamWriter = nil
		s.downstreamStatus = StatusDisconnected
		s.mu.Unlock()

		conn.Close()
		LOGI("[relay] Downstream client disconnected")
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			s.mu.RLock()
			reader := s.downstreamReader
			s.mu.RUnlock()

			if reader == nil {
				return
			}

			msg, err := reader.ReadMessage()
			if err != nil {
				LOGE("[relay] Read message failed: ", err)
				return
			}

			// 设置消息来源为上游
			msg.Source = MsgSourceRelay

			// 发送到事件总线
			s.eventBus.SendMessage(*msg)

			LOGD("[relay] Received message from downstream: ", msg.MessageType, " UUID: ", msg.UUID)
		}
	}
}

// SendToDownstream 发送消息到下游（tproxy）
func (s *RelayServer) SendToDownstream(msg *Message) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.downstreamStatus != StatusConnected || s.downstreamWriter == nil {
		return fmt.Errorf("downstream not connected")
	}

	if err := s.downstreamWriter.WriteMessage(msg); err != nil {
		return fmt.Errorf("write message failed: %w", err)
	}

	LOGD("[relay] Sent message to downstream: ", msg.MessageType, " UUID: ", msg.UUID)
	return nil
}

// Close 关闭服务器
func (s *RelayServer) Close() {
	LOGI("[relay] Closing server")

	// 关闭下游连接
	s.mu.Lock()
	if s.downstreamConn != nil {
		s.downstreamConn.Close()
		s.downstreamConn = nil
	}
	s.downstreamStatus = StatusDisconnected
	s.mu.Unlock()

	// 关闭监听器
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			LOGE("[relay] Close listener failed: ", err)
		} else {
			LOGI("[relay] Listener closed")
		}
		s.listener = nil
	}
}
