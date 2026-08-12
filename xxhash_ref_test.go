package xxhash

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"testing"
)

// This file contains a straightforward, self-contained implementation of XXH64
// which is used as a reference to check the optimized implementations against.
// It intentionally does not share any code (or even constants) with the rest of
// the package.

const (
	refPrime1 uint64 = 0x9E3779B185EBCA87
	refPrime2 uint64 = 0xC2B2AE3D27D4EB4F
	refPrime3 uint64 = 0x165667B19E3779F9
	refPrime4 uint64 = 0x85EBCA77C2B2AE63
	refPrime5 uint64 = 0x27D4EB2F165667C5
)

func refRound(acc, input uint64) uint64 {
	acc += input * refPrime2
	acc = bits.RotateLeft64(acc, 31)
	acc *= refPrime1
	return acc
}

func refMergeRound(acc, val uint64) uint64 {
	val = refRound(0, val)
	acc ^= val
	acc = acc*refPrime1 + refPrime4
	return acc
}

func refSum64(b []byte, seed uint64) uint64 {
	n := len(b)
	var h uint64

	if n >= 32 {
		v1 := seed + refPrime1 + refPrime2
		v2 := seed + refPrime2
		v3 := seed
		v4 := seed - refPrime1
		for len(b) >= 32 {
			v1 = refRound(v1, binary.LittleEndian.Uint64(b[0:8]))
			v2 = refRound(v2, binary.LittleEndian.Uint64(b[8:16]))
			v3 = refRound(v3, binary.LittleEndian.Uint64(b[16:24]))
			v4 = refRound(v4, binary.LittleEndian.Uint64(b[24:32]))
			b = b[32:]
		}
		h = bits.RotateLeft64(v1, 1) +
			bits.RotateLeft64(v2, 7) +
			bits.RotateLeft64(v3, 12) +
			bits.RotateLeft64(v4, 18)
		h = refMergeRound(h, v1)
		h = refMergeRound(h, v2)
		h = refMergeRound(h, v3)
		h = refMergeRound(h, v4)
	} else {
		h = seed + refPrime5
	}

	h += uint64(n)

	for ; len(b) >= 8; b = b[8:] {
		h ^= refRound(0, binary.LittleEndian.Uint64(b[:8]))
		h = bits.RotateLeft64(h, 27)*refPrime1 + refPrime4
	}
	if len(b) >= 4 {
		h ^= uint64(binary.LittleEndian.Uint32(b[:4])) * refPrime1
		h = bits.RotateLeft64(h, 23)*refPrime2 + refPrime3
		b = b[4:]
	}
	for ; len(b) > 0; b = b[1:] {
		h ^= uint64(b[0]) * refPrime5
		h = bits.RotateLeft64(h, 11) * refPrime1
	}

	h ^= h >> 33
	h *= refPrime2
	h ^= h >> 29
	h *= refPrime3
	h ^= h >> 32

	return h
}

// testSizes covers every length up to a few full vector groups, so that all
// combinations of full groups, leftover blocks, and leftover bytes are hit,
// plus a handful of larger sizes.
func testSizes() []int {
	var sizes []int
	for n := 0; n <= 1100; n++ {
		sizes = append(sizes, n)
	}
	return append(sizes, 4096, 10000, 65536, 100000)
}

func testInput(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

func TestSum64Reference(t *testing.T) {
	forEachImpl(t, func(t *testing.T) {
		for _, n := range testSizes() {
			in := testInput(n)
			want := refSum64(in, 0)
			if got := Sum64(in); got != want {
				t.Fatalf("Sum64(%d bytes): got 0x%x; want 0x%x", n, got, want)
			}
			if got := Sum64String(string(in)); got != want {
				t.Fatalf("Sum64String(%d bytes): got 0x%x; want 0x%x", n, got, want)
			}
		}
	})
}

// TestSum64Unaligned checks that the implementations don't assume anything
// about the alignment of the input.
func TestSum64Unaligned(t *testing.T) {
	forEachImpl(t, func(t *testing.T) {
		base := testInput(2048)
		for off := 1; off < 16; off++ {
			for _, n := range []int{0, 1, 15, 31, 32, 33, 255, 256, 257, 512, 1000} {
				in := base[off : off+n]
				want := refSum64(in, 0)
				if got := Sum64(in); got != want {
					t.Fatalf("Sum64(off=%d, %d bytes): got 0x%x; want 0x%x",
						off, n, got, want)
				}
			}
		}
	})
}

func TestDigestReference(t *testing.T) {
	seeds := []uint64{0, 1, 0x9E3779B185EBCA87, math.MaxUint64}
	chunkSizes := []int{1, 3, 8, 31, 32, 33, 64, 255, 256, 257, 1 << 20}
	forEachImpl(t, func(t *testing.T) {
		for _, n := range testSizes() {
			in := testInput(n)
			for _, seed := range seeds {
				want := refSum64(in, seed)
				for _, chunkSize := range chunkSizes {
					d := NewWithSeed(seed)
					ds := NewWithSeed(seed)
					for i := 0; i < len(in); i += chunkSize {
						chunk := in[i:]
						if len(chunk) > chunkSize {
							chunk = chunk[:chunkSize]
						}
						d.Write(chunk)
						ds.WriteString(string(chunk))
					}
					if got := d.Sum64(); got != want {
						t.Fatalf("Digest(n=%d, seed=%d, chunk=%d): got 0x%x; want 0x%x",
							n, seed, chunkSize, got, want)
					}
					if got := ds.Sum64(); got != want {
						t.Fatalf("Digest/WriteString(n=%d, seed=%d, chunk=%d): got 0x%x; want 0x%x",
							n, seed, chunkSize, got, want)
					}
				}
			}
		}
	})
}

// TestDigestSplitWrites feeds the same input to a Digest in every possible
// two-part split, which exercises the interaction between the buffered
// remainder and the block loop.
func TestDigestSplitWrites(t *testing.T) {
	forEachImpl(t, func(t *testing.T) {
		in := testInput(600)
		want := refSum64(in, 0)
		for i := 0; i <= len(in); i++ {
			d := New()
			d.Write(in[:i])
			d.Write(in[i:])
			if got := d.Sum64(); got != want {
				t.Fatalf("Digest split at %d: got 0x%x; want 0x%x", i, got, want)
			}
		}
	})
}

func BenchmarkSum64Sizes(b *testing.B) {
	for _, n := range []int{8, 32, 64, 128, 256, 512, 1024, 8192, 65536} {
		in := testInput(n)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.SetBytes(int64(n))
			for i := 0; i < b.N; i++ {
				sink = Sum64(in)
			}
		})
	}
}

// BenchmarkImplSizes compares the available implementations against each other
// at the sizes where the choice between them matters.
func BenchmarkImplSizes(b *testing.B) {
	sizes := []int{96, 128, 160, 192, 256, 320, 384, 512, 768, 1024, 2048, 4096}
	forEachImplBench(b, func(b *testing.B) {
		for _, n := range sizes {
			in := testInput(n)
			b.Run(fmt.Sprint(n), func(b *testing.B) {
				b.SetBytes(int64(n))
				for i := 0; i < b.N; i++ {
					sink = Sum64(in)
				}
			})
		}
	})
}

// TestInitConstants checks the hand-derived accumulator seeds against the
// wrapping arithmetic they stand for.
func TestInitConstants(t *testing.T) {
	p1, p2 := primes[0], primes[1]
	if got, want := initV1, p1+p2; got != want {
		t.Errorf("initV1 = %d; want prime1+prime2 = %d", got, want)
	}
	if got, want := initV4, -p1; got != want {
		t.Errorf("initV4 = %d; want -prime1 = %d", got, want)
	}
}
