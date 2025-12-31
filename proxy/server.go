//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"syscall"
	. "tproxy/common"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

type ProxyServer struct {
	addr     string
	connMgr  *ConnectionManager
	msgBus   *MessageBus
	bufPool  *BufferPool
	listener net.Listener
	status   int
}

func NewProxyServer(addr string, connMgr *ConnectionManager, msgBus *MessageBus) *ProxyServer {
	return &ProxyServer{
		addr:    addr,
		connMgr: connMgr,
		msgBus:  msgBus,
		status:  StatusNull,
	}
}

func (ps *ProxyServer) Start(ctx context.Context) error {
	LOGI("[proxy] Initializing server on: ", ps.addr)

	tcpAddr, err := net.ResolveTCPAddr("tcp", ps.addr)
	if err != nil {
		return fmt.Errorf("resolve address failed: %w", err)
	}

	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		return fmt.Errorf("create socket failed: %w", err)
	}

	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("set SO_REUSEADDR failed: %w", err)
	}

	if err := syscall.SetsockoptInt(fd, syscall.SOL_IP, syscall.IP_TRANSPARENT, 1); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("set IP_TRANSPARENT failed: %w", err)
	}

	sockaddr := &syscall.SockaddrInet4{
		Port: tcpAddr.Port,
	}
	copy(sockaddr.Addr[:], tcpAddr.IP.To4())
	if err := syscall.Bind(fd, sockaddr); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("bind failed: %w", err)
	}

	if err := syscall.Listen(fd, syscall.SOMAXCONN); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("listen failed: %w", err)
	}

	file := os.NewFile(uintptr(fd), "proxy-listener")
	tmpListener, err := net.FileListener(file)
	file.Close()
	if err != nil {
		syscall.Close(fd)
		return fmt.Errorf("create listener failed: %w", err)
	}

	ps.listener = tmpListener
	ps.status = StatusListen

	LOGI("[proxy] Server listening on ", ps.addr)
	LOGI("[proxy] IP_TRANSPARENT enabled")

	return ps.acceptLoop(ctx)
}

func (ps *ProxyServer) acceptLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			conn, err := ps.listener.Accept()
			if err != nil {
				LOGE("[proxy] Accept failed: ", err)
				continue
			}

			go ps.handleConnection(conn)
		}
	}
}

func (ps *ProxyServer) handleConnection(conn net.Conn) {
	origDst, err := getOriginalDst(conn.(*net.TCPConn))
	if err != nil {
		LOGE("[proxy] Get original destination failed: ", err)
		conn.Close()
		return
	}

	connUUID := uuid.New().String()
	clientAddr := conn.RemoteAddr().String()

	LOGI("[proxy] New connection: ", connUUID, " from ", clientAddr, " to ", origDst)

	connInfo := &ConnInfo{
		UUID:   connUUID,
		IPStr:  origDst,
		Conn:   conn,
		Status: StatusConnected,
	}

	ps.connMgr.Add(connUUID, connInfo)

	ps.msgBus.AddConnectMsg(connUUID, origDst, nil)

	ps.readLoop(connUUID, conn)
}

// readLoop 读取连接数据循环（零拷贝优化）
func (ps *ProxyServer) readLoop(uuid string, conn net.Conn) {
	defer func() {
		// 连接关闭时发送断开事件
		ps.msgBus.AddDisconnectMsg(uuid)
		ps.connMgr.Delete(uuid)
		LOGI("[proxy] Connection closed: ", uuid)
	}()

	for {
		buf := BufferPool2K.Get()
		n, err := conn.Read(buf)

		if err != nil {
			BufferPool2K.Put(buf[:0])
			LOGD("[proxy] Read error: ", uuid, " ", err)
			return
		}

		if n > 0 {
			// 零拷贝：直接使用buffer切片，传递释放回调
			data := buf[:n]

			// 发送数据事件（带释放回调）
			ps.msgBus.AddDataMsg(uuid, data, n)
			LOGD("[proxy] Data read: ", uuid, " ", n, " bytes")
		} else {
			BufferPool2K.Put(buf)
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

func (ps *ProxyServer) Close() {
	LOGI("[proxy] Closing server")

	ps.status = StatusNull

	if ps.listener != nil {
		if err := ps.listener.Close(); err != nil {
			LOGE("[proxy] Close listener failed: ", err)
		} else {
			LOGI("[proxy] Listener closed")
		}
		ps.listener = nil
	}
}