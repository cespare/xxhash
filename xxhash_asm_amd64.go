//go:build amd64 && !appengine && gc && !purego
// +build amd64,!appengine,gc,!purego

//go:generate go run ./gen -out xxhash_avx512_amd64.s

package xxhash

import "github.com/klauspost/cpuid/v2"

var useAvx512 = cpuid.CPU.Supports(cpuid.AVX, cpuid.AVX2, cpuid.AVX512DQ, cpuid.AVX512F, cpuid.AVX512VL, cpuid.BMI1)

// Sum64 computes the 64-bit xxHash digest of b with a zero seed.
func Sum64(b []byte) uint64 {
	if useAvx512 {
		return sum64avx512(b)
	}
	return sum64(b)
}

//go:noescape
func sum64(b []byte) uint64

//go:noescape
func sum64avx512(b []byte) uint64

//go:noescape
func writeBlocks(d *Digest, b []byte) int
