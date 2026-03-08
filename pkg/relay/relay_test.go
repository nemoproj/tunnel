package relay

import (
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

var benchmarkCounterSink int64

func TestCountingReaderUpdatesCounters(t *testing.T) {
	var counter1, counter2 int64

	reader := &CountingReader{
		r:        strings.NewReader("hello"),
		counters: []*int64{&counter1, &counter2},
	}

	buf := make([]byte, 8)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 5 {
		t.Fatalf("Read() bytes = %d, want 5", n)
	}
	if got := atomic.LoadInt64(&counter1); got != 5 {
		t.Fatalf("counter1 = %d, want 5", got)
	}
	if got := atomic.LoadInt64(&counter2); got != 5 {
		t.Fatalf("counter2 = %d, want 5", got)
	}
}

func TestCountingReaderSupportsNilCounter(t *testing.T) {
	var counter int64

	reader := &CountingReader{
		r:        strings.NewReader("go"),
		counters: []*int64{&counter},
	}

	buf := make([]byte, 8)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 2 {
		t.Fatalf("Read() bytes = %d, want 2", n)
	}
	if got := atomic.LoadInt64(&counter); got != 2 {
		t.Fatalf("counter = %d, want 2", got)
	}

	n, err = reader.Read(buf)
	if err != io.EOF {
		t.Fatalf("Read() error = %v, want %v", err, io.EOF)
	}
	if n != 0 {
		t.Fatalf("Read() bytes = %d, want 0", n)
	}
	if got := atomic.LoadInt64(&counter); got != 2 {
		t.Fatalf("counter after EOF = %d, want 2", got)
	}
}

type fixedReadSizer struct {
	n int
}

func (r fixedReadSizer) Read(p []byte) (int, error) {
	if len(p) < r.n {
		return len(p), nil
	}
	return r.n, nil
}

func BenchmarkCountingReaderRead(b *testing.B) {
	const readSize = 4096

	buf := make([]byte, readSize)
	reader := fixedReadSizer{n: readSize}

	b.Run("current_slice", func(b *testing.B) {
		var counter1, counter2 int64
		cr := &CountingReader{
			r:        reader,
			counters: []*int64{&counter1, &counter2},
		}

		b.ReportAllocs()
		b.SetBytes(readSize)
		for i := 0; i < b.N; i++ {
			if _, err := cr.Read(buf); err != nil {
				b.Fatalf("Read() error = %v", err)
			}
		}
		benchmarkCounterSink = counter1 + counter2
	})

	b.Run("direct_fields_candidate", func(b *testing.B) {
		var counter1, counter2 int64
		cr := &directCountingReader{
			r:        reader,
			counter1: &counter1,
			counter2: &counter2,
		}

		b.ReportAllocs()
		b.SetBytes(readSize)
		for i := 0; i < b.N; i++ {
			if _, err := cr.Read(buf); err != nil {
				b.Fatalf("Read() error = %v", err)
			}
		}
		benchmarkCounterSink = counter1 + counter2
	})
}

type directCountingReader struct {
	r        io.Reader
	counter1 *int64
	counter2 *int64
}

func (c *directCountingReader) Read(p []byte) (n int, err error) {
	n, err = c.r.Read(p)
	if n > 0 {
		delta := int64(n)
		if c.counter1 != nil {
			atomic.AddInt64(c.counter1, delta)
		}
		if c.counter2 != nil {
			atomic.AddInt64(c.counter2, delta)
		}
	}
	return
}
