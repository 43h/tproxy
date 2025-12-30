//go:build windows

package main

import (
	. "tproxy/common"
	"context"
)

// RelayApp 中继服务器应用
type RelayApp struct {
	config   *Config
	connMgr  *ConnectionManager
	eventBus *EventBus
	server   *RelayServer
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewRelayApp 创建应用
func NewRelayApp(config *Config) *RelayApp {
	connMgr := NewConnectionManager()
	eventBus := NewEventBus(10000)
	ctx, cancel := context.WithCancel(context.Background())

	return &RelayApp{
		config:   config,
		connMgr:  connMgr,
		eventBus: eventBus,
		server:   NewRelayServer(config.Listen, connMgr, eventBus),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Run 运行应用
func (app *RelayApp) Run() error {
	LOGI("=== Relay Server Starting ===")
	LOGI("Listen Address: ", app.config.Listen)

	// 启动事件处理循环
	go app.handleEvents(app.ctx)

	// 启动中继服务器（阻塞）
	return app.server.Start(app.ctx)
}

// handleEvents 处理事件循环
func (app *RelayApp) handleEvents(ctx context.Context) {
	msgChan := app.eventBus.GetMessageChannel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-msgChan:
			app.processMessage(msg)
		}
	}
}

// processMessage 处理消息
func (app *RelayApp) processMessage(msg Message) {
	switch msg.Source {
	case MsgSourceLocal:
		app.handleLocalEvent(msg)
	case MsgSourceRelay:
		app.handleUpstreamEvent(msg)
	default:
		LOGE("Unknown message source: ", msg.Source)
	}
}

// handleLocalEvent 处理本地事件（来自后端真实服务器）
func (app *RelayApp) handleLocalEvent(msg Message) {
	switch msg.MessageType {
	case MsgTypeConnect:
		// 后端连接成功，通知下游（tproxy）
		LOGD("[local-event] Backend connected: ", msg.UUID)
		msg.Source = MsgSourceTproxy
		if err := app.server.SendToDownstream(&msg); err != nil {
			LOGE("[local-event] Failed to send connect message: ", err)
		}

	case MsgTypeDisconnect:
		// 后端连接断开，通知下游
		LOGD("[local-event] Backend disconnected: ", msg.UUID)
		msg.Source = MsgSourceTproxy
		if err := app.server.SendToDownstream(&msg); err != nil {
			LOGE("[local-event] Failed to send disconnect message: ", err)
		}
		app.connMgr.Delete(msg.UUID)

	case MsgTypeData:
		// 后端数据，转发到下游
		LOGD("[local-event] Backend data: ", msg.UUID, " length: ", msg.Length)
		msg.Source = MsgSourceTproxy
		if err := app.server.SendToDownstream(&msg); err != nil {
			LOGE("[local-event] Failed to send data message: ", err)
		}

	default:
		LOGE("[local-event] Unknown message type: ", msg.MessageType)
	}
}

// handleUpstreamEvent 处理上游事件（来自tproxy的消息）
func (app *RelayApp) handleUpstreamEvent(msg Message) {
	switch msg.MessageType {
	case MsgTypeConnect:
		// tproxy请求连接到真实服务器
		LOGI("[upstream-event] Connect request: ", msg.UUID, " to ", msg.IPStr)

		// 创建连接信息
		connInfo := &ConnInfo{
			UUID:       msg.UUID,
			IPStr:      msg.IPStr,
			Status:     StatusDisconnected,
			MsgChannel: make(chan Message, 1000),
		}
		app.connMgr.Add(msg.UUID, connInfo)

		// 启动后端连接
		go connectToBackend(msg.UUID, msg.IPStr, app.eventBus, app.connMgr)

	case MsgTypeData:
		// 上游数据，转发到后端真实服务器
		conn, exists := app.connMgr.Get(msg.UUID)
		if !exists {
			LOGE("[upstream-event] Connection not found: ", msg.UUID)
			return
		}

		if conn.MsgChannel != nil {
			select {
			case conn.MsgChannel <- msg:
				LOGD("[upstream-event] Data queued: ", msg.UUID, " length: ", msg.Length)
			default:
				LOGE("[upstream-event] Message channel full: ", msg.UUID)
			}
		}

	case MsgTypeDisconnect:
		// 上游断开连接
		LOGD("[upstream-event] Disconnect: ", msg.UUID)
		conn, exists := app.connMgr.Get(msg.UUID)
		if exists && conn.Conn != nil {
			conn.Conn.Close()
		}
		app.connMgr.Delete(msg.UUID)

	default:
		LOGE("[upstream-event] Unknown message type: ", msg.MessageType)
	}
}

// Shutdown 优雅关闭
func (app *RelayApp) Shutdown() {
	LOGI("=== Relay Server Shutting Down ===")

	// 取消context，停止所有goroutine
	if app.cancel != nil {
		app.cancel()
	}

	// 关闭服务器
	if app.server != nil {
		app.server.Close()
	}

	// 清理所有连接
	app.connMgr.ForEach(func(uuid string, info *ConnInfo) {
		app.connMgr.Delete(uuid)
	})

	LOGI("=== Shutdown Complete ===")
}
