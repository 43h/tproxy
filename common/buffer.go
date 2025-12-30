package common

import (
	"sync"
)

type BufSize int

const (
	BufSize64   BufSize = 64
	BufSize128          = 128
	BufSize2048         = 2048
)

type BufferPool struct {
	bufSize BufSize
	pool    *sync.Pool
}

func NewBufPool(bufSize BufSize) *BufferPool {
	return &BufferPool{
		bufSize: bufSize,
		pool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, bufSize)
			},
		},
	}
}

func (bp *BufferPool) Get() []byte {
	return bp.pool.Get().([]byte)
}

func (bp *BufferPool) Put(buf []byte) {
	bp.pool.Put(buf)
}

func (bp *BufferPool) getBufSize() int {
	return int(bp.bufSize)
}

// DefaultBufferPool 默认buffer池（2048字节）
var DefaultBufferPool = NewBufPool(BufSize2048)