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

type TProxyServer struct {
	addr     string
	connMgr  *ConnectionManager
	msgBus   *MessageBus
	listener net.Listener
	status   int
}

func NewTProxyServer(addr string, connMgr *ConnectionManager, msgBus *MessageBus) *TProxyServer {
	return &TProxyServer{
		addr:    addr,
		connMgr: connMgr,
		msgBus:  msgBus,
		status:  StatusNull,
	}
}

func (s *TProxyServer) Start(ctx context.Context) error {
	LOGI("[tproxy] Initializing server on: ", s.addr)

	// 解析地址
	tcpAddr, err := net.ResolveTCPAddr("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("resolve address failed: %w", err)
	}

	// 创建 socket
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		return fmt.Errorf("create socket failed: %w", err)
	}

	// 设置 SO_REUSEADDR
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("set SO_REUSEADDR failed: %w", err)
	}

	// 设置 IP_TRANSPARENT
	if err := syscall.SetsockoptInt(fd, syscall.SOL_IP, syscall.IP_TRANSPARENT, 1); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("set IP_TRANSPARENT failed: %w", err)
	}

	// Bind
	sockaddr := &syscall.SockaddrInet4{
		Port: tcpAddr.Port,
	}
	copy(sockaddr.Addr[:], tcpAddr.IP.To4())
	if err := syscall.Bind(fd, sockaddr); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("bind failed: %w", err)
	}

	// Listen
	if err := syscall.Listen(fd, syscall.SOMAXCONN); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("listen failed: %w", err)
	}

	// 将文件描述ost.Listener
	file := os.NewFile(uintptr(fd), "tproxy-listener")
	tmpListener, err := net.FileListener(file)
	file.Close()
	if err != nil {
		syscall.Close(fd)
		return fmt.Errorf("create listener failed: %w", err)
	}

	s.listener = tmpListener
	s.status = StatusListen

	LOGI("[tproxy] Server listening on ", s.addr)
	LOGI("[tproxy] IP_TRANSPARENT enabled")

	// 接受连接循环
	return s.acceptLoop(ctx)
}

// acceptLoop 接受连接循环
func (s *TProxyServer) acceptLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				LOGE("[tproxy] Accept failed: ", err)
				continue
			}

			// 每个连接在独立的goroutine中处理
			go s.handleConnection(conn)
		}
	}
}

// handleConnection 处理新连接
func (s *TProxyServer) handleConnection(conn net.Conn) {
	origDst, err := getOriginalDst(conn.(*net.TCPConn))
	if err != nil {
		LOGE("[tproxy] Get original destination failed: ", err)
		conn.Close()
		return
	}

	// 生成连接UUID
	connUUID := uuid.New().String()
	clientAddr := conn.RemoteAddr().String()

	LOGI("[tproxy] New connection: ", connUUID, " from ", clientAddr, " to ", origDst)

	// 创建连接信息
	connInfo := &ConnInfo{
		UUID:   connUUID,
		IPStr:  origDst,
		Conn:   conn,
		Status: StatusConnected,
	}

	// 添加到连接管理器
	s.connMgr.Add(connUUID, connInfo)

	// 发送连接事件到上游
	s.msgBus.EmitConnect(connUUID, origDst, nil)

	// 启动数据读取循环
	s.readLoop(connUUID, conn)
}

// readLoop 读取连接数据循环（零拷贝优化）
func (s *TProxyServer) readLoop(uuid string, conn net.Conn) {
	defer func() {
		// 连接关闭时发送断开事件
		s.msgBus.EmitDisconnect(uuid)
		s.connMgr.Delete(uuid)
		LOGI("[tproxy] Connection closed: ", uuid)
	}()

	bufPool := DefaultBufferPool

	for {
		// 从池中获取buffer
		buf := bufPool.Get()
		n, err := conn.Read(buf)

		if err != nil {
			bufPool.Put(buf)
			LOGD("[tproxy] Read error: ", uuid, " ", err)
			return
		}

		if n > 0 {
			// 零拷贝：直接使用buffer切片，传递释放回调
			data := buf[:n]

			// 发送数据事件（带释放回调）
			s.msgBus.EmitData(uuid, data, n)
			LOGD("[tproxy] Data read: ", uuid, " ", n, " bytes")
		} else {
			bufPool.Put(buf)
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

func (s *TProxyServer) Close() {
	LOGI("[tproxy] Closing server")

	s.status = StatusNull

	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			LOGE("[tproxy] Close listener failed: ", err)
		} else {
			LOGI("[tproxy] Listener closed")
		}
		s.listener = nil
	}
}