// +build !appengine
// +build gc
// +build !noasm

package xxhash

import "testing"

func TestASM(t *testing.T) {
	for i := 0; i < 500; i++ {
		b := make([]byte, i)
		for j := range b {
			b[j] = byte(j)
		}
		pureGo := sum64Go(b)
		asm := sum64(b)
		if pureGo != asm {
			t.Fatalf("[i=%d] pure go gave 0x%x; asm gave 0x%x", i, pureGo, asm)
		}
	}
}
