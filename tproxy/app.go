//go:build linux

package main

import (
	"context"
	"errors"
	. "tproxy/common"
)

type TProxyApp struct {
	config   *Config
	connMgr  *ConnectionManager
	msgBus   *MessageBus
	tproxy   *TProxyServer
	upstream *UpstreamClient
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewTProxyApp(config *Config) *TProxyApp {
	connMgr := NewConnectionManager()
	msgBus := NewMessageBus(10000)
	ctx, cancel := context.WithCancel(context.Background())

	return &TProxyApp{
		config:   config,
		connMgr:  connMgr,
		msgBus:   msgBus,
		tproxy:   NewTProxyServer(config.Listen, connMgr, msgBus),
		upstream: NewUpstreamClient(config.Server, connMgr, msgBus),
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (app *TProxyApp) Run() error {
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
	return app.tproxy.Start(app.ctx)
}

// handleEvents 处理事件循环
func (app *TProxyApp) handleMessages(ctx context.Context) {
	msgChan := app.msgBus.GetMessageChannel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-msgChan:
			switch msg.Source {
			case MsgSourceLocal:
				app.handleLocalMessage(msg)
			case MsgSourceRelay:
				app.handleUpstreamMessage(msg)
			default:
				LOGE("Unknown message source: ", msg.Source)
			}
		}
	}
}

// handleLocalMessage 处理本地事件（TPROXY服务器产生的事件）
func (app *TProxyApp) handleLocalMessage(msg Message) {
	defer func() {
		// 消息处理完成后释放buffer
		if msg.ReleaseFunc != nil {
			msg.ReleaseFunc()
		}
	}()

	switch msg.MsgType {
	case MsgTypeConnect:
		// TPROXY接收到新连接，转发连接请求到上游
		LOGD("[local-msg] Connect: ", msg.UUID, " ", msg.IPStr)
		if err := app.upstream.SendMessage(&msg); err != nil {
			LOGE("[local-msg] Failed to send connect message: ", err)
		}

	case MsgTypeDisconnect:
		// 本地连接断开，通知上游
		LOGD("[local-msg] Disconnect: ", msg.UUID)
		if err := app.upstream.SendMessage(&msg); err != nil {
			LOGE("[local-msg] Failed to send disconnect message: ", err)
		}
		app.connMgr.Delete(msg.UUID)

	case MsgTypeData:
		// 本地数据，转发到上游
		LOGD("[local-msg] Data: ", msg.UUID, " length: ", msg.Length)
		if err := app.upstream.SendMessage(&msg); err != nil {
			LOGE("[local-msg] Failed to send data message: ", err)
		}

	default:
		LOGE("[local-msg] Unknown message type: ", msg.MsgType)
	}
}
// handleUpstreamMessage 处理上游事件（来自relay服务器的消息）
func (app *TProxyApp) handleUpstreamMessage(msg Message) {
	defer func() {
		// 消息处理完成后释放buffer
		if msg.ReleaseFunc != nil {
			msg.ReleaseFunc()
		}
	}()

	switch msg.MsgType {
	case MsgTypeData:
		// 上游数据，转发到本地连接
		conn, exists := app.connMgr.Get(msg.UUID)
		if !exists {
			LOGE("[upstream-msg] Connection not found: ", msg.UUID)
			return
		}

		n, err := conn.Conn.Write(msg.Data)
		if err != nil {
			LOGE("[upstream-msg] Write failed: ", msg.UUID, " ", err)
			app.msgBus.EmitDisconnect(msg.UUID)
		} else {
			LOGD("[upstream-msg] Data written: ", msg.UUID, " ", n, " bytes")
		}

	case MsgTypeDisconnect:
		// 上游断开连接
		LOGD("[upstream-msg] Disconnect: ", msg.UUID)
		conn, exists := app.connMgr.Get(msg.UUID)
		if exists && conn.Conn != nil {
			conn.Conn.Close()
		}
		app.connMgr.Delete(msg.UUID)

	default:
		LOGE("[upstream-msg] Unknown message type: ", msg.MsgType)
	}
}

// Shutdown 优雅关闭
func (app *TProxyApp) Shutdown() {
	LOGI("=== TProxy Application Shutting Down ===")

	// 取消context，停止所有goroutine
	if app.cancel != nil {
		app.cancel()
	}

	// 关闭上游连接
	if app.upstream != nil {
		app.upstream.Close()
	}

	// 关闭TPROXY服务器
	if app.tproxy != nil {
		app.tproxy.Close()
	}

	// 清理所有连接
	app.connMgr.ForEach(func(uuid string, info *ConnInfo) {
		app.connMgr.Delete(uuid)
	})

	LOGI("=== Shutdown Complete ===")
}