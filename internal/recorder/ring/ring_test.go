package ring

import (
	"sync"
	"testing"
)

func TestWrapAround(t *testing.T) {
	r := New(8)
	out := make([]float32, 8)
	next := float32(0)
	want := float32(0)
	for round := 0; round < 100; round++ {
		in := []float32{next, next + 1, next + 2}
		if !r.WriteAll(in) {
			t.Fatalf("round %d: write failed with len %d", round, r.Len())
		}
		next += 3
		n := r.Read(out[:3])
		for _, v := range out[:n] {
			if v != want {
				t.Fatalf("got %v want %v", v, want)
			}
			want++
		}
	}
}

func TestWriteAllRejectsOverflow(t *testing.T) {
	r := New(4)
	if !r.WriteAll([]float32{1, 2, 3}) {
		t.Fatal("first write should fit")
	}
	if r.WriteAll([]float32{4, 5}) {
		t.Fatal("write beyond capacity must fail atomically")
	}
	if r.Len() != 3 {
		t.Fatalf("len = %d", r.Len())
	}
}

func TestConcurrentSPSC(t *testing.T) {
	const total = 1 << 20
	r := New(1024)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		chunk := make([]float32, 64)
		for v := 0; v < total; {
			for i := range chunk {
				chunk[i] = float32(v + i)
			}
			if r.WriteAll(chunk) {
				v += len(chunk)
			}
		}
	}()
	buf := make([]float32, 100)
	want := 0
	for want < total {
		n := r.Read(buf)
		for _, v := range buf[:n] {
			if int(v) != want {
				t.Fatalf("got %v want %d", v, want)
			}
			want++
		}
	}
	wg.Wait()
}
