package common

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
)

type MessageReader struct {
	conn io.Reader
	br   *bufio.Reader
}

type MessageWriter struct {
	conn io.Writer
	bw   *bufio.Writer
}

func NewMessageReader(conn io.Reader) *MessageReader {
	return &MessageReader{
		conn: conn,
		br:   bufio.NewReaderSize(conn, 4096),
	}
}

func NewMessageWriter(conn io.Writer) *MessageWriter {
	return &MessageWriter{
		conn: conn,
		bw:   bufio.NewWriterSize(conn, 4096),
	}
}

func (r *MessageReader) ReadMessage() (*Message, error) {

	msgBuf := BufferPool2K.Get()

	//read length of header
	if n, err := io.ReadFull(r.br, msgBuf[:2]); err != nil || n != 2 {
		return nil, fmt.Errorf("read length failed: %w", err)
	}
	totalLen := binary.BigEndian.Uint16(msgBuf[:2])

	//read header
	if n, err := io.ReadFull(r.br, msgBuf[:totalLen]); err != nil || n != int(totalLen) {
		return nil, fmt.Errorf("read message body failed: %w", err)
	}

	//decode
	var msg Message
	decoder := gob.NewDecoder(bytes.NewReader(msgBuf[:totalLen]))
	if err := decoder.Decode(&msg.Header); err != nil {
		return nil, fmt.Errorf("decode header failed: %w", err)
	}

	if msg.Header.Len > 0 {
		if n, err := io.ReadFull(r.br, msgBuf[:msg.Header.Len]); err != nil || n != msg.Header.Len {
			BufferPool2K.Put(msgBuf[:0])
			return nil, fmt.Errorf("read message body failed: %w", err)
		}
		msg.Data = msgBuf[:msg.Header.Len]
	} else {
		BufferPool2K.Put(msgBuf[:0])
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

	if _, err := w.bw.Write(lengthBuf[:]); err != nil {
		return fmt.Errorf("write length failed: %w", err)
	}

	if _, err := w.bw.Write(headerBytes); err != nil {
		return fmt.Errorf("write header failed: %w", err)
	}

	if msg.Header.Len > 0 && len(msg.Data) > 0 {
		if _, err := w.bw.Write(msg.Data); err != nil {
			return fmt.Errorf("write data failed: %w", err)
		}
		BufferPool2K.Put(msg.Data[:0])
		msg.Data = nil
	}

	if err := w.bw.Flush(); err != nil {
		return fmt.Errorf("flush failed: %w", err)
	}

	return nil
}
