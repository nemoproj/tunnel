package relay

import (
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCountingReaderUpdatesCounters(t *testing.T) {
	var counter1, counter2 int64

	reader := &CountingReader{
		r:        strings.NewReader("hello"),
		counter1: &counter1,
		counter2: &counter2,
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
		counter1: &counter,
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
