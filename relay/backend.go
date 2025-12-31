//go:build windows

package main

import (
	"net"
	. "tproxy/common"
)

// connectToBackend 连接到真实服务器
func connectToBackend(uuid string, serverAddr string, eventBus *MessageBus, connMgr *ConnectionManager) {
	LOGI("[backend] Connecting to: ", uuid, " ", serverAddr)

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		LOGE("[backend] Connect failed: ", uuid, " ", serverAddr, " ", err)
		eventBus.AddDisconnectMsg(uuid)
		return
	}

	LOGI("[backend] Connected: ", uuid, " ", serverAddr)

	// 更新连接信息
	connMgr.Update(uuid, func(ci *ConnInfo) {
		ci.Conn = conn
		ci.Status = StatusConnected
	})

	// 发送连接成功事件
	eventBus.AddConnectMsg(uuid, serverAddr, nil)

	// 获取连接信息
	connInfo, exists := connMgr.Get(uuid)
	if !exists {
		LOGE("[backend] Connection not found after connect: ", uuid)
		conn.Close()
		return
	}

	// 启动发送和接收goroutine
	go handleBackendReceive(uuid, conn, eventBus)
	go handleBackendSend(uuid, conn, connInfo.MsgChannel)
}

// handleBackendReceive 接收来自真实服务器的数据（零拷贝优化）
func handleBackendReceive(uuid string, conn net.Conn, eventBus *MessageBus) {
	defer func() {
		eventBus.AddDisconnectMsg(uuid)
		conn.Close()
		LOGI("[backend] Receive loop ended: ", uuid)
	}()

	bufPool := DefaultBufferPool

	for {
		// 从池中获取buffer
		buf := bufPool.Get()
		n, err := conn.Read(buf)

		if err != nil {
			bufPool.Put(buf)
			LOGE("[backend] Read failed: ", uuid, " ", err)
			return
		}

		if n > 0 {
			// 零拷贝：直接使用buffer切片，传递释放回调
			data := buf[:n]
			releaseFunc := func() {
				bufPool.Put(buf)
			}

			// 发送数据事件（带释放回调）
			eventBus.EmitDataWithRelease(uuid, data, n, releaseFunc)
			LOGD("[backend] Data received: ", uuid, " ", n, " bytes")
		} else {
			bufPool.Put(buf)
		}
	}
}

// handleBackendSend 发送数据到真实服务器
func handleBackendSend(uuid string, conn net.Conn, msgChannel chan Message) {
	defer func() {
		conn.Close()
		LOGI("[backend] Send loop ended: ", uuid)
	}()

	for msg := range msgChannel {
		// 确保消息处理后释放buffer

		if msg.Header.MsgType != MsgTypeData {
			continue
		}

		n, err := conn.Write(msg.Data)
		if err != nil {
			LOGE("[backend] Write failed: ", uuid, " ", err)
			return
		}

		LOGD("[backend] Data sent: ", uuid, " need: ", msg.Header.Len, " sent: ", n)
	}
}