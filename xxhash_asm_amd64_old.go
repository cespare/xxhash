//go:build amd64 && !appengine && gc && !purego && !go1.22
// +build amd64,!appengine,gc,!purego,!go1.22

// The avx512 impl relies on PCALIGN.

package xxhash

// Sum64 computes the 64-bit xxHash digest of b with a zero seed.
func Sum64(b []byte) uint64 {
	return sum64Scalar(b)
}

//go:noescape
func sum64Scalar(b []byte) uint64

// extra is a first block before b, it may be nil then skip it.
func writeBlocks(d *Digest, extra *[32]byte, b []byte) {
	return writeBlocksScalar(d, extra, b)
}

//go:noescape
func writeBlocksScalar(d *Digest, extra *[32]byte, b []byte)
