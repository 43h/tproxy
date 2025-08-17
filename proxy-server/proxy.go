package main

import (
	"net"
)

func initClient(serverAddr string, uuid string) {
	go connectToRealServer(serverAddr, uuid) //use goroutine to connect to remote server
}

func connectToRealServer(serverAddr string, uuid string) {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		AddEventDisconnect(uuid)
		LOGE(uuid, " proxy connect to remote-server, fail, ", serverAddr, " ", err)
	} else {
		AddEventConnect(uuid, conn)
		LOGD(uuid, " proxy connect to remote-server, success, ", serverAddr)
	}
}

func handleClientRcv(conn net.Conn, uuid string) {
	for {
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			LOGE(uuid, " proxy<---server, read, fail, ", err)
			AddEventDisconnect(uuid)
			return
		} else {
			LOGI(uuid, "proxy<---server, read, success, length:", n)
			AddEventMsg(uuid, buf[:n], n)
		}
	}
}

func handleClientSnd(conn net.Conn, messageChannel chan Message) {
	for {
		select {
		case message := <-messageChannel:
			n, err := conn.Write(message.Data)
			if err != nil {
				LOGE(message.UUID, " proxy--->server, write, fail, ", err)
				return
			} else {
				LOGD(message.UUID, " proxy--->server, write, success, need: ", message.Length, "send: ", n)
			}
		}
	}
}
