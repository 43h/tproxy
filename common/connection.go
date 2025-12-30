package common

import (
	"net"
	"sync"
	"time"
)

// 连接状态
const (
	StatusNull = iota
	StatusListen
	StatusConnected
	StatusDisconnect
	StatusDisconnected
)

type ConnInfo struct {
	UUID       string
	IPStr      string //目的地址
	Conn       net.Conn
	Status     int
	MsgChannel chan Message
	Timestamp  int64
}

type ConnectionManager struct {
	mu          sync.RWMutex
	connections map[string]*ConnInfo
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*ConnInfo),
	}
}

func (cm *ConnectionManager) Add(uuid string, info *ConnInfo) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if oldConn, exists := cm.connections[uuid]; exists {
		LOGI("Connection UUID conflict, cleaning old connection: ", uuid)
		if oldConn.Conn != nil {
			if err := oldConn.Conn.Close(); err != nil {
				LOGE("Connection closed err: ", err)
			}
		}
		if oldConn.MsgChannel != nil {
			close(oldConn.MsgChannel)
		}
	}

	info.Timestamp = time.Now().Unix()
	cm.connections[uuid] = info
}

func (cm *ConnectionManager) Get(uuid string) (*ConnInfo, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	conn, exists := cm.connections[uuid]
	return conn, exists
}

func (cm *ConnectionManager) Update(uuid string, fn func(*ConnInfo)) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	conn, exists := cm.connections[uuid]
	if exists {
		fn(conn)
		conn.Timestamp = time.Now().Unix()
		return true
	}
	return false
}

func (cm *ConnectionManager) Delete(uuid string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if conn, exists := cm.connections[uuid]; exists {
		if conn.Conn != nil {
			if err := conn.Conn.Close(); err != nil {
				LOGE("Connection close error:", err)
			}
			conn.Conn = nil
		}
		if conn.MsgChannel != nil {
			close(conn.MsgChannel)
			conn.MsgChannel = nil
		}
		delete(cm.connections, uuid)
	}
}

func (cm *ConnectionManager) ForEach(fn func(uuid string, info *ConnInfo)) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	for uuid, info := range cm.connections {
		fn(uuid, info)
	}
}

func (cm *ConnectionManager) Count() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.connections)
}

func (cm *ConnectionManager) CleanTimeout(timeout int64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	now := time.Now().Unix()
	for uuid, info := range cm.connections {
		if now-info.Timestamp > timeout {
			if info.Conn != nil {
				if err := info.Conn.Close(); err != nil {
					LOGE("Connection close error:", err)
				}
			}
			if info.MsgChannel != nil {
				close(info.MsgChannel)
			}
			delete(cm.connections, uuid)
			LOGI("Connection timeout cleaned: ", uuid)
		}
	}
}
