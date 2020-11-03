// +build !amd64 !appengine,purego

package xxhash

// Sum64String computes the 64-bit xxHash digest of s.
// It may be faster than Sum64([]byte(s)) by avoiding a copy.
func Sum64String(s string) uint64 {
	// Forward to the version in xxhash_unsafe.go.
	// Go 1.15 inlines this function.
	return sum64String(s)
}
