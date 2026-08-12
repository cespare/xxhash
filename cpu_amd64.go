//go:build amd64 && !appengine && gc && !purego
// +build amd64,!appengine,gc,!purego

package xxhash

// CPU feature flags used to select the fastest available implementation of the
// 32-byte block loop. They are read directly by the assembly code.
//
// The variables are only written once, from init, before any hashing happens.
var (
	useVec    bool // useAVX2 || useAVX512
	useAVX2   bool
	useAVX512 bool
)

// vecPrime2 holds the two halves of prime2, which the AVX2 block loop uses to
// synthesize a 64-bit multiply out of VPMULUDQ.
var vecPrime2 = [2]uint64{prime2 & 0xffffffff, prime2 >> 32}

// sum64Vec and writeBlocksVec are the vectorized counterparts of Sum64 and
// writeBlocks. They are only reached by a tail jump from the assembly of those
// two functions, never called from Go, but they need declarations here so that
// vet can check their argument references.

//go:noescape
func sum64Vec(b []byte) uint64

//go:noescape
func writeBlocksVec(d *Digest, b []byte) int

//go:noescape
func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)

//go:noescape
func xgetbv() (eax, edx uint32)

func init() {
	maxID, _, _, _ := cpuid(0, 0)
	if maxID < 7 {
		return
	}

	_, _, ecx1, _ := cpuid(1, 0)
	const (
		osxsaveBit = 1 << 27
		avxBit     = 1 << 28
	)
	if ecx1&(osxsaveBit|avxBit) != osxsaveBit|avxBit {
		return
	}

	// Check that the OS saves and restores the vector register state. Bits 1
	// and 2 cover XMM and YMM; bits 5, 6 and 7 add the opmask registers, the
	// upper halves of ZMM0-15 and ZMM16-31.
	xcr0, _ := xgetbv()
	const (
		xmmYmmState = 1<<1 | 1<<2
		zmmState    = 1<<5 | 1<<6 | 1<<7
	)
	if xcr0&xmmYmmState != xmmYmmState {
		return
	}

	_, ebx7, _, _ := cpuid(7, 0)
	const (
		avx2Bit     = 1 << 5
		avx512fBit  = 1 << 16
		avx512dqBit = 1 << 17
		avx512vlBit = 1 << 31
	)
	useAVX2 = ebx7&avx2Bit != 0

	// The vector block loop needs VPMULLQ (AVX512DQ) on 256-bit vectors
	// (AVX512VL).
	const avx512Bits = avx512fBit | avx512dqBit | avx512vlBit
	useAVX512 = xcr0&zmmState == zmmState && ebx7&avx512Bits == avx512Bits

	useVec = useAVX2 || useAVX512
}
