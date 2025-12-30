package common

import (
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"net"
	"sync"
)

// EventBus 事件总线别名（relay使用）
type EventBus = MessageBus

// NewEventBus 创建事件总线（relay使用）
func NewEventBus(chanSize int) *EventBus {
	return NewMessageBus(chanSize)
}

// MessageReader 消息读取器
// 消息格式：2字节长度 + gob编码的MessageHeader + 真实数据
type MessageReader struct {
	conn    io.Reader
	mu      sync.Mutex
	bufPool *BufferPool
}

// MessageWriter 消息写入器
// 消息格式：2字节长度 + gob编码的MessageHeader + 真实数据
type MessageWriter struct {
	conn    io.Writer
	mu      sync.Mutex
	bufPool *BufferPool
	encoder *gob.Encoder
	encBuf  []byte // 用于编码header的临时缓冲区
}

var (
	// 用于编码的buffer pool
	headerBufPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, 256) // header通常不会太大
		},
	}
)

// NewMessageReader 创建消息读取器
func NewMessageReader(conn io.Reader) *MessageReader {
	return &MessageReader{
		conn:    conn,
		bufPool: NewBufPool(BufSize2048),
	}
}

// NewMessageWriter 创建消息写入器
func NewMessageWriter(conn io.Writer) *MessageWriter {
	return &MessageWriter{
		conn:    conn,
		bufPool: NewBufPool(BufSize2048),
		encBuf:  make([]byte, 0, 256),
	}
}

// ReadMessage 读取一个完整消息
func (r *MessageReader) ReadMessage() (*Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 1. 读取2字节长度
	lengthBuf := make([]byte, 2)
	if _, err := io.ReadFull(r.conn, lengthBuf); err != nil {
		return nil, fmt.Errorf("read length failed: %w", err)
	}
	totalLen := binary.BigEndian.Uint16(lengthBuf)

	// 2. 读取完整消息体（header + data）
	msgBuf := make([]byte, totalLen)
	if _, err := io.ReadFull(r.conn, msgBuf); err != nil {
		return nil, fmt.Errorf("read message body failed: %w", err)
	}

	// 3. 解码header（使用gob）
	var header MessageHeader
	decoder := gob.NewDecoder(newBytesReader(msgBuf))
	if err := decoder.Decode(&header); err != nil {
		return nil, fmt.Errorf("decode header failed: %w", err)
	}

	// 4. 构建消息
	msg := &Message{
		Header: header,
		Len:    header.Len,
	}

	// 5. 提取数据部分
	if header.Len > 0 {
		// 计算header占用的字节数
		headerLen := len(msgBuf) - header.Len
		if headerLen < 0 || headerLen+header.Len > len(msgBuf) {
			return nil, fmt.Errorf("invalid message format: header_len=%d, data_len=%d, total=%d",
				headerLen, header.Len, len(msgBuf))
		}

		// 复制数据部分到新buffer（避免引用临时buffer）
		msg.Data = make([]byte, header.Len)
		copy(msg.Data, msgBuf[headerLen:])
	}

	return msg, nil
}

// WriteMessage 写入一个完整消息
func (w *MessageWriter) WriteMessage(msg *Message) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 1. 编码header到临时buffer
	w.encBuf = w.encBuf[:0]
	encoder := gob.NewEncoder(newBytesWriter(&w.encBuf))
	if err := encoder.Encode(&msg.Header); err != nil {
		return fmt.Errorf("encode header failed: %w", err)
	}

	// 2. 计算总长度
	headerLen := len(w.encBuf)
	dataLen := len(msg.Data)
	totalLen := headerLen + dataLen

	if totalLen > 65535 {
		return fmt.Errorf("message too large: %d bytes", totalLen)
	}

	// 3. 构建完整消息：2字节长度 + header + data
	lengthBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(lengthBuf, uint16(totalLen))

	// 4. 一次性写入（减少系统调用）
	// 写入长度
	if _, err := w.conn.Write(lengthBuf); err != nil {
		return fmt.Errorf("write length failed: %w", err)
	}

	// 写入header
	if _, err := w.conn.Write(w.encBuf); err != nil {
		return fmt.Errorf("write header failed: %w", err)
	}

	// 写入数据
	if dataLen > 0 {
		if _, err := w.conn.Write(msg.Data); err != nil {
			return fmt.Errorf("write data failed: %w", err)
		}
	}

	return nil
}

// bytesReader 简单的bytes reader（避免import bytes）
type bytesReader struct {
	data []byte
	pos  int
}

func newBytesReader(data []byte) *bytesReader {
	return &bytesReader{data: data, pos: 0}
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// bytesWriter 简单的bytes writer
type bytesWriter struct {
	buf *[]byte
}

func newBytesWriter(buf *[]byte) *bytesWriter {
	return &bytesWriter{buf: buf}
}

func (w *bytesWriter) Write(p []byte) (n int, err error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}

// Emit* 方法 - 为MessageBus添加Emit接口（支持零拷贝优化）

// EmitConnect 发送连接事件
func (mb *MessageBus) EmitConnect(uuid string, ipStr string, conn net.Conn) {
	msg := Message{
		Header: MessageHeader{
			Source:  MsgSourceLocal,
			MsgType: MsgTypeConnect,
			UUID:    uuid,
			IPStr:   ipStr,
			Len:     0,
		},
		Source:      MsgSourceLocal,
		MessageType: MsgTypeConnect,
		MsgType:     MsgTypeConnect,
		UUID:        uuid,
		IPStr:       ipStr,
		Length:      0,
		Len:         0,
	}
	mb.msgChan <- msg
}

// EmitDisconnect 发送断开连接事件
func (mb *MessageBus) EmitDisconnect(uuid string) {
	msg := Message{
		Header: MessageHeader{
			Source:  MsgSourceLocal,
			MsgType: MsgTypeDisconnect,
			UUID:    uuid,
			Len:     0,
		},
		Source:      MsgSourceLocal,
		MessageType: MsgTypeDisconnect,
		MsgType:     MsgTypeDisconnect,
		UUID:        uuid,
		Length:      0,
		Len:         0,
	}
	mb.msgChan <- msg
}

// EmitData 发送数据事件（带自动释放）
func (mb *MessageBus) EmitData(uuid string, data []byte, length int) {
	bufPool := DefaultBufferPool
	releaseFunc := func() {
		bufPool.Put(data)
	}

	msg := Message{
		Header: MessageHeader{
			Source:  MsgSourceLocal,
			MsgType: MsgTypeData,
			UUID:    uuid,
			Len:     length,
		},
		Source:      MsgSourceLocal,
		MessageType: MsgTypeData,
		MsgType:     MsgTypeData,
		UUID:        uuid,
		Length:      length,
		Len:         length,
		Data:        data[:length],
		ReleaseFunc: releaseFunc,
	}
	mb.msgChan <- msg
}

// EmitDataWithRelease 发送数据事件（手动指定释放函数）
func (mb *MessageBus) EmitDataWithRelease(uuid string, data []byte, length int, releaseFunc func()) {
	msg := Message{
		Header: MessageHeader{
			Source:  MsgSourceLocal,
			MsgType: MsgTypeData,
			UUID:    uuid,
			Len:     length,
		},
		Source:      MsgSourceLocal,
		MessageType: MsgTypeData,
		MsgType:     MsgTypeData,
		UUID:        uuid,
		Length:      length,
		Len:         length,
		Data:        data[:length],
		ReleaseFunc: releaseFunc,
	}
	mb.msgChan <- msg
}