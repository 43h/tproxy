package main

import (
	. "common"
	"errors"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
	"net"
	"strconv"
	"syscall"
	"time"
)

var listener net.Listener

func initServer() bool {
	tmpListener, err := net.Listen("tcp", ConfigParam.Listen)
	if err == nil {
		file, err := tmpListener.(*net.TCPListener).File()
		if err == nil {
			fd := int(file.Fd())
			err = syscall.SetsockoptInt(fd, syscall.SOL_IP, syscall.IP_TRANSPARENT, 1)
			if err == nil {
				listener = tmpListener
				LOGI("[server] start to listen on " + ConfigParam.Listen + "...")
				return true
			} else {
				LOGE("[server] set IP_TRANSPARENT, fail, ", err)
			}
		} else {
			LOGE("[server] get file descriptor, fail, ", err)
		}
	} else {
		LOGE("[server] listen, fail, ", err)
	}

	return false
}

func closeServer() {
	if listener != nil {
		err := listener.Close()
		if err == nil {
			LOGI("[server] close")
		} else {
			LOGE("[server] close, fail, ", err)
		}
	} else {
		LOGI("[server] close(SKIP)")
	}
}

func startServer() {
	LOGI("[server] start to accept new connection ...")

	for {
		conn, err := listener.Accept()
		if err == nil {
			go handleNewConnection(conn)
		} else {
			LOGE("[server] fail to accepting, ", err)
			continue
		}
	}
}

func handleNewConnection(conn net.Conn) {
	connUuID := uuid.New().String()
	remoteAddr := conn.RemoteAddr().(*net.TCPAddr)
	sourceIP := remoteAddr.IP.String()
	sourcePort := remoteAddr.Port

	realDstIp, err := getOriginalDst(conn.(*net.TCPConn))
	if err != nil || realDstIp == "" {
		LOGE("[server] ", connUuID, " get dst ip, fail, ", err)
		err := conn.Close()
		if err != nil {
			LOGE("[server] ", connUuID, " close new connection, fail, ", err)
		} else {
			LOGI("[server] ", connUuID, " close new connection, success")
		}
		return
	}

	LOGD("[server] ", connUuID, " new connection: ", sourceIP, ":", sourcePort, "---> ", realDstIp)
	connections[connUuID] = ConnectionInfo{
		Conn:      conn,
		Timestamp: time.Now().Unix(),
		Status:    Connected,
	}
	connInfo := connections[connUuID]
	AddEventConnect(connUuID, realDstIp)

	for { //接受客户端消息
		buf := make([]byte, 2048)
		err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err != nil {
			LOGE("[server] ", connUuID, " set read deadline, fail, ", err)
			connInfo.Status = Disconnect
		}
		n, err := conn.Read(buf)
		if err == nil {
			AddEventMsg(connUuID, buf[:n], n)
			LOGD("[server] ", connUuID, " client--->server, read, success, length: ", n)
		} else {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				LOGD("[server] ", connUuID, " read timeout")
				if connInfo.Status == Connected {
					continue
				} else if connInfo.Status == Disconnect {
					LOGI("[server] ", connUuID, " disconnect connection actively")
				}
			} else {
				LOGE("[server] ", connUuID, " client--->server, read, fail, ", err, ", disconnect connection")
			}

			err = conn.Close()
			if err != nil {
				LOGI("[server] ", connUuID, " close new connection, fail, ", err)
			} else {
				LOGD("[server] ", connUuID, " close new connection, success")
			}
			AddEventDisconnect(connUuID)
			return
		}
	}
}

func getOriginalDst(conn *net.TCPConn) (string, error) {
	file, err := conn.File()
	if err != nil {
		return "", err
	}
	fd := file.Fd()

	sa, err := unix.Getsockname(int(fd))
	if err != nil {
		return "", err
	} else {
		switch addr := sa.(type) {
		case *unix.SockaddrInet4:
			ip := net.IP(addr.Addr[:]).String()
			port := addr.Port
			return ip + ":" + strconv.Itoa(port), nil
		//case *unix.SockaddrInet6:  //Todo: support IPv6
		//	ip := net.IP(addr.Addr[:]).String()
		//	port := addr.Port
		//	fmt.Printf("IPv6 Address: %s, Port: %d\n", ip, port)

		//case *unix.SockaddrUnix:
		//	fmt.Printf("Unix Socket Path: %s\n", addr.Name)

		default:
		}
	}
	return "", errors.New("unknown address type")
}
