package main

import (
	"fmt"
	"net"
	"time"
	. "tproxy/common"
)

func connectToBackend(uuid string, serverAddr string, msgBus *MessageBus, connMgr *ConnectionManager) {
	LOGI("[backend] Connecting to: ", uuid, " ", serverAddr)
	defer func() {
		msgBus.AddDisconnectMsg(uuid)
		LOGI("[backend] Receive loop ended: ", uuid)
	}()

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		LOGE("[backend] Connect failed: ", uuid, " ", serverAddr, " ", err)
		return
	}

	LOGI("[backend] Connected: ", uuid, " ", serverAddr)

	// 更新连接信息，设置 Conn 和状态
	connMgr.Update(uuid, func(ci *ConnInfo) {
		ci.Conn = conn
		ci.Status = StatusConnected
	})

	// 验证连接是否成功设置
	connInfo, exists := connMgr.Get(uuid)
	if !exists {
		LOGE("[backend] Connection not found after connect: ", uuid)
		if err := conn.Close(); err != nil {
			LOGE("[backend] Failed to close connection: ", uuid, " ", err)
		}
		return
	}

	// 启动发送协程处理 MsgChannel 中的数据
	go handleBackendSend(uuid, conn, connInfo.MsgChannel)

	for {
		if connInfo.Status == StatusDisconnect {
			return
		}

		// 设置读取超时，Windows平台需要显式超时来保持连接活性检测
		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			LOGE("[backend] Set read deadline failed: ", uuid, " ", err)
			return
		}

		// 获取初始buffer，在循环中复用，仅在读到数据后才重新获取
		buf := BufferPool2K.Get()
		LOGD("[backend] backend--->server ", uuid, " waiting for data...")
		n, err := conn.Read(buf)

		// 错误处理
		if err != nil {
			BufferPool2K.Put(buf[:0])
			// 检查是否为超时错误
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				LOGD("[backend] Read timeout: ", uuid)
				// 超时时继续使用当前buffer，无需重新分配
				continue
			}

			// 其他错误，记录并退出
			LOGE("[backend] Read error: ", uuid, " ", err)
			return
		}

		// 读取到数据
		if n > 0 {
			// 记录数据预览（调试用）
			if n > 16 {
				LOGD("[backend] Data preview: ", uuid, " first 8 bytes: ", fmt.Sprintf("%x", buf[:8]), " last 8 bytes: ", fmt.Sprintf("%x", buf[n-8:n]))
			} else if n > 8 {
				LOGD("[backend] Data preview: ", uuid, " first 8 bytes: ", fmt.Sprintf("%x", buf[:8]), " remaining: ", fmt.Sprintf("%x", buf[8:n]))
			}

			// 将当前buffer传递给消息总线（buffer的所有权转移）
			msgBus.AddDataMsg(uuid, buf[:n], n)
			LOGD("[backend] backend<---server ", uuid, " recv: ", n)
		} else {
			// 获取新的buffer用于下次读取
			buf = BufferPool2K.Get()
		}
	}
}

func handleBackendSend(uuid string, conn net.Conn, msgChannel chan Message) {
	defer func() {
		if err := conn.Close(); err != nil {
			LOGE("[backend] Close failed: ", uuid, " ", err)
		}
		LOGI("[backend] Send loop ended: ", uuid)
	}()

	for msg := range msgChannel {
		if msg.Header.MsgType != MsgTypeData {
			continue
		}

		n, err := conn.Write(msg.Data)
		if n > 16 {
			LOGD("[backend] Data preview: ", uuid, " first 8 bytes: ", fmt.Sprintf("%x", msg.Data[:8]), " last 8 bytes: ", fmt.Sprintf("%x", msg.Data[n-8:n]))
		} else if n > 8 {
			LOGD("[backend] Data preview: ", uuid, " first 8 bytes: ", fmt.Sprintf("%x", msg.Data[:8]), " remaining: ", fmt.Sprintf("%x", msg.Data[8:n]))
		}
		BufferPool2K.Put(msg.Data[:0])

		if err != nil {
			LOGE("[backend] Write failed: ", uuid, " ", err)
			return
		}

		LOGD("[backend] backend--->server ", uuid, " sent: ", n)
	}
}
