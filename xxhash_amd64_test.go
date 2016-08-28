// +build !appengine
// +build gc
// +build !noasm

package xxhash

import (
	"fmt"
	"testing"
)

func TestFoo(t *testing.T) {
	b := make([]byte, 32)
	for i := range b {
		b[i] = 'a' + byte(i)
	}
	fmt.Println(sum64Go(b))
	fmt.Println(sum64(b))
}

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
