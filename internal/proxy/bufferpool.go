package proxy

import "sync"

// BufferPool is a pool of reusable byte buffers with a size of multiples of 1KB
type BufferPool struct {
	pool sync.Pool
}

// NewBufferPool creates a new BufferPool with buffers of the given size
func NewBufferPool(bufferSize int) *BufferPool {
	new := func() any {
		buf := make([]byte, bufferSize)
		return &buf
	}
	return &BufferPool{
		pool: sync.Pool{
			New: new,
		},
	}
}

// Get retrieves a buffer from the pool
func (bp *BufferPool) Get() *[]byte {
	return bp.pool.Get().(*[]byte)
}

// Put returns a buffer to the pool
func (bp *BufferPool) Put(buf *[]byte) {
	bp.pool.Put(buf)
}
