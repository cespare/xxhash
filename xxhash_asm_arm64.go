//go:build arm64 && !appengine && gc && !purego
// +build arm64,!appengine,gc,!purego

package xxhash

var useAvx512 = false

// Sum64 computes the 64-bit xxHash digest of b with a zero seed.
//
//go:noescape
func Sum64(b []byte) uint64

//go:noescape
func writeBlocks(d *Digest, b []byte) int
