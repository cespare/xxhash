//go:build !appengine && gc && !purego
// +build !appengine
// +build gc
// +build !purego

#include "textflag.h"

// Registers:
#define h      AX
#define dig    AX // *Digest
#define p      SI // pointer to advance through b
#define n      DX
#define nb     DL // low byte of n, used by the length bit tests
#define end    BX // loop end
#define vend   CX // end of the vectorized region
#define v1     R8
#define v2     R9
#define v3     R10
#define v4     R11
#define x      R12
#define prime1 R13
#define prime2 R14
#define prime4 DI

#define round(acc, x) \
	IMULQ prime2, x   \
	ADDQ  x, acc      \
	ROLQ  $31, acc    \
	IMULQ prime1, acc

// round0 performs the operation x = round(0, x).
#define round0(x) \
	IMULQ prime2, x \
	ROLQ  $31, x    \
	IMULQ prime1, x

// mergeRound applies a merge round on the two registers acc and x.
// It assumes that prime1, prime2, and prime4 have been loaded.
#define mergeRound(acc, x) \
	round0(x)         \
	XORQ  x, acc      \
	IMULQ prime1, acc \
	ADDQ  prime4, acc

// initState sets up v1, v2, v3, and v4 for a zero seed.
// It assumes that prime1 and prime2 have been loaded.
#define initState() \
	MOVQ prime1, v1 \
	ADDQ prime2, v1 \
	MOVQ prime2, v2 \
	XORQ v3, v3     \
	XORQ v4, v4     \
	SUBQ prime1, v4

// mergeState folds v1, v2, v3, and v4 into h.
// It assumes that prime1, prime2, and prime4 have been loaded.
#define mergeState() \
	MOVQ v1, h        \
	ROLQ $1, h        \
	MOVQ v2, x        \
	ROLQ $7, x        \
	ADDQ x, h         \
	MOVQ v3, x        \
	ROLQ $12, x       \
	ADDQ x, h         \
	MOVQ v4, x        \
	ROLQ $18, x       \
	ADDQ x, h         \
	mergeRound(h, v1) \
	mergeRound(h, v2) \
	mergeRound(h, v3) \
	mergeRound(h, v4)

// blockLoop processes as many 32-byte blocks as possible,
// updating v1, v2, v3, and v4. It assumes that there is at least one block
// to process.
#define blockLoop() \
loop:  \
	MOVQ +0(p), x  \
	round(v1, x)   \
	MOVQ +8(p), x  \
	round(v2, x)   \
	MOVQ +16(p), x \
	round(v3, x)   \
	MOVQ +24(p), x \
	round(v4, x)   \
	ADDQ $32, p    \
	CMPQ p, end    \
	JLE  loop

// tail folds the final n&31 bytes at p into h and then applies the final
// avalanche. Rather than looping, it tests the low bits of the length one at a
// time: the branches then depend only on len(b), so hashing a stream of
// same-sized inputs mispredicts nothing, and the two 8-byte rounds of a 16-to-31
// byte tail issue back to back instead of through a loop.
//
// It assumes that prime1, prime2, and prime4 have been loaded and that h
// already includes the length.
#define tail() \
	TESTB $24, nb           \
	JEQ   tail4             \
	TESTB $16, nb           \
	JEQ   tail8             \
	MOVQ  +0(p), x          \
	round0(x)               \
	XORQ  x, h              \
	ROLQ  $27, h            \
	IMULQ prime1, h         \
	ADDQ  prime4, h         \
	MOVQ  +8(p), x          \
	round0(x)               \
	XORQ  x, h              \
	ROLQ  $27, h            \
	IMULQ prime1, h         \
	ADDQ  prime4, h         \
	ADDQ  $16, p            \
tail8:                      \
	TESTB $8, nb            \
	JEQ   tail4             \
	MOVQ  +0(p), x          \
	round0(x)               \
	XORQ  x, h              \
	ROLQ  $27, h            \
	IMULQ prime1, h         \
	ADDQ  prime4, h         \
	ADDQ  $8, p             \
tail4:                      \
	TESTB $4, nb            \
	JEQ   tailBytes         \
	MOVL  +0(p), x          \
	IMULQ prime1, x         \
	XORQ  x, h              \
	ROLQ  $23, h            \
	IMULQ prime2, h         \
	ADDQ  ·primes+16(SB), h \
	ADDQ  $4, p             \
tailBytes:                  \
	TESTB $3, nb            \
	JEQ   finalize          \
	TESTB $2, nb            \
	JEQ   tail1             \
	MOVBQZX +0(p), x        \
	IMULQ ·primes+32(SB), x \
	XORQ  x, h              \
	ROLQ  $11, h            \
	IMULQ prime1, h         \
	MOVBQZX +1(p), x        \
	IMULQ ·primes+32(SB), x \
	XORQ  x, h              \
	ROLQ  $11, h            \
	IMULQ prime1, h         \
	ADDQ  $2, p             \
tail1:                      \
	TESTB $1, nb            \
	JEQ   finalize          \
	MOVBQZX +0(p), x        \
	IMULQ ·primes+32(SB), x \
	XORQ  x, h              \
	ROLQ  $11, h            \
	IMULQ prime1, h         \
finalize:                   \
	MOVQ  h, x              \
	SHRQ  $33, x            \
	XORQ  x, h              \
	IMULQ prime2, h         \
	MOVQ  h, x              \
	SHRQ  $29, x            \
	XORQ  x, h              \
	IMULQ ·primes+16(SB), h \
	MOVQ  h, x              \
	SHRQ  $32, x            \
	XORQ  x, h

// The vectorized block loops below split each round
//
//	acc = rol31(acc + x*prime2) * prime1
//
// into the half that only depends on the input, x*prime2, and the half that
// carries the accumulator dependency.
//
// The plain block loop is limited by 64-bit multiply throughput: it needs eight
// IMULQs per 32-byte block and x86 cores can only start one per cycle, which is
// the ~8 cycles per block it achieves. The four lanes of a block are
// independent and a block's worth of products fits in one 256-bit vector, so
// the vector units can supply x*prime2 while the scalar half runs closer to its
// own dependency-chain limit of 5 cycles per block: add, rotate, multiply.
//
// The products go through a small stack buffer; extracting them from the vector
// registers would cost more than the round trip through L1. The loop is
// software pipelined by one group, so a product is read a whole group after it
// is written, and the two halves are interleaved instruction by instruction so
// that the scalar and vector multiplies stay spread across the execution ports.

// vecBlocks is the number of 32-byte blocks converted at a time. vecGroupSize
// must be a power of two, since the loop bound is computed with a mask.
//
// sumCutoff and writeCutoff are the lengths below which Sum64 and writeBlocks
// don't enter the vector path at all: below them the setup (the group buffer,
// the broadcast constants, and the pipeline prologue) costs more than the loop
// saves. Both must be at least vecGroupSize, since the pipeline prologue reads
// a whole group before the first bound is tested.
//
// All three were tuned by measurement. The group stays at four blocks: two
// costs 5% at 1 KB and eight costs 7% at 512 bytes.
//
// The two cutoffs differ because the crossover does. Forcing each kernel at a
// fixed length on a Redwood Cove P-core puts Sum64's just under 224 -- the
// vector loop is 5.5% ahead there, level at 192 and 3.8% behind at 160 -- but
// writeBlocks' somewhere past 256, where the vector loop is still 2% behind at
// 255 and 4% behind at 256, and only 4% ahead by 384. That gap is not a draw in
// the 4K-aliasing lottery: it holds at every input offset from 0 to 3072. So
// Sum64 takes the lower crossover and writeBlocks keeps the 256 that was tuned
// on a Zen 4, which is the one number here no measurement has argued down.
#define vecBlocks    4
#define vecGroupSize 128 // vecBlocks * 32
#define sumCutoff    224
#define writeCutoff  256

// prodAVX2 computes the four x*prime2 products of the block at off(p) and
// stores them at off(SP).
//
// AVX2 has no 64x64 multiply, so it is synthesized from three VPMULUDQs:
//
//	x*P = xlo*Plo + ((xlo*Phi + xhi*Plo) << 32)   (mod 2^64)
//
// where Y0 broadcasts the low half of prime2 and Y1 the high half.
#define prodAVX2(off) \
	VMOVDQU  off(p), Y2  \
	VPSRLQ   $32, Y2, Y3 \
	VPMULUDQ Y0, Y2, Y4  \
	VPMULUDQ Y0, Y3, Y3  \
	VPMULUDQ Y1, Y2, Y5  \
	VPADDQ   Y5, Y3, Y3  \
	VPSLLQ   $32, Y3, Y3 \
	VPADDQ   Y4, Y3, Y3  \
	VMOVDQU  Y3, off(SP)

// prodAVX512 is prodAVX2 using the AVX512DQ 64-bit multiply, with Y0
// broadcasting prime2. It works on 256-bit vectors (AVX512VL) rather than
// 512-bit ones: a block is 256 bits wide and consecutive blocks are dependent,
// so the extra width would buy nothing but the frequency penalty that wide
// multiplies carry on some implementations.
#define prodAVX512(off) \
	VMOVDQU off(p), Y2 \
	VPMULLQ Y0, Y2, Y2 \
	VMOVDQU Y2, off(SP)

// consume applies the accumulator half of the round to the four products
// stored at off(SP). It assumes that prime1 has been loaded.
#define consume(off) \
	ADDQ  off+0(SP), v1  \
	ROLQ  $31, v1        \
	IMULQ prime1, v1     \
	ADDQ  off+8(SP), v2  \
	ROLQ  $31, v2        \
	IMULQ prime1, v2     \
	ADDQ  off+16(SP), v3 \
	ROLQ  $31, v3        \
	IMULQ prime1, v3     \
	ADDQ  off+24(SP), v4 \
	ROLQ  $31, v4        \
	IMULQ prime1, v4

// stepAVX2 is consume(off) followed by prodAVX2(off), interleaved. The store
// stays last: it overwrites the slot that the four loads above just read.
#define stepAVX2(off) \
	VMOVDQU  off(p), Y2     \
	ADDQ     off+0(SP), v1  \
	ROLQ     $31, v1        \
	IMULQ    prime1, v1     \
	VPSRLQ   $32, Y2, Y3    \
	VPMULUDQ Y0, Y2, Y4     \
	ADDQ     off+8(SP), v2  \
	ROLQ     $31, v2        \
	IMULQ    prime1, v2     \
	VPMULUDQ Y0, Y3, Y3     \
	VPMULUDQ Y1, Y2, Y5     \
	VPADDQ   Y5, Y3, Y3     \
	ADDQ     off+16(SP), v3 \
	ROLQ     $31, v3        \
	IMULQ    prime1, v3     \
	VPSLLQ   $32, Y3, Y3    \
	VPADDQ   Y4, Y3, Y3     \
	ADDQ     off+24(SP), v4 \
	ROLQ     $31, v4        \
	IMULQ    prime1, v4     \
	VMOVDQU  Y3, off(SP)

// stepAVX512 is stepAVX2 for the AVX512DQ multiply.
#define stepAVX512(off) \
	VMOVDQU off(p), Y2     \
	ADDQ    off+0(SP), v1  \
	ROLQ    $31, v1        \
	IMULQ   prime1, v1     \
	VPMULLQ Y0, Y2, Y2     \
	ADDQ    off+8(SP), v2  \
	ROLQ    $31, v2        \
	IMULQ   prime1, v2     \
	ADDQ    off+16(SP), v3 \
	ROLQ    $31, v3        \
	IMULQ   prime1, v3     \
	ADDQ    off+24(SP), v4 \
	ROLQ    $31, v4        \
	IMULQ   prime1, v4     \
	VMOVDQU Y2, off(SP)

#define consumeGroup() \
	consume(0)  \
	consume(32) \
	consume(64) \
	consume(96)

#define prodGroupAVX2() \
	prodAVX2(0)  \
	prodAVX2(32) \
	prodAVX2(64) \
	prodAVX2(96)

#define prodGroupAVX512() \
	prodAVX512(0)  \
	prodAVX512(32) \
	prodAVX512(64) \
	prodAVX512(96)

#define stepGroupAVX2() \
	stepAVX2(0)  \
	stepAVX2(32) \
	stepAVX2(64) \
	stepAVX2(96)

#define stepGroupAVX512() \
	stepAVX512(0)  \
	stepAVX512(32) \
	stepAVX512(64) \
	stepAVX512(96)

// vecSetup points vend at the end of the last full group.
#define vecSetup() \
	MOVQ n, vend              \
	ANDQ $-vecGroupSize, vend \
	ADDQ p, vend

// func Sum64(b []byte) uint64
TEXT ·Sum64(SB), NOSPLIT|NOFRAME, $0-32
	// Load slice.
	MOVQ b_base+0(FP), p
	MOVQ b_len+8(FP), n

	// Load fixed primes.
	MOVQ ·primes+0(SB), prime1
	MOVQ ·primes+8(SB), prime2
	MOVQ ·primes+24(SB), prime4

	// Check whether we have at least one block.
	CMPQ n, $32
	JB   noBlocks

	// Long inputs are handed off to the vectorized implementation.
	CMPQ n, $sumCutoff
	JB   scalarBlocks
	CMPB ·useVec(SB), $0
	JEQ  scalarBlocks
	JMP  ·sum64Vec(SB)

scalarBlocks:
	// The loop limit is len(b)-32.
	LEAQ (p)(n*1), end
	SUBQ $32, end

	initState()
	blockLoop()
	mergeState()

	JMP afterBlocks

noBlocks:
	MOVQ ·primes+32(SB), h

afterBlocks:
	ADDQ n, h
	tail()

	MOVQ h, ret+24(FP)
	RET

// func sum64Vec(b []byte) uint64
//
// sum64Vec is Sum64 for inputs of at least sumCutoff bytes. It is reached by a
// tail jump from Sum64, which stays frameless so that short inputs don't pay
// for reserving the group buffer.
TEXT ·sum64Vec(SB), NOSPLIT, $vecGroupSize-32
	MOVQ b_base+0(FP), p
	MOVQ b_len+8(FP), n

	MOVQ ·primes+0(SB), prime1
	MOVQ ·primes+8(SB), prime2
	MOVQ ·primes+24(SB), prime4

	initState()
	vecSetup()

	CMPB ·useAVX512(SB), $0
	JNE  avx512Start

	VPBROADCASTQ ·vecPrime2+0(SB), Y0
	VPBROADCASTQ ·vecPrime2+8(SB), Y1

	prodGroupAVX2()
	ADDQ $vecGroupSize, p
	CMPQ p, vend
	JAE  avx2Last

avx2Loop:
	stepGroupAVX2()
	ADDQ $vecGroupSize, p
	CMPQ p, vend
	JB   avx2Loop

avx2Last:
	consumeGroup()
	VZEROUPPER
	JMP vecDone

avx512Start:
	VPBROADCASTQ ·primes+8(SB), Y0

	prodGroupAVX512()
	ADDQ $vecGroupSize, p
	CMPQ p, vend
	JAE  avx512Last

avx512Loop:
	stepGroupAVX512()
	ADDQ $vecGroupSize, p
	CMPQ p, vend
	JB   avx512Loop

avx512Last:
	consumeGroup()
	VZEROUPPER

vecDone:
	// Any whole blocks left over after the last full group.
	MOVQ b_base+0(FP), end
	ADDQ n, end
	SUBQ $32, end
	CMPQ p, end
	JA   afterBlocks

	blockLoop()

afterBlocks:
	mergeState()

	ADDQ n, h
	tail()

	MOVQ h, ret+24(FP)
	RET

// func writeBlocks(d *Digest, b []byte) int
TEXT ·writeBlocks(SB), NOSPLIT|NOFRAME, $0-40
	MOVQ b_len+16(FP), n

	CMPQ n, $writeCutoff
	JB   scalarBlocks
	CMPB ·useVec(SB), $0
	JEQ  scalarBlocks
	JMP  ·writeBlocksVec(SB)

scalarBlocks:
	// Load fixed primes needed for round.
	MOVQ ·primes+0(SB), prime1
	MOVQ ·primes+8(SB), prime2

	// Load slice.
	MOVQ b_base+8(FP), p
	LEAQ (p)(n*1), end
	SUBQ $32, end

	// Load vN from d.
	MOVQ d+0(FP), dig
	MOVQ 0(dig), v1
	MOVQ 8(dig), v2
	MOVQ 16(dig), v3
	MOVQ 24(dig), v4

	// We don't need to check the loop condition here; this function is
	// always called with at least one block of data to process.
	blockLoop()

	// Copy vN back to d.
	MOVQ v1, 0(dig)
	MOVQ v2, 8(dig)
	MOVQ v3, 16(dig)
	MOVQ v4, 24(dig)

	// The number of bytes written is the number of whole blocks.
	ANDQ $-32, n
	MOVQ n, ret+32(FP)

	RET

// func writeBlocksVec(d *Digest, b []byte) int
//
// writeBlocksVec is writeBlocks for inputs of at least writeCutoff bytes.
TEXT ·writeBlocksVec(SB), NOSPLIT, $vecGroupSize-40
	MOVQ ·primes+0(SB), prime1
	MOVQ ·primes+8(SB), prime2

	MOVQ b_base+8(FP), p
	MOVQ b_len+16(FP), n

	MOVQ d+0(FP), dig
	MOVQ 0(dig), v1
	MOVQ 8(dig), v2
	MOVQ 16(dig), v3
	MOVQ 24(dig), v4

	vecSetup()

	CMPB ·useAVX512(SB), $0
	JNE  avx512Start

	VPBROADCASTQ ·vecPrime2+0(SB), Y0
	VPBROADCASTQ ·vecPrime2+8(SB), Y1

	prodGroupAVX2()
	ADDQ $vecGroupSize, p
	CMPQ p, vend
	JAE  avx2Last

avx2Loop:
	stepGroupAVX2()
	ADDQ $vecGroupSize, p
	CMPQ p, vend
	JB   avx2Loop

avx2Last:
	consumeGroup()
	VZEROUPPER
	JMP vecDone

avx512Start:
	VPBROADCASTQ ·primes+8(SB), Y0

	prodGroupAVX512()
	ADDQ $vecGroupSize, p
	CMPQ p, vend
	JAE  avx512Last

avx512Loop:
	stepGroupAVX512()
	ADDQ $vecGroupSize, p
	CMPQ p, vend
	JB   avx512Loop

avx512Last:
	consumeGroup()
	VZEROUPPER

vecDone:
	// Any whole blocks left over after the last full group.
	MOVQ b_base+8(FP), end
	ADDQ n, end
	SUBQ $32, end
	CMPQ p, end
	JA   afterBlocks

	blockLoop()

afterBlocks:
	MOVQ d+0(FP), dig
	MOVQ v1, 0(dig)
	MOVQ v2, 8(dig)
	MOVQ v3, 16(dig)
	MOVQ v4, 24(dig)

	ANDQ $-32, n
	MOVQ n, ret+32(FP)

	RET
