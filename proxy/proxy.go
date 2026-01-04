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

	"golang.org/x/sys/unix"
)

type ProxyServer struct {
	addr     string
	connMgr  *ConnectionManager
	msgBus   *MessageBus
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

			go ps.handleConnection(ctx, conn)
		}
	}
}

func (ps *ProxyServer) handleConnection(ctx context.Context, conn net.Conn) {
	if ps.upstream == nil || !ps.upstream.IsConnected() {
		LOGE("[proxy] Rejecting connection: upstream not connected")
		if err := conn.Close(); err != nil {
			LOGE("[proxy] Failed to close connection")
		}
		return
	}

	origDst, err := getOriginalDst(conn.(*net.TCPConn))
	if err != nil {
		LOGE("[proxy] Get original destination failed: ", err)
		if err := conn.Close(); err != nil {
			LOGE("[proxy] Failed to close connection")
		}
		return
	}

	clientAddr := conn.RemoteAddr().String()
	connUUID := clientAddr + "->" + origDst + "-" + strconv.FormatInt(time.Now().Unix(), 10)

	LOGI("[proxy] New connection: ", connUUID)

	connInfo := &ConnInfo{
		UUID:   connUUID,
		IPStr:  origDst,
		Conn:   conn,
		Status: StatusConnected,
	}

	ps.connMgr.Add(connUUID, connInfo)

	ps.msgBus.AddConnectMsg(connUUID, origDst)

	ps.readLoop(ctx, connInfo)
}

func (ps *ProxyServer) readLoop(ctx context.Context, connInfo *ConnInfo) {
	uuid := connInfo.UUID
	defer func() {
		ps.msgBus.AddDisconnectMsg(uuid)
		LOGI("[proxy] Connection closed: ", uuid)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if connInfo.Status == StatusDisconnect {
				LOGI("[proxy] close read routine: ", uuid)
				return
			}

			if err := connInfo.Conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				LOGE("[proxy] Set read deadline failed: ", err)
			}

			buf := BufferPool2K.Get()
			n, err := connInfo.Conn.Read(buf)

			if err != nil {
				BufferPool2K.Put(buf[:0])

				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					LOGD("[proxy] Read timeout: ", uuid)
					continue
				}

				LOGD("[proxy] Read error: ", uuid, " ", err)
				return
			}

			if n > 0 {
				ps.msgBus.AddDataMsg(uuid, buf[:n], n)
				LOGD("[proxy] client--->proxy ", uuid, " recv: ", n)
			} else {
				BufferPool2K.Put(buf[:0])
			}
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
		//case *unix.SockaddrInet6:  //IPv6
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
