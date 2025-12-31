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

type Message struct {
	Header MessageHeader
	Offset int
	Data   []byte
}

const (
	MsgSourceLocal = iota + 1
	MsgSourceRelay
	MsgSourceTproxy
)

const (
	MsgTypeUnknown MessageType = iota
	MsgTypeConnect
	MsgTypeDisconnect
	MsgTypeData
	MsgTypeMax
)

type MessageBus struct {
	msgChan chan Message
}

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