package common

import (
	"sync"
)

type BufSize int

const (
	BufSize128  = 128
	BufSize2048 = 2048
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

var BufferPool128 = NewBufPool(BufSize128)
var BufferPool2K = NewBufPool(BufSize2048)
