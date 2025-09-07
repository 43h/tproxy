//go:build linux

package main

import (
	. "common"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"
)

type ProxyClient struct {
	conn       net.Conn
	status     int
	serverAddr string
}

var proxyClient ProxyClient

func initProxy() {
	proxyClient.serverAddr = ConfigParam.Server
	proxyClient.status = Disconnected
}

func runProxy() {

}

func connectToUpstream() bool {
	LOGI("[proxy] init")
	var err error
	proxyClient.conn, err = net.Dial("tcp", proxyClient.serverAddr)
	if err != nil {
		LOGE("[proxy] connect to ", proxyClient.serverAddr, ", fail, ", err)
		return false
	} else {
		LOGI("[proxy] connect to ", proxyClient.serverAddr, ", success")
		proxyClient.status = StatusConnected
		return true
	}
}

func closeProxy() {
	if proxyClient.conn != nil {
		return
	}

	err := proxyClient.conn.Close()
	if err != nil {
		LOGE("[proxy] close, fail, ", err)
	} else {
		LOGI("[proxy] close, success")
	}
	proxyClient.status = Disconnected
}

func startClient() {
	go handleEvents()
	for {
		for status == Disconnected {
			if connectToUpstream() == true {
				break
			} else {
				time.Sleep(5 * time.Second)
			}
		}
		rcvFromUpstream()
	}
}

func sndToUpstream(conn net.Conn, data []byte) (n int, err error) {
	if conn == nil {
		return 0, errors.New("downstream--->upstream, write, conn is nil")
	}

	lenData := len(data)
	lenBuf := []byte{byte(lenData >> 8), byte(lenData & 0xff)}
	_, err = conn.Write(lenBuf) //发送长度
	if err == nil {
		length, err := conn.Write(data) //发送数据
		if err != nil {
			closeClient()
			LOGE("downstream--->upstream, write, fail, ", err)
			return 0, err
		} else {
			LOGD("downstream--->upstream, write data: ", length, " need: ", lenData)
			return length, nil
		}
	} else {
		return 0, err
	}
}

func rcvFromUpstream() {
	LOGI("[client] start to rcv data")
	for {
		lengthBuf := make([]byte, 2) //读取长度部分
		length, err := io.ReadFull(conn, lengthBuf)
		if err != nil {
			closeClient()
			LOGE("[client] downstream<---upstream, read length, fail, ", err)
			return
		} else {
			LOGD("[client] downstream<---upstream, read length, success, length: ", length)
		}

		length = int(lengthBuf[0])<<8 + int(lengthBuf[1])
		dataBuf := make([]byte, length)
		lenData, err := io.ReadFull(conn, dataBuf)
		if err != nil {
			LOGE("[client] downstream<---upstream, read data, fail, ", err)
			closeClient()
			return
		} else {
			LOGD("[client] downstream<---upstream, read data, success, need: ", length, ", read: ", lenData)
		}

		var msg Message
		err = json.Unmarshal(dataBuf, &msg)
		if err == nil {
			messageChannel <- msg
		} else {
			LOGE("[client] downstream unmarshalling message, fail, ", err)
		}
	}
}
