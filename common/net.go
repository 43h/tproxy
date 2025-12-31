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

func NewMessageReader(conn io.Reader, bufPool *BufferPool) *MessageReader {
	return &MessageReader{
		conn: conn,
	}
}

func NewMessageWriter(conn io.Writer, bufPool *BufferPool) *MessageWriter {
	return &MessageWriter{
		conn: conn,
	}
}

func (r *MessageReader) ReadMessage() (*Message, error) {

	msgBuf := BufferPool2K.Get()

	//read length of header
	if _, err := io.ReadFull(r.conn, msgBuf[:2]); err != nil {
		return nil, fmt.Errorf("read length failed: %w", err)
	}
	totalLen := binary.BigEndian.Uint16(msgBuf[:2])

	//read header
	if _, err := io.ReadFull(r.conn, msgBuf[:totalLen]); err != nil {
		return nil, fmt.Errorf("read message body failed: %w", err)
	}

	//decode
	var msg Message
	decoder := gob.NewDecoder(bytes.NewReader(msgBuf[:totalLen]))
	if err := decoder.Decode(&msg.Header); err != nil {
		return nil, fmt.Errorf("decode header failed: %w", err)
	}

	if msg.Header.Len > 0 {
		if _, err := io.ReadFull(r.conn, msgBuf[:msg.Header.Len]); err != nil {
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

	if _, err := w.conn.Write(lengthBuf[:]); err != nil {
		return fmt.Errorf("write length failed: %w", err)
	}

	if _, err := w.conn.Write(headerBytes); err != nil {
		return fmt.Errorf("write header failed: %w", err)
	}

	if msg.Header.Len > 0 && len(msg.Data) > 0 {
		_, err := w.conn.Write(msg.Data)
		BufferPool2K.Put(msg.Data[:0])
		msg.Data = nil
		if err != nil {
			return fmt.Errorf("write data failed: %w", err)
		}
	}

	return nil
}