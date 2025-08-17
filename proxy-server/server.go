package main

import (
	. "common"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

var listener net.Listener
var conn net.Conn
var status int

type ConnectionInfo struct {
	IPStr      string
	Conn       net.Conn
	Status     int
	MsgChannel chan Message //cache upstream message
	Timestamp  int64
}

const (
	Connected = iota + 1
	Disconnected
)

var messageChannel = make(chan Message, 10000)

var connectionsLock sync.RWMutex
var connections = make(map[string]ConnectionInfo)

func initServerTls() bool {
	LOGI("upstream start with TLS")
	cert, err := tls.LoadX509KeyPair("test.pem", "test.key")
	if err != nil {
		LOGE("upstream fail to load certificate, ", err)
		return false
	}

	config := &tls.Config{Certificates: []tls.Certificate{cert}}
	tmpListener, err := tls.Listen("tcp", ConfigParam.Listen, config)
	if err != nil {
		LOGE("upstream fail to start TLS listener, ", err)
		return false
	}
	listener = tmpListener
	return true
}

func initServer() bool {
	LOGI("upstream start without TLS")
	tmpListener, err := net.Listen("tcp", ConfigParam.Listen)
	if err != nil {
		LOGE("upstream fail to start listener, ", err)
		return false
	}
	listener = tmpListener
	return true
}

func closeServer() {
	if listener != nil {
		err := listener.Close()
		if err != nil {
			LOGE("upstream close listener, fail, ", err)
		} else {
			LOGI("upstream close listener, success")
		}
	} else {
		LOGI("upstream close listener(SKIP)")
	}
}

func startServer() {
	LOGI("upstream started, Listening on ", ConfigParam.Listen)
	go handleEvents()
	for {
		tmpConn, err := listener.Accept()
		if err != nil {
			LOGE("upstream accepting, fail ", err)
			continue
		}

		if conn != nil || status == Connected {
			fmt.Println("Only one client is allowed to connect at a time")
			tmpConn.Close()
			continue
		} else {
			conn = tmpConn
			status = Connected
		}

		go rcvServer()
	}
}

func rcvServer() {
	LOGI("downstream connect to upstream")

	for {
		lengthBuf := make([]byte, 2)
		lenData, err := io.ReadFull(conn, lengthBuf)
		if err != nil {
			LOGE("downstream--->upstream, read length, fail, ", err)
			conn = nil
			status = Disconnected
			return
		} else {
			LOGD("downstream--->upstream, read length, success, length: ", lenData)
		}

		length := int(lengthBuf[0])<<8 + int(lengthBuf[1])
		dataBuf := make([]byte, length)
		rcvLength, err := io.ReadFull(conn, dataBuf)
		if err != nil {
			LOGE("downstream--->upstream, read data, fail, ", err)
			return
		} else {
			LOGD("downstream--->upstream, read date, success, need: ", length, " read: ", rcvLength, "total: ", rcvLength+4)
		}

		var msg Message
		err = json.Unmarshal(dataBuf, &msg)
		if err != nil {
			LOGE("upstream unmarshalling message, fail, ", err)
			return
		} else {
			messageChannel <- msg
		}
	}
}

func handleEvents() {
	for {
		select {
		case message := <-messageChannel:
			switch message.MessageClass {
			case MessageClassLocal:
				handleEventLocal(message)
			case MessageClassUpstream:
				handleEventUpstream(message)
			}
		}
	}
}

func handleEventLocal(msg Message) {
	switch msg.MessageType {
	case MessageTypeConnect: //proxy connect to remote server
		connectionsLock.RLock()
		connection, exists := connections[msg.UUID]
		connectionsLock.RUnlock()
		if exists {
			connection.Timestamp = time.Now().Unix()
			go handleClientRcv(connection.Conn, msg.UUID)
			go handleClientSnd(connection.Conn, connection.MsgChannel)
		} else {
			LOGE(msg.UUID, " connection not found")
		}
	case MessageTypeDisconnect:
		connectionsLock.Lock()
		delete(connections, msg.UUID)
		connectionsLock.Unlock()
	case MessageTypeData:
		msg.MessageClass = MessageClassDownstream
		data, err := json.Marshal(msg)
		if err != nil {
			LOGE(msg.UUID, " marshaling message, fail, ", err)
			return
		}
		length, err := sndToDownstream(conn, data)
		if err != nil {
			LOGE(msg.UUID, " downstream<---upstream, write, event-data, fail, ", err)
			return
		} else {
			LOGD(msg.UUID, " downstream<---upstream, write, event-data, success, length: ", length)
		}
	}
}

func handleEventUpstream(msg Message) {
	switch msg.MessageType {
	case MessageTypeConnect: //connect to remote server
		connection := ConnectionInfo{
			IPStr:      msg.IPStr,
			Conn:       nil,
			Status:     Disconnected,
			MsgChannel: make(chan Message, 1000),
			Timestamp:  time.Now().Unix(),
		}
		connectionsLock.Lock()
		connections[msg.UUID] = connection
		connectionsLock.Unlock()
		initClient(msg.IPStr, msg.UUID)
	case MessageTypeData:
		connectionsLock.RLock()
		connection, exists := connections[msg.UUID]
		connectionsLock.RUnlock()
		if exists {
			connection.MsgChannel <- msg
		} else {
			LOGE(msg.UUID, " connection not found")
		}
	default:
		LOGE("Unknown message type")
	}
}

func AddEventConnect(uuid string, conn net.Conn) {
	connectionsLock.RLock()
	connection, exists := connections[uuid]
	connectionsLock.RUnlock()
	if exists {
		connection.Conn = conn
		connection.Status = Connected
		connectionsLock.Lock()
		connections[uuid] = connection
		connectionsLock.Unlock()
	} else {
		LOGE(uuid, " fail to find the connection")
		return
	}

	message := Message{
		MessageClass: MessageClassLocal,
		MessageType:  MessageTypeConnect,
		UUID:         uuid,
		IPStr:        "",
		Length:       0,
		Data:         nil,
	}
	messageChannel <- message
}

func AddEventDisconnect(uuid string) {
	message := Message{
		MessageClass: MessageClassLocal,
		MessageType:  MessageTypeDisconnect,
		UUID:         uuid,
		IPStr:        "",
		Length:       0,
		Data:         nil,
	}
	messageChannel <- message
}

func AddEventMsg(uuid string, buf []byte, len int) {
	message := Message{
		MessageClass: MessageClassLocal,
		MessageType:  MessageTypeData,
		UUID:         uuid,
		IPStr:        "",
		Length:       len,
		Data:         buf[:len],
	}
	messageChannel <- message
}

func sndToDownstream(conn net.Conn, data []byte) (n int, err error) {
	length := len(data)
	lenBytes := []byte{byte(length >> 8), byte(length & 0xff)}
	n, err = conn.Write(lenBytes) //发送长度
	if err == nil {
		LOGD("downstream<---upstream, write, body: ", length, "length: ", n)
		_, err := conn.Write(data) //发送数据
		if err != nil {
			return 0, err
		} else {
			return length, nil
		}
	} else {
		return 0, err
	}
}
