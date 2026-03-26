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
	buf := bp.pool.Get().([]byte)
	return buf[:cap(buf)] // 确保返回完整长度，防止 Put(buf[:0]) 后 Get 到零长度 buffer
}

func (bp *BufferPool) Put(buf []byte) {
	bp.pool.Put(buf[:cap(buf)]) // 归还时恢复完整长度
}

func (bp *BufferPool) getBufSize() int {
	return int(bp.bufSize)
}

var BufferPool128 = NewBufPool(BufSize128)
var BufferPool2K = NewBufPool(BufSize2048)
