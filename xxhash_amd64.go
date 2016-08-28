// +build !appengine
// +build gc
// +build !noasm

package xxhash

func sum64(b []byte) uint64
