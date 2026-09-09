package platform

import (
	"bytes"
	"fmt"
	"strconv"
	"testing"
)

func TestBoundedBufferBoundary(t *testing.T) {
	t.Parallel()
	const limit = 17
	for _, size := range []int{0, limit - 1, limit, limit + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			t.Parallel()
			buffer := newBoundedBuffer(limit)
			input := bytes.Repeat([]byte("x"), size)
			// Single-byte writes exercise cumulative boundaries as well as empty writes.
			for _, value := range input {
				if n, err := buffer.Write([]byte{value}); n != 1 || err != nil {
					t.Fatalf("Write: %d %v", n, err)
				}
			}
			if n, err := buffer.Write(nil); n != 0 || err != nil {
				t.Fatalf("empty Write: %d %v", n, err)
			}
			want := input[:min(size, limit)]
			if !bytes.Equal(buffer.Bytes(), want) || buffer.Truncated() != (size > limit) {
				t.Fatalf("bytes=%q truncated=%v, input length=%d", buffer.Bytes(), buffer.Truncated(), size)
			}
			if size > 0 {
				copyBytes := buffer.Bytes()
				copyBytes[0] = 'z'
				if !bytes.Equal(buffer.Bytes(), want) {
					t.Fatal("Bytes aliases buffer")
				}
			}
		})
	}
}

func BenchmarkBoundedBuffer(b *testing.B) {
	const (
		small  = 4 << 10
		medium = 8 << 10
		large  = 16 << 10
	)
	for _, initial := range []int{0, small, medium, large} {
		for _, size := range []int{0, 64, small, large, maxRunnerOutputBytes - 1, maxRunnerOutputBytes, maxRunnerOutputBytes + 1} {
			for _, chunked := range []bool{false, true} {
				for _, copyResult := range []bool{false, true} {
					name := fmt.Sprintf("initial=%d/size=%d/chunked=%t/copy=%t", initial, size, chunked, copyResult)
					b.Run(name, func(b *testing.B) {
						input := bytes.Repeat([]byte("x"), size)
						b.ReportAllocs()
						for b.Loop() {
							var buffer *boundedBuffer
							if initial == 0 {
								buffer = newBoundedBuffer(maxRunnerOutputBytes)
							} else {
								buffer = &boundedBuffer{capacity: maxRunnerOutputBytes, data: make([]byte, 0, initial)}
							}
							chunk := max(1, len(input))
							if chunked {
								chunk = small
							}
							for offset := 0; offset < len(input); offset += chunk {
								if _, err := buffer.Write(input[offset:min(offset+chunk, len(input))]); err != nil {
									b.Fatal(err)
								}
							}
							if copyResult {
								benchmarkBufferBytes = buffer.Bytes()
							}
						}
					})
				}
			}
		}
	}
}

var benchmarkBufferBytes []byte

func TestBoundedBufferGrowthBudget(t *testing.T) {
	t.Parallel()
	const smallInitial = 16 << 10
	for _, limit := range []int{0, 1, 17, 10000, maxRunnerOutputBytes} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			t.Parallel()
			buffer := newBoundedBuffer(limit)
			if cap(buffer.data) > min(limit, smallInitial) {
				t.Errorf("eager capacity=%d for limit=%d", cap(buffer.data), limit)
			}
			const chunkSize = 1009
			input := bytes.Repeat([]byte("x"), chunkSize)
			for written := 0; written < limit+chunkSize; written += chunkSize {
				if n, err := buffer.Write(input); n != len(input) || err != nil {
					t.Fatalf("Write: %d %v", n, err)
				}
				if cap(buffer.data) > limit || len(buffer.data) > limit {
					t.Fatalf("exceeded budget: len=%d cap=%d limit=%d", len(buffer.data), cap(buffer.data), limit)
				}
			}
			if len(buffer.Bytes()) != limit || !buffer.Truncated() {
				t.Fatal("incorrect retained prefix/overflow")
			}
		})
	}
}

func TestBoundedBufferSingleWriteAndZeroCapacity(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{0, 17, maxRunnerOutputBytes} {
		for _, size := range []int{0, max(0, limit-1), limit, limit + 1} {
			t.Run(fmt.Sprintf("limit=%d/size=%d", limit, size), func(t *testing.T) {
				t.Parallel()
				buffer := newBoundedBuffer(limit)
				input := bytes.Repeat([]byte("y"), size)
				if n, err := buffer.Write(input); n != size || err != nil {
					t.Fatalf("Write: %d %v", n, err)
				}
				if n, err := buffer.Write(nil); n != 0 || err != nil {
					t.Fatalf("empty Write: %d %v", n, err)
				}
				if !bytes.Equal(buffer.Bytes(), input[:min(limit, size)]) || buffer.Truncated() != (size > limit) {
					t.Fatalf("retained=%d truncated=%v", len(buffer.Bytes()), buffer.Truncated())
				}
				if cap(buffer.data) > limit {
					t.Fatal("oversized single write exceeded backing capacity budget")
				}
			})
		}
	}
}
