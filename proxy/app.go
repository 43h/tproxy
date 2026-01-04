package main

import (
	"context"
	. "tproxy/common"
)

type ProxyApp struct {
	config   *Config
	proxy    *ProxyServer
	connMgr  *ConnectionManager
	msgBus   *MessageBus
	upstream *UpstreamClient
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewProxyApp(config *Config) *ProxyApp {
	connMgr := NewConnectionManager()
	msgBus := NewMessageBus(10000)
	ctx, cancel := context.WithCancel(context.Background())
	upstream := NewUpstreamClient(config.Server, connMgr, msgBus)
	proxy := NewProxyServer(config.Listen, connMgr, msgBus, upstream)

	return &ProxyApp{
		config:   config,
		connMgr:  connMgr,
		msgBus:   msgBus,
		proxy:    proxy,
		upstream: upstream,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (app *ProxyApp) Run() error {
	LOGI("=== TProxy Application Starting ===")
	LOGI("Listen Address: ", app.config.Listen)
	LOGI("Upstream Server: ", app.config.Server)

	go app.handleMessages(app.ctx)

	go app.upstream.Start(app.ctx)

	return app.proxy.Start(app.ctx)
}

func (app *ProxyApp) handleMessages(ctx context.Context) {
	msgChan := app.msgBus.GetMessageChannel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-msgChan:
			switch msg.Header.Source {
			case MsgSourceLocal:
				app.handleLocalMessage(msg)
			case MsgSourceRelay:
				app.handleRelayMessage(msg)
			default:
				LOGE("Unknown message source: ", msg.Header.Source)
			}
		}
	}
}

func (app *ProxyApp) handleLocalMessage(msg Message) {
	switch msg.Header.MsgType {
	case MsgTypeConnect:
		msg.Header.Source = MsgSourceProxy
		if err := app.upstream.SendMessage(&msg); err != nil {
			app.connMgr.UpdateStatus(msg.Header.UUID, StatusDisconnect)
			LOGE("[local-msg] Failed to send connect message: ", err)
		}

	case MsgTypeDisconnect:
		connInfo, exists := app.connMgr.Get(msg.Header.UUID)
		if !exists {
			LOGE("[upstream-msg] Connection not found: ", msg.Header.UUID)
			return
		}
		if connInfo.Status == StatusConnected {
			msg.Header.Source = MsgSourceProxy
			if err := app.upstream.SendMessage(&msg); err != nil {
				LOGE("[local-msg] Failed to send disconnect message: ", err)
			}
		}
		app.connMgr.Delete(msg.Header.UUID)

	case MsgTypeData:
		msg.Header.Source = MsgSourceProxy
		if err := app.upstream.SendMessage(&msg); err != nil {
			app.connMgr.UpdateStatus(msg.Header.UUID, StatusDisconnect)
			LOGE("[local-msg] Failed to send data message: ", err)
		}

	default:
		LOGE("[local-msg] Unknown message type: ", msg.Header.MsgType)
	}
}

func (app *ProxyApp) handleRelayMessage(msg Message) {
	switch msg.Header.MsgType {
	case MsgTypeData:
		conn, exists := app.connMgr.Get(msg.Header.UUID)
		if !exists {
			LOGE("[relay-msg] Connection not found: ", msg.Header.UUID)
			return
		}

		n, err := conn.Conn.Write(msg.Data)
		if err != nil {
			LOGE("[relay-msg] Write failed: ", msg.Header.UUID, " ", err)
			app.msgBus.AddDisconnectMsg(msg.Header.UUID)
		} else {
			LOGD("[relay-msg] client<---proxy ", msg.Header.UUID, " sent: ", n)
		}
		BufferPool2K.Put(msg.Data[:0])
		msg.Data = nil

	case MsgTypeDisconnect:
		LOGD("[relay-msg] Disconnect: ", msg.Header.UUID)
		conn, exists := app.connMgr.Get(msg.Header.UUID)
		if exists && conn.Conn != nil {
			conn.Status = StatusDisconnect
		}

	default:
		LOGE("[relay-msg] Unknown message type: ", msg.Header.MsgType)
	}
}

func (app *ProxyApp) Shutdown() {
	LOGI("=== TProxy Application Shutting Down ===")

	if app.cancel != nil {
		app.cancel()
	}

	if app.upstream != nil {
		app.upstream.Close()
	}

	if app.proxy != nil {
		app.proxy.Close()
	}

	app.connMgr.ForEach(func(uuid string, info *ConnInfo) {
		app.connMgr.Delete(uuid)
	})

	LOGI("=== Shutdown Complete ===")
}
