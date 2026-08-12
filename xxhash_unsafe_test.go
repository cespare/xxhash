//go:build !appengine
// +build !appengine

package xxhash

import (
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

func TestStringAllocs(t *testing.T) {
	longStr := strings.Repeat("a", 1000)
	t.Run("Sum64String", func(t *testing.T) {
		testAllocs(t, func() {
			sink = Sum64String(longStr)
		})
	})
	t.Run("Digest.WriteString", func(t *testing.T) {
		testAllocs(t, func() {
			d := New()
			d.WriteString(longStr)
			sink = d.Sum64()
		})
	})
}

// This test is inspired by the Go runtime tests in https://go.dev/cl/57410.
// It asserts that certain important functions may be inlined.
func TestInlining(t *testing.T) {
	funcs := map[string]struct{}{
		"Sum64String":           {},
		"(*Digest).WriteString": {},
	}

	// Build rather than test: the inlining decisions are all we need, and
	// building doesn't run anything, so this still works when the test itself
	// is running under an emulator for another GOARCH.
	cmd := exec.Command("go", "build", "-gcflags=-m", "-o", os.DevNull, ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Log(string(out))
		t.Fatal(err)
	}

	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Split(line, ": can inline")
		if len(parts) < 2 {
			continue
		}
		delete(funcs, strings.TrimSpace(parts[1]))
	}

	var failed []string
	for fn := range funcs {
		failed = append(failed, fn)
	}
	sort.Strings(failed)
	for _, fn := range failed {
		t.Errorf("function %s not inlined", fn)
	}
}
