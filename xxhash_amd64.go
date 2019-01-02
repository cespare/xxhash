// +build !appengine
// +build gc
// +build !purego

package xxhash

//go:noescape
func sum64(b []byte) uint64

//go:noescape
func writeBlocks(*Digest, []byte) int
