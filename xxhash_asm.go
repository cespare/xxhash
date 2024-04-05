//go:build (amd64 || arm64) && !appengine && gc && !purego
// +build amd64 arm64
// +build !appengine
// +build gc
// +build !purego

package xxhash

//go:noescape
func Sum64WithSeed(b []byte, seed uint64) uint64

//go:noescape
func writeBlocks(d *Digest, b []byte) int
