package common

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
)

type MessageReader struct {
	conn io.Reader
}

type MessageWriter struct {
	conn io.Writer
}

func NewMessageReader(conn io.Reader) *MessageReader {
	return &MessageReader{
		conn: conn,
	}
}

func NewMessageWriter(conn io.Writer) *MessageWriter {
	return &MessageWriter{
		conn: conn,
	}
}

func (r *MessageReader) ReadMessage() (*Message, error) {

	msgBuf := BufferPool2K.Get()

	//read length of header
	if n, err := io.ReadFull(r.conn, msgBuf[:2]); err != nil || n != 2 {
		return nil, fmt.Errorf("read length failed: %w", err)
	}
	totalLen := binary.BigEndian.Uint16(msgBuf[:2])

	//read header
	if n, err := io.ReadFull(r.conn, msgBuf[:totalLen]); err != nil || n != int(totalLen) {
		return nil, fmt.Errorf("read message body failed: %w", err)
	}

	//decode
	var msg Message
	decoder := gob.NewDecoder(bytes.NewReader(msgBuf[:totalLen]))
	if err := decoder.Decode(&msg.Header); err != nil {
		return nil, fmt.Errorf("decode header failed: %w", err)
	}

	if msg.Header.Len > 0 {
		if n, err := io.ReadFull(r.conn, msgBuf[:msg.Header.Len]); err != nil || n != msg.Header.Len {
			return nil, fmt.Errorf("read message body failed: %w", err)
		}
		msg.Data = msgBuf[:msg.Header.Len]
	}

	return &msg, nil
}

func (w *MessageWriter) WriteMessage(msg *Message) error {
	headerBuf := BufferPool128.Get()
	defer BufferPool128.Put(headerBuf[:0])

	buf := bytes.NewBuffer(headerBuf[:0])
	encoder := gob.NewEncoder(buf)

	if err := encoder.Encode(&msg.Header); err != nil {
		return fmt.Errorf("encode header failed: %w", err)
	}

	headerBytes := buf.Bytes()
	headerLen := len(headerBytes)

	lengthBuf := [2]byte{}
	binary.BigEndian.PutUint16(lengthBuf[:], uint16(headerLen))

	if n, err := w.conn.Write(lengthBuf[:]); err != nil || n != 2 {
		return fmt.Errorf("write length failed: %w", err)
	}

	if n, err := w.conn.Write(headerBytes); err != nil || n != headerLen {
		return fmt.Errorf("write header failed: %w", err)
	}

	if msg.Header.Len > 0 && len(msg.Data) > 0 {
		if n, err := w.conn.Write(msg.Data); err != nil || n != len(msg.Data) {
			return fmt.Errorf("write data failed: %w", err)
		}
		BufferPool2K.Put(msg.Data[:0])
		msg.Data = nil
	}

	return nil
}
