// +build !appengine
// +build gc
// +build !purego

package xxhash

// TODO(caleb): Fix and re-enable with any ideas I get from
// https://groups.google.com/d/msg/golang-nuts/wb5I2tjrwoc/xCzk6uchBgAJ

//func TestSum64ASM(t *testing.T) {
//        for i := 0; i < 500; i++ {
//                b := make([]byte, i)
//                for j := range b {
//                        b[j] = byte(j)
//                }
//                pureGo := sum64Go(b)
//                asm := Sum64(b)
//                if pureGo != asm {
//                        t.Fatalf("[i=%d] pure go gave 0x%x; asm gave 0x%x", i, pureGo, asm)
//                }
//        }
//}

//func TestWriteBlocksASM(t *testing.T) {
//        d0 := New().(*Digest)
//        d1 := New().(*Digest)
//        for i := 32; i < 500; i++ {
//                b := make([]byte, i)
//                for j := range b {
//                        b[j] = byte(j)
//                }
//                pureGo := writeBlocksGo(d0, b)
//                asm := writeBlocks(d1, b)
//                if !reflect.DeepEqual(pureGo, asm) {
//                        t.Fatalf("[i=%d] pure go gave %v; asm gave %v", i, pureGo, asm)
//                }
//                if !reflect.DeepEqual(d0, d1) {
//                        t.Fatalf("[i=%d] pure go had state %v; asm had state %v", i, d0, d1)
//                }
//        }
//}
