package xxhash

import (
	"fmt"
	"strings"
	"testing"
)

var benchmarks = []struct {
	name string
	n    int64
}{
	{"4B", 4},
	{"16B", 16},
	{"100B", 100},
	{"4KB", 4e3},
	{"10MB", 10e6},
}

func BenchmarkSum64(b *testing.B) {
	for _, bb := range benchmarks {
		in := make([]byte, bb.n)
		for i := range in {
			in[i] = byte(i)
		}
		b.Run(bb.name, func(b *testing.B) {
			b.SetBytes(bb.n)
			for i := 0; i < b.N; i++ {
				_ = Sum64(in)
			}
		})
	}
}

func BenchmarkSum64String(b *testing.B) {
	for _, bb := range benchmarks {
		s := strings.Repeat("a", int(bb.n))
		b.Run(bb.name, func(b *testing.B) {
			b.SetBytes(bb.n)
			for i := 0; i < b.N; i++ {
				_ = Sum64String(s)
			}
		})
	}
}

func BenchmarkDigestBytes(b *testing.B) {
	for _, bb := range benchmarks {
		in := make([]byte, bb.n)
		for i := range in {
			in[i] = byte(i)
		}
		b.Run(bb.name, func(b *testing.B) {
			b.SetBytes(bb.n)
			for i := 0; i < b.N; i++ {
				h := New()
				h.Write(in)
				_ = h.Sum64()
			}
		})
	}
}

// BenchmarkDigestChunks feeds a fixed amount of data to a Digest in small
// pieces. Writes that don't fill out the 32-byte block are the common case for
// a streaming caller, and they take a different path through Write than the
// single large write the benchmarks above measure.
func BenchmarkDigestChunks(b *testing.B) {
	const n = 1024
	in := make([]byte, n)
	for i := range in {
		in[i] = byte(i)
	}
	for _, chunk := range []int{1, 4, 8, 16, 24, 32} {
		b.Run(fmt.Sprint(chunk), func(b *testing.B) {
			b.SetBytes(n)
			for i := 0; i < b.N; i++ {
				h := New()
				for j := 0; j < n; j += chunk {
					end := j + chunk
					if end > n {
						end = n
					}
					h.Write(in[j:end])
				}
				sink = h.Sum64()
			}
		})
	}
}

func BenchmarkDigestString(b *testing.B) {
	for _, bb := range benchmarks {
		s := strings.Repeat("a", int(bb.n))
		b.Run(bb.name, func(b *testing.B) {
			b.SetBytes(bb.n)
			for i := 0; i < b.N; i++ {
				h := New()
				h.WriteString(s)
				_ = h.Sum64()
			}
		})
	}
}
