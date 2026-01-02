package main

import (
	"net"
	. "tproxy/common"
)

func connectToBackend(uuid string, serverAddr string, msgBus *MessageBus, connMgr *ConnectionManager) {
	LOGI("[backend] Connecting to: ", uuid, " ", serverAddr)

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		LOGE("[backend] Connect failed: ", uuid, " ", serverAddr, " ", err)
		msgBus.AddDisconnectMsg(uuid)
		return
	}

	LOGI("[backend] Connected: ", uuid, " ", serverAddr)

	connMgr.Update(uuid, func(ci *ConnInfo) {
		ci.Conn = conn
		ci.Status = StatusConnected
	})
	
	connInfo, exists := connMgr.Get(uuid)
	if !exists {
		LOGE("[backend] Connection not found after connect: ", uuid)
		if err := conn.Close(); err != nil {
			LOGE("[backend] Failed to close connection: ", uuid, " ", serverAddr, " ", err)
		}
		return
	}

	go handleBackendReceive(uuid, conn, msgBus)
	go handleBackendSend(uuid, conn, connInfo.MsgChannel)
}

func handleBackendReceive(uuid string, conn net.Conn, msgBus *MessageBus) {
	defer func() {
		msgBus.AddDisconnectMsg(uuid)
		if err := conn.Close(); err != nil {
			LOGE("[backend] Close failed: ", uuid, " ", err)
		}
		LOGI("[backend] Receive loop ended: ", uuid)
	}()

	for {
		buf := BufferPool2K.Get()
		n, err := conn.Read(buf)

		if err != nil {
			BufferPool2K.Put(buf)
			LOGE("[backend] Read failed: ", uuid, " ", err)
			return
		}

		if n > 0 {
			data := buf[:n]

			msgBus.AddDataMsg(uuid, data, n)
			LOGD("[backend] Data received: ", uuid, " ", n, " bytes")
		} else {
			BufferPool2K.Put(buf)
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
		// 总是释放buffer
		BufferPool2K.Put(msg.Data)

		if err != nil {
			LOGE("[backend] Write failed: ", uuid, " ", err)
			return
		}

		LOGD("[backend] Data sent: ", uuid, " need: ", msg.Header.Len, " sent: ", n)
	}
}
