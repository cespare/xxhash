//go:build !go1.17
// +build !go1.17

package xxhash

const slideLength = 0

func slide(b []byte) uint64 {
	panic("unreachable")
}
