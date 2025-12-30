package common

import "net"

type MessageType int

type MessageHeader struct {
	Source  int
	MsgType MessageType
	UUID    string
	IPStr   string
	Len     int
}

// Message 统一消息协议
type Message struct {
	Header MessageHeader
	Data   []byte
}

// 消息来源
const (
	MsgSourceLocal = iota + 1
	MsgSourceRelay
	MsgSourceTproxy
)

// 消息类型
const (
	MsgTypeUnknown MessageType = iota
	MsgTypeConnect
	MsgTypeDisconnect
	MsgTypeData
	MsgTypeMax
)

// MessageBus 消息总线（事件驱动架构）
type MessageBus struct {
	msgChan chan Message
}

// NewMessageBus 创建消息总线
func NewMessageBus(chanSize int) *MessageBus {
	return &MessageBus{
		msgChan: make(chan Message, chanSize),
	}
}

func (mb *MessageBus) GetMessageChannel() <-chan Message {
	return mb.msgChan
}

func (mb *MessageBus) SendMessage(msg Message) {
	mb.msgChan <- msg
}

func (mb *MessageBus) AddConnectMsg(uuid string, ipStr string, conn net.Conn) {
	msg := Message{
		Header: MessageHeader{
			Source:  MsgSourceLocal,
			MsgType: MsgTypeConnect,
			UUID:    uuid,
			IPStr:   ipStr,
			Len:     0,
		}}
	mb.msgChan <- msg
}

func (mb *MessageBus) AddDisconnectMsg(uuid string) {
	msg := Message{Header: MessageHeader{
		Source:  MsgSourceLocal,
		MsgType: MsgTypeDisconnect,
		UUID:    uuid,
		Len:     0,
	}}
	mb.msgChan <- msg
}

func (mb *MessageBus) AddDataMsg(uuid string, data []byte, length int) {
	msg := Message{Header: MessageHeader{
		Source:  MsgSourceLocal,
		MsgType: MsgTypeData,
		UUID:    uuid,
		Len:     length,
	},
		Data: data[:length],
	}
	mb.msgChan <- msg
}