//go:build amd64 && !appengine && gc && !purego && go1.22
// +build amd64,!appengine,gc,!purego,go1.22

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
//
//go:noescape
func Sum64(b []byte) uint64

// extra is a first block before b, it may be nil then skip it.
//
//go:noescape
func writeBlocks(d *Digest, extra *[32]byte, b []byte)
