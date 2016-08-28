// +build !amd64 appengine !gc noasm

package xxhash

func sum64(b []byte) uint64 { return sum64Go(b) }
