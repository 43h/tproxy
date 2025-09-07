package common

import "net"

const (
	MsgSourceLocal      = 1
	MsgSourceUpstream   = 2
	MsgSourceDownstream = 3
)

const (
	MessageTypeConnect    = 1
	MessageTypeDisconnect = 2
	MessageTypeData       = 3
)

type Message struct {
	Source      int    `json:"source"`
	MessageType int    `json:"message_type"`
	UUID        string `json:"uuid"`
	IPStr       string `json:"ip_str"`
	Length      int    `json:"length"`
	Data        []byte `json:"data"`
}

const (
	StatusNull = iota
	StatusListen
	StatusConnected
	StatusDisconnect
	StatusDisconnected
)

type ConnectionInfo struct {
	Conn      net.Conn
	Status    int
	Timestamp int64
}
