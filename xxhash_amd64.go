// +build !appengine
// +build gc
// +build !noasm

package xxhash

func sum64(b []byte) uint64

func writeBlocks(x *xxh, b []byte) []byte
