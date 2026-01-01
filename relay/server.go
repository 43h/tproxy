//go:build windows

package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	. "tproxy/common"
)

// RelayServer 中继服务器（监听来自tproxy的连接）
type RelayServer struct {
	addr     string
	connMgr  *ConnectionManager
	msgBus   *MessageBus
	listener net.Listener

	// 下游连接（tproxy客户端）
	mu               sync.RWMutex
	downstreamConn   net.Conn
	downstreamReader *MessageReader
	downstreamWriter *MessageWriter
	downstreamStatus int
	sendChan         chan Message // 发送消息通道
	ctx              context.Context
	cancelSend       context.CancelFunc
}

// NewRelayServer 创建中继服务器
func NewRelayServer(addr string, connMgr *ConnectionManager, msgBus *MessageBus) *RelayServer {
	return &RelayServer{
		addr:             addr,
		connMgr:          connMgr,
		msgBus:           msgBus,
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

			// 创建发送goroutine的context
			sendCtx, cancelSend := context.WithCancel(ctx)

			// 接受新连接
			s.downstreamConn = conn
			s.downstreamReader = NewMessageReader(conn)
			s.downstreamWriter = NewMessageWriter(conn)
			s.downstreamStatus = StatusConnected
			s.sendChan = make(chan Message, 10000)
			s.ctx = sendCtx
			s.cancelSend = cancelSend
			s.mu.Unlock()

			LOGI("[relay] Downstream client connected from: ", conn.RemoteAddr())

			// 启动接收和发送循环
			go s.receiveLoop(ctx, conn)
			go s.sendLoop(sendCtx, conn)
		}
	}
}

// receiveLoop 接收来自下游的消息
func (s *RelayServer) receiveLoop(ctx context.Context, conn net.Conn) {
	defer func() {
		s.mu.Lock()

		// 取消发送goroutine
		if s.cancelSend != nil {
			s.cancelSend()
			s.cancelSend = nil
		}

		// 关闭发送通道
		if s.sendChan != nil {
			close(s.sendChan)
			s.sendChan = nil
		}

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

			// 设置消息来源为Proxy
			msg.Header.Source = MsgSourceProxy

			// 发送到事件总线
			s.msgBus.SendMessage(*msg)

			LOGD("[relay] Received message from downstream: ", msg.Header.MsgType, " UUID: ", msg.Header.UUID)
		}
	}
}

// sendLoop 发送消息到下游循环
func (s *RelayServer) sendLoop(ctx context.Context, conn net.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.sendChan:
			s.mu.RLock()
			writer := s.downstreamWriter
			s.mu.RUnlock()

			if writer == nil {
				LOGE("[relay] Writer is nil")
				continue
			}

			if err := writer.WriteMessage(&msg); err != nil {
				LOGE("[relay] Write message failed: ", err)
				return
			}

			LOGD("[relay] Sent message to downstream: ", msg.Header.MsgType, " UUID: ", msg.Header.UUID)
		}
	}
}

// SendToDownstream 发送消息到下游（tproxy）通过消息通道
func (s *RelayServer) SendToDownstream(msg *Message) error {
	s.mu.RLock()
	status := s.downstreamStatus
	sendChan := s.sendChan
	s.mu.RUnlock()

	if status != StatusConnected || sendChan == nil {
		return fmt.Errorf("downstream not connected")
	}

	// 发送到通道
	select {
	case sendChan <- *msg:
		LOGD("[relay] Message queued for downstream: ", msg.Header.MsgType, " UUID: ", msg.Header.UUID)
		return nil
	default:
		return fmt.Errorf("send channel full")
	}
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
