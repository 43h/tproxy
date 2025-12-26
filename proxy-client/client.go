//go:build linux

package main

import (
	. "tproxy/common"
	"encoding/json"
)

var messageChannel = make(chan Message, 10000)

var connections = make(map[string]ConnectionInfo)

func handleEvents() {
	LOGI("[client]  start to handle events")
	for {
		select {
		case message := <-messageChannel:
			switch message.Source {
			case MsgSourceLocal:
				handleEventLocal(message)
			case MsgSourceUpstream:
				handleEventFromUpstream(message)
			default:
				LOGE("[client] Unknown message source:", message.Source)
			}
		}
	}
}

func handleEventLocal(msg Message) {
	switch msg.MessageType {
	case MessageTypeConnect:
		connClient := connections[msg.UUID]
		if proxyClient.conn == nil { //若与上游断开链接，则关闭与客户端链接
			connClient.Status = StatusDisconnect
			return
		}

		msg.Source = MsgSourceDownstream
		data, err := json.Marshal(msg)
		if err != nil {
			LOGE("[client]", msg.UUID, " marshaling message, fail, ", err)
			return
		}
		_, err = sndToUpstream(proxyClient.conn, data)
		if err != nil {
			LOGE("[client]", msg.UUID, " downstream--->upstream, write, event-connect, fail, ", err)
			return
		} else {
			LOGD("[client]", msg.UUID, " downstream--->upstream, write, event-connect, success")
		}

	case MessageTypeDisconnect: //与客户端之间的连接断开
		connClient := connections[msg.UUID]
		if connClient.Status == StatusConnected { //主动断开，上报状态
			msg.Source = MsgSourceUpstream
			data, err := json.Marshal(msg)
			if err != nil {
				LOGE(msg.UUID, " marshaling message, fail, ", err)
				return
			}
			_, err = sndToUpstream(proxyClient.conn, data)
			if err != nil {
				LOGE("[client]", msg.UUID, " downstream--->upstream, write, event-disconnect, fail, ", err)
				return
			} else {
				LOGD("[client]", msg.UUID, " downstream--->upstream, write, event-disconnect, success")
			}
		} //else if connClient.Status == Disconnect 被动断开，无需上报

		connClient.Status = StatusDisconnected
		delete(connections, msg.UUID)

	case MessageTypeData: //客户端消息转发到上游
		msg.Source = MsgSourceUpstream
		data, err := json.Marshal(msg)
		if err != nil {
			LOGE("[client]", msg.UUID, " fail to marshaling message ", err)
			return
		}
		_, err = sndToUpstream(proxyClient.conn, data)
		if err != nil {
			LOGE("[client]", msg.UUID, " downstream--->upstream, write, event-data, fail, ", err)
			return
		} else {
			LOGD("[client]", msg.UUID, " downstream--->upstream, write, event-data, success")
		}
	}
}

func handleEventFromUpstream(msg Message) {
	connection, exists := connections[msg.UUID]
	if !exists {
		LOGE("[client]", msg.UUID, " connection not found")
		return
	}

	if msg.MessageType == MessageTypeData { //收到真实服务器数据
		length, err := connection.Conn.Write(msg.Data) //数据转发给客户端
		if err != nil {
			LOGE("[client]", msg.UUID, " client<---downstream, write, fail, ", err)
			return
		} else {
			LOGD("[client]", msg.UUID, " client<---downstream, write, success, need: ", msg.Length, " snd: ", length)
		}
	} else if msg.MessageType == MessageTypeDisconnect { //与真实服务器断开链接
		if connection.Status == StatusConnected {
			connection.Status = StatusDisconnect //修改状态后续断开
		}
	}
}

func AddEventConnect(uuid string, ipStr string) {
	message := Message{
		Source:      MsgSourceLocal,
		MessageType: MessageTypeConnect,
		UUID:        uuid,
		IPStr:       ipStr,
		Length:      0,
		Data:        nil,
	}
	messageChannel <- message
}

func AddEventDisconnect(uuid string) {
	message := Message{
		Source:      MsgSourceLocal,
		MessageType: MessageTypeDisconnect,
		UUID:        uuid,
		IPStr:       "",
		Length:      0,
		Data:        nil,
	}
	messageChannel <- message
}

func AddEventMsg(uuid string, buf []byte, len int) {
	message := Message{
		Source:      MsgSourceLocal,
		MessageType: MessageTypeData,
		UUID:        uuid,
		IPStr:       "",
		Length:      len,
		Data:        buf[:len],
	}
	messageChannel <- message
}
