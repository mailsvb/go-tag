// Package ring implements a lock-free single-producer/single-consumer ring
// buffer of float32 samples, safe to use from a real-time audio callback.
package ring

import "sync/atomic"

// Ring is an SPSC ring buffer. Exactly one goroutine/thread may call the
// producer methods (Write, Free) and exactly one the consumer methods (Read,
// Discard). Len may be called from either side.
type Ring struct {
	buf  []float32
	mask uint64
	_    [64]byte // keep the indices on separate cache lines
	w    atomic.Uint64
	_    [56]byte
	r    atomic.Uint64
}

// New returns a ring holding at least minCap samples (rounded up to a power of two).
func New(minCap int) *Ring {
	n := 1
	for n < minCap {
		n <<= 1
	}
	return &Ring{buf: make([]float32, n), mask: uint64(n - 1)}
}

// Cap returns the capacity in samples.
func (r *Ring) Cap() int { return len(r.buf) }

// Len returns the number of samples available for reading.
func (r *Ring) Len() int { return int(r.w.Load() - r.r.Load()) }

// Free returns the number of samples that can be written.
func (r *Ring) Free() int { return len(r.buf) - r.Len() }

// WriteAll writes all of p or nothing. It returns false if p does not fit.
// Writing all-or-nothing keeps interleaved frames intact.
func (r *Ring) WriteAll(p []float32) bool {
	w := r.w.Load()
	if len(p) > len(r.buf)-int(w-r.r.Load()) {
		return false
	}
	i := int(w & r.mask)
	n := copy(r.buf[i:], p)
	copy(r.buf, p[n:])
	r.w.Store(w + uint64(len(p)))
	return true
}

// Read copies up to len(p) samples into p and returns how many were read.
func (r *Ring) Read(p []float32) int {
	rd := r.r.Load()
	avail := int(r.w.Load() - rd)
	if avail < len(p) {
		p = p[:avail]
	}
	i := int(rd & r.mask)
	n := copy(p, r.buf[i:])
	copy(p[n:], r.buf)
	r.r.Store(rd + uint64(len(p)))
	return len(p)
}

// Discard drops up to n samples from the read side and returns how many were dropped.
func (r *Ring) Discard(n int) int {
	rd := r.r.Load()
	if avail := int(r.w.Load() - rd); n > avail {
		n = avail
	}
	r.r.Store(rd + uint64(n))
	return n
}
