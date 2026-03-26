package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	. "tproxy/common"
)

type RelayServer struct {
	addr       string
	webhookURL string
	connMgr    *ConnectionManager
	msgBus     *MessageBus
	listener   net.Listener

	mu               sync.RWMutex
	downstreamConn   net.Conn
	downstreamReader *MessageReader
	downstreamWriter *MessageWriter
	downstreamStatus int
	sendChan         chan Message
	ctx              context.Context
	cancelSend       context.CancelFunc
}

func NewRelayServer(addr string, webhookURL string, connMgr *ConnectionManager, msgBus *MessageBus) *RelayServer {
	return &RelayServer{
		addr:             addr,
		webhookURL:       webhookURL,
		connMgr:          connMgr,
		msgBus:           msgBus,
		downstreamStatus: StatusDisconnected,
	}
}

func (s *RelayServer) Start(ctx context.Context) error {
	LOGI("[relay] Starting server on: ", s.addr)

	tmpListener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}

	s.listener = tmpListener
	LOGI("[relay] Server listening on ", s.addr)

	return s.acceptLoop(ctx)
}

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

			s.mu.Lock()
			if s.downstreamConn != nil || s.downstreamStatus == StatusConnected {
				LOGI("[relay] Only one client allowed, rejecting new connection")
				if err := conn.Close(); err != nil {
					LOGE("[relay] Failed to close rejected connection: ", err)
				}
				s.mu.Unlock()
				continue
			}

			sendCtx, cancelSend := context.WithCancel(ctx)

			s.downstreamConn = conn
			s.downstreamReader = NewMessageReader(conn)
			s.downstreamWriter = NewMessageWriter(conn)
			s.downstreamStatus = StatusConnected
			s.sendChan = make(chan Message, 4096)
			s.ctx = sendCtx
			s.cancelSend = cancelSend
			s.mu.Unlock()

			LOGI("[relay] Downstream client connected from: ", conn.RemoteAddr())

			go s.receiveLoop(ctx, conn)
			go s.sendLoop(sendCtx, conn)
		}
	}
}

func (s *RelayServer) receiveLoop(ctx context.Context, conn net.Conn) {

	defer func() {
		s.mu.Lock()

		if s.cancelSend != nil {
			s.cancelSend()
			s.cancelSend = nil
		}

		if s.sendChan != nil {
			close(s.sendChan)
			s.sendChan = nil
		}

		s.downstreamConn = nil
		s.downstreamReader = nil
		s.downstreamWriter = nil
		s.downstreamStatus = StatusDisconnected
		s.mu.Unlock()

		if err := conn.Close(); err != nil {
			LOGE("[relay] Failed to close connection: ", err)
		}

		// proxy 断开后，清理所有 backend 连接，使后端 goroutine 立即退出
		var uuids []string
		s.connMgr.ForEach(func(uuid string, info *ConnInfo) {
			uuids = append(uuids, uuid)
		})
		for _, uuid := range uuids {
			s.connMgr.Delete(uuid)
		}
		LOGI("[relay] Downstream client disconnected, cleaned up ", len(uuids), " backend connections")

		SendWechatNotify(s.webhookURL, "提示: 客户端服务掉线")
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

			msg.Header.Source = MsgSourceProxy

			s.msgBus.SendMessage(*msg)

			LOGD("[relay] Received message from downstream: ", msg.Header.MsgType, " UUID: ", msg.Header.UUID)
		}
	}
}

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

func (s *RelayServer) SendToDownstream(msg *Message) error {
	s.mu.RLock()
	status := s.downstreamStatus
	sendChan := s.sendChan
	ctx := s.ctx
	s.mu.RUnlock()

	if status != StatusConnected || sendChan == nil {
		return fmt.Errorf("downstream not connected")
	}

	select {
	case sendChan <- *msg:
		LOGD("[relay] Message queued for downstream: ", msg.Header.MsgType, " UUID: ", msg.Header.UUID)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("downstream disconnected")
	}
}

func (s *RelayServer) SendQueueLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.sendChan == nil {
		return 0
	}
	return len(s.sendChan)
}

func (s *RelayServer) SendQueueCap() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.sendChan == nil {
		return 0
	}
	return cap(s.sendChan)
}

func (s *RelayServer) Close() {
	LOGI("[relay] Closing server")

	s.mu.Lock()
	if s.downstreamConn != nil {
		s.downstreamConn.Close()
		s.downstreamConn = nil
	}
	s.downstreamStatus = StatusDisconnected
	s.mu.Unlock()

	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			LOGE("[relay] Close listener failed: ", err)
		} else {
			LOGI("[relay] Listener closed")
		}
		s.listener = nil
	}
}
