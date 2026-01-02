package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"syscall"
	"time"
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
	upstream *UpstreamClient
	status   int
}

func NewProxyServer(addr string, connMgr *ConnectionManager, msgBus *MessageBus, upstream *UpstreamClient) *ProxyServer {
	return &ProxyServer{
		addr:     addr,
		connMgr:  connMgr,
		msgBus:   msgBus,
		upstream: upstream,
		status:   StatusNull,
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
	// 检查上游连接状态
	if ps.upstream == nil || !ps.upstream.IsConnected() {
		LOGE("[proxy] Rejecting connection: upstream not connected")
		conn.Close()
		return
	}

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

	ps.readLoop(connUUID, connInfo)
}

func (ps *ProxyServer) readLoop(uuid string, connInfo *ConnInfo) {
	defer func() {
		ps.msgBus.AddDisconnectMsg(uuid)
		LOGI("[proxy] Connection closed: ", uuid)
	}()

	for {
		if connInfo.Status == StatusDisconnect {
			LOGI("[proxy] close read routine: ", uuid)
			return
		}

		if err := connInfo.Conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			LOGI("[proxy] Set read deadline failed: ", err)
		}

		buf := BufferPool2K.Get()
		n, err := connInfo.Conn.Read(buf)

		if err != nil {
			BufferPool2K.Put(buf[:0])

			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}

			LOGD("[proxy] Read error: ", uuid, " ", err)
			return
		}

		if n > 0 {
			data := buf[:n]

			ps.msgBus.AddDataMsg(uuid, data, n)
			LOGD("[proxy] Data read: ", uuid, " ", n, " bytes")
		} else {
			BufferPool2K.Put(buf)
		}
	}
}

func getOriginalDst(conn *net.TCPConn) (string, error) {
	// 使用SyscallConn获取文件描述符，避免File()导致连接进入阻塞模式
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return "", err
	}

	var addr string
	var sockErr error

	err = rawConn.Control(func(fd uintptr) {
		sa, err := unix.Getsockname(int(fd))
		if err != nil {
			sockErr = err
			return
		}

		switch sockAddr := sa.(type) {
		case *unix.SockaddrInet4:
			ip := net.IP(sockAddr.Addr[:]).String()
			port := sockAddr.Port
			addr = ip + ":" + strconv.Itoa(port)
		//case *unix.SockaddrInet6:  //Todo: support IPv6
		//	ip := net.IP(sockAddr.Addr[:]).String()
		//	port := sockAddr.Port
		//	addr = ip + ":" + strconv.Itoa(port)
		default:
			sockErr = errors.New("unknown address type")
		}
	})

	if err != nil {
		return "", err
	}
	if sockErr != nil {
		return "", sockErr
	}
	if addr == "" {
		return "", errors.New("failed to get original destination")
	}

	return addr, nil
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
