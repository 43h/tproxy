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
	server := NewRelayServer(config.Listen, connMgr, msgBus)

	return &RelayApp{
		config:  config,
		connMgr: connMgr,
		msgBus:  msgBus,
		server:  server,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (app *RelayApp) Run() error {
	LOGI("=== Relay Server Starting ===")
	LOGI("Listen Address: ", app.config.Listen)

	go app.handleMessages(app.ctx)

	return app.server.Start(app.ctx)
}

func (app *RelayApp) handleMessages(ctx context.Context) {
	msgChan := app.msgBus.GetMessageChannel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-msgChan:
			switch msg.Header.Source {
			case MsgSourceLocal:
				app.handleLocalMessage(msg)
			case MsgSourceProxy:
				app.handleProxyMessage(msg)
			default:
				LOGE("Unknown message source: ", msg.Header.Source)
			}
		}
	}
}

func (app *RelayApp) handleLocalMessage(msg Message) {
	switch msg.Header.MsgType {
	case MsgTypeDisconnect:
		LOGD("[local-msg] Backend disconnected: ", msg.Header.UUID)
		connInfo, exists := app.connMgr.Get(msg.Header.UUID)
		if !exists {
			LOGE("[upstream-msg] Connection not found: ", msg.Header.UUID)
			return
		}
		if connInfo.Status == StatusConnected {
			msg.Header.Source = MsgSourceRelay
			if err := app.server.SendToDownstream(&msg); err != nil {
				LOGE("[local-msg] Failed to send disconnect message: ", err)
			}
		}
		app.connMgr.Delete(msg.Header.UUID)

	case MsgTypeData:
		msg.Header.Source = MsgSourceRelay
		if err := app.server.SendToDownstream(&msg); err != nil {
			LOGE("[local-msg] Failed to send data message: ", err)
		}

	default:
		LOGE("[local-msg] Unknown message type: ", msg.Header.MsgType)
	}
}

func (app *RelayApp) handleProxyMessage(msg Message) {
	switch msg.Header.MsgType {
	case MsgTypeConnect:
		connInfo := &ConnInfo{
			UUID:       msg.Header.UUID,
			IPStr:      msg.Header.IPStr,
			Status:     StatusDisconnected,
			MsgChannel: make(chan Message, 10000),
		}
		app.connMgr.Add(msg.Header.UUID, connInfo)

		go connectToBackend(msg.Header.UUID, msg.Header.IPStr, app.msgBus, app.connMgr)

	case MsgTypeData:
		conn, exists := app.connMgr.Get(msg.Header.UUID)
		if !exists {
			LOGE("[proxy-msg] Connection not found: ", msg.Header.UUID)
			BufferPool2K.Put(msg.Data[:0])
			return
		}

		// 将数据放入 MsgChannel，由 handleBackendSend 协程处理
		// 这样即使连接还在建立中，数据也会被缓存，避免丢包
		select {
		case conn.MsgChannel <- msg:
			LOGD("[proxy-msg] Data queued: ", msg.Header.UUID, " len: ", len(msg.Data))
		default:
			LOGE("[proxy-msg] Message channel full, dropping data: ", msg.Header.UUID)
			BufferPool2K.Put(msg.Data[:0])
		}

	case MsgTypeDisconnect:
		connInfo, exists := app.connMgr.Get(msg.Header.UUID)
		if !exists {
			LOGE("[proxy-msg] Connection not found: ", msg.Header.UUID)
			return
		}
		connInfo.Status = StatusDisconnect
	default:
		LOGE("[proxy-msg] Unknown message type: ", msg.Header.MsgType)
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
