// Package xxhash implements the 64-bit variant of xxHash (XXH64) as described
// at http://cyan4973.github.io/xxHash/.
package xxhash

import (
	"encoding/binary"
	"hash"
)

// NOTE(caleb): These are vars instead of consts to make them easier to use with
// intentional overflow without having to realize them as vars first.
var (
	prime1 uint64 = 11400714785074694791
	prime2 uint64 = 14029467366897019727
	prime3 uint64 = 1609587929392839161
	prime4 uint64 = 9650029242287828579
	prime5 uint64 = 2870177450012600261
)

type xxh struct {
	total int
	v1    uint64
	v2    uint64
	v3    uint64
	v4    uint64
	mem   [32]byte
	n     int // how much of mem is used
}

// Sum64 computes the 64-bit xxHash digest of b.
func Sum64(b []byte) uint64 { return sum64(b) }

func sum64Go(b []byte) uint64 {
	// A simpler version would be
	//   x := New()
	//   x.Write(b)
	//   return x.Sum64()
	// but this is faster, particularly for small inputs.

	n := len(b)
	var h uint64

	if n >= 32 {
		v1 := prime1 + prime2
		v2 := prime2
		v3 := uint64(0)
		v4 := -prime1
		for len(b) >= 32 {
			v1 = round(v1, u64(b[0:8:len(b)]))
			v2 = round(v2, u64(b[8:16:len(b)]))
			v3 = round(v3, u64(b[16:24:len(b)]))
			v4 = round(v4, u64(b[24:32:len(b)]))
			b = b[32:len(b):len(b)]
		}
		h = rotl(v1, 1) + rotl(v2, 7) + rotl(v3, 12) + rotl(v4, 18)
		h = mergeRound(h, v1)
		h = mergeRound(h, v2)
		h = mergeRound(h, v3)
		h = mergeRound(h, v4)
	} else {
		h = prime5
	}

	h += uint64(n)

	i, end := 0, len(b)
	for ; i+8 <= end; i += 8 {
		k1 := round(0, u64(b[i:i+8:len(b)]))
		h ^= k1
		h = rotl(h, 27)*prime1 + prime4
	}
	if i+4 <= end {
		h ^= uint64(u32(b[i:i+4:len(b)])) * prime1
		h = rotl(h, 23)*prime2 + prime3
		i += 4
	}
	for i < end {
		h ^= uint64(b[i]) * prime5
		h = rotl(h, 11) * prime1
		i++
	}

	h ^= h >> 33
	h *= prime2
	h ^= h >> 29
	h *= prime3
	h ^= h >> 32

	return h
}

// New creates a new hash.Hash64 that implements the 64-bit xxHash algorithm.
func New() hash.Hash64 {
	var x xxh
	x.Reset()
	return &x
}

func (x *xxh) Reset() {
	x.n = 0
	x.total = 0
	x.v1 = prime1 + prime2
	x.v2 = prime2
	x.v3 = 0
	x.v4 = -prime1
}

func (x *xxh) Size() int      { return 8 }
func (x *xxh) BlockSize() int { return 32 }

// Write adds more data to x. It always returns len(b), nil.
func (x *xxh) Write(b []byte) (n int, err error) {
	n = len(b)
	x.total += len(b)

	if x.n+len(b) < 32 {
		// This new data doesn't even fill the current block.
		copy(x.mem[x.n:], b)
		x.n += len(b)
		return
	}

	if x.n > 0 {
		// Finish off the partial block.
		copy(x.mem[x.n:], b)
		x.v1 = round(x.v1, u64(x.mem[0:8]))
		x.v2 = round(x.v2, u64(x.mem[8:16]))
		x.v3 = round(x.v3, u64(x.mem[16:24]))
		x.v4 = round(x.v4, u64(x.mem[24:32]))
		b = b[32-x.n:]
		x.n = 0
	}

	if len(b) >= 32 {
		// One or more full blocks left.
		v1, v2, v3, v4 := x.v1, x.v2, x.v3, x.v4
		for len(b) >= 32 {
			v1 = round(v1, u64(b[0:8:len(b)]))
			v2 = round(v2, u64(b[8:16:len(b)]))
			v3 = round(v3, u64(b[16:24:len(b)]))
			v4 = round(v4, u64(b[24:32:len(b)]))
			b = b[32:len(b):len(b)]
		}
		x.v1, x.v2, x.v3, x.v4 = v1, v2, v3, v4
	}

	// Store any remaining partial block.
	copy(x.mem[:], b)
	x.n = len(b)

	return
}

func (x *xxh) Sum(b []byte) []byte {
	s := x.Sum64()
	return append(
		b,
		byte(s>>56),
		byte(s>>48),
		byte(s>>40),
		byte(s>>32),
		byte(s>>24),
		byte(s>>16),
		byte(s>>8),
		byte(s),
	)
}

func (x *xxh) Sum64() uint64 {
	var h uint64

	if x.total >= 32 {
		v1, v2, v3, v4 := x.v1, x.v2, x.v3, x.v4
		h = rotl(v1, 1) + rotl(v2, 7) + rotl(v3, 12) + rotl(v4, 18)
		h = mergeRound(h, v1)
		h = mergeRound(h, v2)
		h = mergeRound(h, v3)
		h = mergeRound(h, v4)
	} else {
		h = x.v3 + prime5
	}

	h += uint64(x.total)

	i, end := 0, x.n
	for ; i+8 <= end; i += 8 {
		k1 := round(0, u64(x.mem[i:i+8]))
		h ^= k1
		h = rotl(h, 27)*prime1 + prime4
	}
	if i+4 <= end {
		h ^= uint64(u32(x.mem[i:i+4])) * prime1
		h = rotl(h, 23)*prime2 + prime3
		i += 4
	}
	for i < end {
		h ^= uint64(x.mem[i]) * prime5
		h = rotl(h, 11) * prime1
		i++
	}

	h ^= h >> 33
	h *= prime2
	h ^= h >> 29
	h *= prime3
	h ^= h >> 32

	return h
}

func u64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }
func u32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

func round(acc, input uint64) uint64 {
	acc += input * prime2
	acc = rotl(acc, 31)
	acc *= prime1
	return acc
}

func mergeRound(acc, val uint64) uint64 {
	val = round(0, val)
	acc ^= val
	acc = acc*prime1 + prime4
	return acc
}

func rotl(x, r uint64) uint64 { return (x << r) | (x >> (64 - r)) }
