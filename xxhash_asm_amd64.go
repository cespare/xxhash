//go:build amd64 && !appengine && gc && !purego
// +build amd64,!appengine,gc,!purego

//go:generate sh -c "cd gen && go run -tags purego . -out xxhash_avx512_amd64.s"

package xxhash

import "github.com/klauspost/cpuid/v2"

var useAvx512 = cpuid.CPU.Supports(
	cpuid.AVX,
	cpuid.AVX2,
	cpuid.AVX512DQ,
	cpuid.AVX512F,
	cpuid.AVX512VL,
	cpuid.BMI1,

// Today, vectorized 64 bits integer multiples positively sucks on intel,
// with ILP a single scalar unit is multiple times faster.
// This means sometime we wont be using the AVX512 when under virtualization
// because vendor will be the hypervisor, but in my experience that is rare.
// Most virtualization setups defaults to reporting the vendorid of the host.
) && cpuid.CPU.IsVendor(cpuid.AMD)

// Sum64 computes the 64-bit xxHash digest of b with a zero seed.
func Sum64(b []byte) uint64 {
	if useAvx512 {
		return sum64Avx512(b)
	}
	return sum64Scalar(b)
}

//go:noescape
func sum64Scalar(b []byte) uint64

//go:noescape
func sum64Avx512(b []byte) uint64

// extra is a first block before b, it may be nil then skip it.
func writeBlocks(d *Digest, extra *[32]byte, b []byte) {
	if useAvx512 {
		writeBlocksAvx512(&d.s, extra, b)
		return
	}
	writeBlocksScalar(d, nil, b)
}

//go:noescape
func writeBlocksAvx512(d *[4]uint64, extra *[32]byte, b []byte)

//go:noescape
func writeBlocksScalar(d *Digest, extra *[32]byte, b []byte)
