//go:build windows

package main

import (
	"context"
	. "tproxy/common"
)

type RelayApp struct {
	config  *Config
	connMgr *ConnectionManager
	msgBus  *MessageBus
	server  *RelayServer
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewRelayApp(config *Config) *RelayApp {
	connMgr := NewConnectionManager()
	msgBus := NewMessageBus(10000)
	ctx, cancel := context.WithCancel(context.Background())

	return &RelayApp{
		config:  config,
		connMgr: connMgr,
		msgBus:  msgBus,
		server:  NewRelayServer(config.Listen, connMgr, msgBus),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (app *RelayApp) Run() error {
	LOGI("=== Relay Server Starting ===")
	LOGI("Listen Address: ", app.config.Listen)

	// 启动事件处理循环
	go app.handleEvents(app.ctx)

	// 启动中继服务器（阻塞）
	return app.server.Start(app.ctx)
}

func (app *RelayApp) handleEvents(ctx context.Context) {
	msgChan := app.msgBus.GetMessageChannel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-msgChan:
			switch msg.Header.Source {
			case MsgSourceLocal:
				app.handleLocalMsg(msg)
			case MsgSourceProxy:
				app.handleProxyMsg(msg)
			default:
				LOGE("Unknown message source: ", msg.Header.Source)
			}
		}
	}
}

func (app *RelayApp) handleLocalMsg(msg Message) {
	switch msg.Header.MsgType {
	case MsgTypeConnect:
		LOGD("[local-event] Backend connected: ", msg.Header.UUID)
		msg.Header.Source = MsgSourceProxy
		if err := app.server.SendToDownstream(&msg); err != nil {
			LOGE("[local-event] Failed to send connect message: ", err)
		}

	case MsgTypeDisconnect:
		LOGD("[local-event] Backend disconnected: ", msg.Header.UUID)
		msg.Header.Source = MsgSourceProxy
		if err := app.server.SendToDownstream(&msg); err != nil {
			LOGE("[local-event] Failed to send disconnect message: ", err)
		}
		app.connMgr.Delete(msg.Header.UUID)

	case MsgTypeData:
		LOGD("[local-event] Backend data: ", msg.Header.UUID, " length: ", msg.Header.Len)
		msg.Header.Source = MsgSourceProxy
		if err := app.server.SendToDownstream(&msg); err != nil {
			LOGE("[local-event] Failed to send data message: ", err)
		}

	default:
		LOGE("[local-event] Unknown message type: ", msg.Header.MsgType)
	}
}

func (app *RelayApp) handleProxyMsg(msg Message) {
	switch msg.Header.MsgType {
	case MsgTypeConnect:
		LOGI("[upstream-event] Connect request: ", msg.Header.UUID, " to ", msg.Header.IPStr)

		// 创建连接信息
		connInfo := &ConnInfo{
			UUID:       msg.Header.UUID,
			IPStr:      msg.Header.IPStr,
			Status:     StatusDisconnected,
			MsgChannel: make(chan Message, 1000),
		}
		app.connMgr.Add(msg.Header.UUID, connInfo)

		go connectToBackend(msg.Header.UUID, msg.Header.IPStr, app.msgBus, app.connMgr)

	case MsgTypeData:
		conn, exists := app.connMgr.Get(msg.Header.UUID)
		if !exists {
			LOGE("[upstream-event] Connection not found: ", msg.Header.UUID)
			return
		}

		if conn.MsgChannel != nil {
			select {
			case conn.MsgChannel <- msg:
				LOGD("[upstream-event] Data queued: ", msg.Header.UUID, " length: ", msg.Header.Len)
			default:
				LOGE("[upstream-event] Message channel full: ", msg.Header.UUID)
			}
		}

	case MsgTypeDisconnect:
		LOGD("[upstream-event] Disconnect: ", msg.Header.UUID)
		conn, exists := app.connMgr.Get(msg.Header.UUID)
		if exists && conn.Conn != nil {
			conn.Conn.Close()
		}
		app.connMgr.Delete(msg.Header.UUID)

	default:
		LOGE("[upstream-event] Unknown message type: ", msg.Header.MsgType)
	}
}

// Shutdown 优雅关闭
func (app *RelayApp) Shutdown() {
	LOGI("=== Relay Server Shutting Down ===")

	if app.cancel != nil {
		app.cancel()
	}

	if app.server != nil {
		app.server.Close()
	}

	app.connMgr.ForEach(func(uuid string, info *ConnInfo) {
		app.connMgr.Delete(uuid)
	})

	LOGI("=== Shutdown Complete ===")
}