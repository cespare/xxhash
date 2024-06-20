//go:build arm64 && !appengine && gc && !purego
// +build arm64,!appengine,gc,!purego

package xxhash

var useAvx512 = false // used in tests

// Sum64 computes the 64-bit xxHash digest of b with a zero seed.
//
//go:noescape
func Sum64(b []byte) uint64

// extra is a first block before b, it may be nil then skip it.
func writeBlocks(d *Digest, extra *[32]byte, b []byte) {
	if extra != nil {
		// FIXME: handle that logic in ASM, *someone* was lazy and didn't
		// cared to learn the arm64 p9 syntax.
		// At least this is hopefully on par with how fast the software impl
		// it used to be.
		writeBlocksArm64(d, extra[:])
	}
	writeBlocksArm64(d, b)
}

//go:noescape
func writeBlocksArm64(d *Digest, b []byte)
