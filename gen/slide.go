package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

const sumSlideSize = 127
const path = "xxhash_slide.go"

func slide() error {
	w := &bytes.Buffer{}

	fmt.Fprintf(w, `//go:build go1.17
// +build go1.17

package xxhash

import "math/bits"

// Generated by gen/slide.go. DO NOT EDIT.

const slideLength = %d

// Handles length 0-%d bytes using sum slides.
func slide(b []byte) uint64 {
	// This function use sum slides, theses are straight unrolled pieces of code which compute hashes, with middle jumps.
	// Each do not contain any conditions to make them trivial for the CPU to parse and never cause any pipeline flushes after the first jump table.
	// We need 32 different slides to cover each offset into the 32 block size. The trailing 32 bytes are handled by their own slides which are shared and reused by the higher slides.
	// The trailing 32 bytes slides are reused for each offset. The CPUs we care about can always correctly read unconditional jumps without causing a pipeline flush.

	// This function is written more like an optimized assembly routine, except we trick the compiler into generating good code by generating the slide ourself.
	// Using the go compiler make the call overhead cheaper since it will use the unstable ABIInternal passing through registers.
	// They are also extremely effective when hashing multiple values of the same size back to back.
	// Assumptions of this strategy:
	// - All the state except b's array will be correctly register allocated.
	//   It probably generate unnecessary MOVs but the critical path includes LAT3 multiplies for each block, so there is plenty of time to dispatch renames.
	// - The compiler is basic block based and will do a good enough job at layout. This is true for some the go compiler, llvm and some of gcc.
	//   This means I make very liberal use of goto, they shouldn't be red as JMPs but abstract basic blocks links.
	// - The compiler has some SSA passes.
	//   This is used for all the b_* tricks.
	// - We can't trust the compiler to inline anything (except intrinsics).

	// Setup variables here since go doesn't want use to do dangerous gotos.
	v1 := prime1
	v1 += prime2
	v2 := prime2
	v3 := uint64(0)
	v4 := prime1
	v4 = -v4
	h := prime5
	n := uint64(len(b))

	// The go compiler has multiple oversight in the propragation of proofs through Phi nodes. Using array pointers is a very unsubtle hint and compiles to nothing.
	// Because we assume the compiler has some low level SSA passes this is otherwise free.
	var (
`, sumSlideSize, sumSlideSize)
	for i := 0; i <= sumSlideSize; i++ {
		fmt.Fprintf(w, "\t\tb_%d *[%d]byte\n", i, i)
	}

	w.WriteString(`	)

	// Jump table to various positions in the slide, this setups proofs for bounds checks.
	// From then on it need to make sure to maintain constance in the length of b.
	switch len(b) {
	case 0:
		// Handle this appart because it can be completely folded.
		h += n
		h ^= h >> 33
		h *= prime2
		h ^= h >> 29
		h *= prime3
		h ^= h >> 32
		return h
`)
	for i := 1; i <= sumSlideSize; i++ {
		fmt.Fprintf(w, `	case %d:
		b_%d = (*[%d]byte)(b)
		goto sz_%d
`, i, i, i, i)
	}
	w.WriteString(`	default:
		panic("unreachable; slide overflow")
	}

	// Theses are the main slides, they handle 32 bytes 4 × 8 bytes at a time using ILP.
`)
	// POTENTIAL OPTIMIZATION: We could use a single slide and shuffle v{1,2,3,4} based on offset. This would make setup and transition into trailer more expensive but codesize would be smaller and some i-cache reuse would be certain to happen for anything touching it.

	for k := range 32 {
		i := sumSlideSize - k
		for ; i >= 32; i -= 32 {
			fmt.Fprintf(w, `sz_%d:
	{
		b := b_%d[:]
		var load uint64
`, i, i)
			for r := 1; r <= 4; r++ {
				fmt.Fprintf(w, `		load = %s
		b = b[8:]
`, load64)
				genRound(w, 2, fmt.Sprintf("v%d", r), "load")
			}
			fmt.Fprintf(w, `b_%d = (*[%d]byte)(b_%d[32:])
	}

`, i-32, i-32, i)
			// POTENTIAL OPTIMIZATION: b[32:] creates an addition to bump the pointer which means the address dependency on the memory loads is not resolved before the jmp table. I know two fixes:
			// - change b to a pointer to the end of the slice and subtract the total offset. I don't know how to do this in pure go.
			// - don't bother reusing the slides, this means each load instruction can hardcode the offset. Make the code significantly bigger and i-cache worst, altho I didn't tried it.
		}
		w.WriteString(`	h = bits.RotateLeft64(v1, 1) + bits.RotateLeft64(v2, 7) + bits.RotateLeft64(v3, 12) + bits.RotateLeft64(v4, 18)
`)
		for r := 1; r <= 4; r++ {
			genMergeRound(w, 1, "h", fmt.Sprintf("v%d", r))
		}
		w.WriteString("\n")
		if i != 0 { // Avoid « label sz_0 defined and not used », case 0 shortcuts with a precomputed value.
			fmt.Fprintf(w, "sz_%d:\n", i)
		}
		fmt.Fprintf(w, `	h += n
	goto sz_%dl

`, i)
	}

	w.WriteString("	// Theses are 8 bytes block trailing slides.\n")
	for k := range 8 {
		i := 31 - k
		for ; i >= 8; i -= 8 {
			fmt.Fprintf(w, `sz_%dl:
	{
		b := b_%d[:]
		load := %s
		var temp uint64
`, i, i, load64)
			genRound(w, 2, "temp", "load")
			fmt.Fprintf(w, `h ^= temp
		h = bits.RotateLeft64(h, 27)*prime1 + prime4
		b_%d = (*[%d]byte)(b_%d[8:])
	}

`, i-8, i-8, i)
		}
		fmt.Fprintf(w, `goto sz_%dl

`, i)
	}

	w.WriteString("	// Theses are the 4 bytes trailing slides.\n")
	for k := range 4 {
		i := 7 - k
		fmt.Fprintf(w, `sz_%dl:
	{
		b := b_%d[:]
		load := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24 // Work around for go.dev/issue/68081.
		h ^= uint64(load) * prime1
		h = bits.RotateLeft64(h, 23)*prime2 + prime3
		b_%d = (*[%d]byte)(b_%d[4:])
		goto sz_%dl
	}

`, i, i, i-4, i-4, i, i-4)
	}

	w.WriteString("	// This is the 1 bytes trailing slide.\n")
	for i := 4; i > 1; {
		i--
		fmt.Fprintf(w, `sz_%dl:
	h ^= uint64(b_%d[0]) * prime5
	h = bits.RotateLeft64(h, 11) * prime1
	b_%d = (*[%d]byte)(b_%d[1:])

`, i, i, i-1, i-1, i)
	}
	// Carefull here, the loop above fallthrough to zero.

	w.WriteString(`	// Finally the terminator.
sz_0l:
	_ = b_0 // this avoids a bunch of if i != 0 { in codegen and is optimized away.

	h ^= h >> 33
	h *= prime2
	h ^= h >> 29
	h *= prime3
	h ^= h >> 32

	return h
}
`)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = w.WriteTo(f)
	if err != nil {
		os.Remove(path)
		return err
	}
	err = f.Close()
	if err != nil {
		os.Remove(path)
		return err
	}

	return nil
}

func genRound(w *bytes.Buffer, tab uint, acc, input string) {
	tabs := strings.Repeat("\t", int(tab))
	fmt.Fprintf(w, `%s%s += %s * prime2
%s%s = bits.RotateLeft64(%s, 31)
%s%s *= prime1
`, tabs, acc, input, tabs, acc, acc, tabs, acc)
}

func genMergeRound(w *bytes.Buffer, tab uint, acc, val string) {
	tabs := strings.Repeat("\t", int(tab))
	tab++
	tabsp := strings.Repeat("\t", int(tab))
	fmt.Fprintf(w, `%s{
%svar temp uint64
`, tabs, tabsp)
	genRound(w, tab, "temp", val)
	fmt.Fprintf(w, `%s%s ^= temp
%s%s = %s*prime1 + prime4
%s}
`, tabsp, acc, tabsp, acc, acc, tabs)
}

const load64 = "uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 | uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56 // Work around for go.dev/issue/68081."
