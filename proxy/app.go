//go:build linux

package main

import (
	"context"
	"errors"
	. "tproxy/common"
)

type ProxyApp struct {
	config   *Config
	connMgr  *ConnectionManager
	msgBus   *MessageBus
	proxy    *ProxyServer
	upstream *UpstreamClient
	bufPool  *BufferPool
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewProxyApp(config *Config) *ProxyApp {
	connMgr := NewConnectionManager()
	msgBus := NewMessageBus(10000)
	bufPool := NewBufPool(BufSize2048)
	ctx, cancel := context.WithCancel(context.Background())

	return &ProxyApp{
		config:   config,
		connMgr:  connMgr,
		msgBus:   msgBus,
		proxy:    NewProxyServer(config.Listen, connMgr, msgBus, bufPool),
		upstream: NewUpstreamClient(config.Server, connMgr, msgBus, bufPool),
		bufPool:  bufPool,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (app *ProxyApp) Run() error {
	LOGI("=== TProxy Application Starting ===")
	LOGI("Listen Address: ", app.config.Listen)
	LOGI("Upstream Server: ", app.config.Server)

	go app.handleMessages(app.ctx)

	// 启动上游连接
	go func() {
		if err := app.upstream.Start(app.ctx); err != nil && !errors.Is(err, context.Canceled) {
			LOGE("Upstream client error: ", err)
		}
	}()

	// 启动tproxy服务器 (阻塞)
	return app.proxy.Start(app.ctx)
}

// handleEvents 处理事件循环
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

// handleLocalMessage 处理本地事件（TPROXY服务器产生的事件）
func (app *ProxyApp) handleLocalMessage(msg Message) {

	switch msg.Header.MsgType {
	case MsgTypeConnect:
		// TPROXY接收到新连接，转发连接请求到上游
		LOGD("[local-msg] Connect: ", msg.Header.UUID, " ", msg.Header.IPStr)
		if err := app.upstream.SendMessage(&msg); err != nil {
			LOGE("[local-msg] Failed to send connect message: ", err)
		}

	case MsgTypeDisconnect:
		// 本地连接断开，通知上游
		LOGD("[local-msg] Disconnect: ", msg.Header.UUID)
		if err := app.upstream.SendMessage(&msg); err != nil {
			LOGE("[local-msg] Failed to send disconnect message: ", err)
		}
		app.connMgr.Delete(msg.Header.UUID)

	case MsgTypeData:
		// 本地数据，转发到上游
		LOGD("[local-msg] Data: ", msg.Header.UUID, " length: ", msg.Header.Len)
		if err := app.upstream.SendMessage(&msg); err != nil {
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
			LOGE("[upstream-msg] Connection not found: ", msg.Header.UUID)
			return
		}

		n, err := conn.Conn.Write(msg.Data)
		if err != nil {
			LOGE("[upstream-msg] Write failed: ", msg.Header.UUID, " ", err)
			app.msgBus.AddDisconnectMsg(msg.Header.UUID)
		} else {
			LOGD("[upstream-msg] Data written: ", msg.Header.UUID, " ", n, " bytes")
		}

	case MsgTypeDisconnect:
		// 上游断开连接
		LOGD("[upstream-msg] Disconnect: ", msg.Header.UUID)
		conn, exists := app.connMgr.Get(msg.Header.UUID)
		if exists && conn.Conn != nil {
			conn.Conn.Close()
		}
		app.connMgr.Delete(msg.Header.UUID)

	default:
		LOGE("[upstream-msg] Unknown message type: ", msg.Header.MsgType)
	}
}

func (app *ProxyApp) Shutdown() {
	LOGI("=== TProxy Application Shutting Down ===")

	// 取消context，停止所有goroutine
	if app.cancel != nil {
		app.cancel()
	}

	// 关闭上游连接
	if app.upstream != nil {
		app.upstream.Close()
	}

	if app.proxy != nil {
		app.proxy.Close()
	}

	// 清理所有连接
	app.connMgr.ForEach(func(uuid string, info *ConnInfo) {
		app.connMgr.Delete(uuid)
	})

	LOGI("=== Shutdown Complete ===")
}
