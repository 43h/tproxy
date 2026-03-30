package main

import (
	"context"
	"time"
	. "tproxy/common"
)

type RelayApp struct {
	config     *Config
	connMgr    *ConnectionManager
	msgBus     *MessageBus
	server     *RelayServer
	vpnMonitor *VpnMonitor
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewRelayApp(config *Config) *RelayApp {
	connMgr := NewConnectionManager()
	msgBus := NewMessageBus(2048)
	ctx, cancel := context.WithCancel(context.Background())
	server := NewRelayServer(config.Listen, config.Webhook, connMgr, msgBus)

	return &RelayApp{
		config:     config,
		connMgr:    connMgr,
		msgBus:     msgBus,
		server:     server,
		vpnMonitor: NewVpnMonitor(config.Webhook, config.VpnCheckInterval),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (app *RelayApp) Run() error {
	LOGI("=== Relay Server Starting ===")
	LOGI("Listen Address: ", app.config.Listen)

	go app.handleMessages(app.ctx)
	go app.vpnMonitor.Start(app.ctx)
	go app.statsMonitor(app.ctx)

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
			LOGD("[local-msg] Connection already removed: ", msg.Header.UUID)
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
			if msg.Data != nil {
				BufferPool2K.Put(msg.Data[:0])
				msg.Data = nil
			}
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
			MsgChannel: make(chan Message, 4096),
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

		// 捕获本地变量，防止并发 Delete 关闭/置 nil 通道导致 panic
		ch := conn.MsgChannel
		if ch == nil {
			LOGE("[proxy-msg] MsgChannel is nil: ", msg.Header.UUID)
			BufferPool2K.Put(msg.Data[:0])
			return
		}

		// 将数据放入 MsgChannel，由 handleBackendSend 协程处理
		// 先尝试非阻塞发送，通道满时改为阻塞+超时，提供背压而不是直接丢数据
		if !safeSendMsg(ch, msg) {
			LOGE("[proxy-msg] Message send failed (channel full or closed): ", msg.Header.UUID)
			BufferPool2K.Put(msg.Data[:0])
		} else {
			LOGD("[proxy-msg] Data queued: ", msg.Header.UUID, " len: ", len(msg.Data))
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

// safeSendMsg 安全地向通道发送消息，先尝试非阻塞，通道满时阻塞等待（最多30秒）
// 使用 recover 防护并发 Delete 关闭通道导致的 panic
func safeSendMsg(ch chan Message, msg Message) (sent bool) {
	defer func() {
		if r := recover(); r != nil {
			sent = false
		}
	}()

	// 非阻塞尝试
	select {
	case ch <- msg:
		return true
	default:
	}

	// 通道满，阻塞等待（提供背压）
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case ch <- msg:
		return true
	case <-timer.C:
		return false
	}
}

func (app *RelayApp) statsMonitor(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			LOGI("[stats] msgBus: ", app.msgBus.QueueLen(), "/", app.msgBus.QueueCap(),
				" sendChan: ", app.server.SendQueueLen(), "/", app.server.SendQueueCap())
		}
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

	// 先收集 UUID，再逐个删除，避免 ForEach(RLock) 内调用 Delete(Lock) 导致死锁
	var uuids []string
	app.connMgr.ForEach(func(uuid string, info *ConnInfo) {
		uuids = append(uuids, uuid)
	})
	for _, uuid := range uuids {
		app.connMgr.Delete(uuid)
	}

	LOGI("=== Shutdown Complete ===")
}
