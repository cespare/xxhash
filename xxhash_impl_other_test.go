//go:build !amd64 || appengine || !gc || purego
// +build !amd64 appengine !gc purego

package xxhash

import "testing"

// forEachImpl runs fn once: on these builds there is only one implementation.
func forEachImpl(t *testing.T, fn func(*testing.T)) {
	t.Helper()
	fn(t)
}

// forEachImplBench is forEachImpl for benchmarks.
func forEachImplBench(b *testing.B, fn func(*testing.B)) {
	b.Helper()
	fn(b)
}
