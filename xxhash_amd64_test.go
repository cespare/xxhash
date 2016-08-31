// +build !appengine
// +build gc
// +build !noasm

package xxhash

import (
	"reflect"
	"testing"
)

func TestSum64ASM(t *testing.T) {
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

func TestWriteBlocksASM(t *testing.T) {
	x0 := New().(*xxh)
	x1 := New().(*xxh)
	for i := 32; i < 500; i++ {
		b0 := make([]byte, i)
		for j := range b0 {
			b0[j] = byte(j)
		}
		b1 := b0
		writeBlocksGo(x0, &b0)
		writeBlocks(x1, &b1)
		if !reflect.DeepEqual(b0, b1) {
			t.Fatalf("[i=%d] pure go gave %v; b1 gave %v", i, b0, b1)
		}
		if !reflect.DeepEqual(x0, x1) {
			t.Fatalf("[i=%d] pure go had state %v; b1 had state %v", i, x0, x1)
		}
	}
}
