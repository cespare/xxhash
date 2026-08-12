//go:build amd64 && !appengine && gc && !purego
// +build amd64,!appengine,gc,!purego

package xxhash

import "testing"

// forEachImpl runs fn once per code path that this CPU can execute: the plain
// scalar block loop, and the AVX2 and AVX512 block loops if they are available.
//
// Note that the flags are only read by the assembly, which is not running
// concurrently with the test that flips them, so this is safe as long as the
// subtests don't call t.Parallel.
func forEachImpl(t *testing.T, fn func(*testing.T)) {
	t.Helper()

	impls := []struct {
		name          string
		vec, v2, v512 bool
		supported     bool
	}{
		{name: "scalar", supported: true},
		{name: "avx2", vec: true, v2: true, supported: useAVX2},
		{name: "avx512", vec: true, v512: true, supported: useAVX512},
	}

	origVec, origAVX2, origAVX512 := useVec, useAVX2, useAVX512
	defer func() { useVec, useAVX2, useAVX512 = origVec, origAVX2, origAVX512 }()

	ran := 0
	for _, impl := range impls {
		if !impl.supported {
			t.Logf("skipping %s: not supported by this CPU", impl.name)
			continue
		}
		ran++
		useVec, useAVX2, useAVX512 = impl.vec, impl.v2, impl.v512
		t.Run(impl.name, fn)
	}
	if ran == 0 {
		t.Fatal("no implementation was tested")
	}
}

// forEachImplBench is forEachImpl for benchmarks.
func forEachImplBench(b *testing.B, fn func(*testing.B)) {
	b.Helper()

	impls := []struct {
		name          string
		vec, v2, v512 bool
		supported     bool
	}{
		{name: "scalar", supported: true},
		{name: "avx2", vec: true, v2: true, supported: useAVX2},
		{name: "avx512", vec: true, v512: true, supported: useAVX512},
	}

	origVec, origAVX2, origAVX512 := useVec, useAVX2, useAVX512
	defer func() { useVec, useAVX2, useAVX512 = origVec, origAVX2, origAVX512 }()

	for _, impl := range impls {
		if !impl.supported {
			continue
		}
		useVec, useAVX2, useAVX512 = impl.vec, impl.v2, impl.v512
		b.Run(impl.name, fn)
	}
}
