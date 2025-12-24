//go:build arm64 && !appengine && gc && !purego
// +build arm64,!appengine,gc,!purego

package xxhash

// Sum64 computes the 64-bit xxHash digest of b with a zero seed.
//
//go:noescape
func Sum64(b []byte) uint64

//go:noescape
func writeBlocks(d *Digest, b []byte) int

func BatchSum64String(src []string, dst []uint64) int64 {
	if len(src) != len(dst) {
		return 0
	}
	for i := 0; i < len(src); i++ {
		dst[i] = Sum64String(src[i])
	}
	return int64(len(src))
}
