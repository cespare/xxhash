// +build !appengine
// +build gc
// +build !purego

#include "textflag.h"

// Register allocation:
// SI	h
// AX	pointer to advance through b
// BX	n
// DX	loop end
// R8	v1, k1
// R9	v2
// R10	v3
// R11	v4
// R12	tmp
// R13	prime1v
// R14	prime2v
// DI	prime4v

// round reads from and advances the buffer pointer in AX.
// It assumes that R13 has prime1v and R14 has prime2v.
#define round(r) \
	MOVQ  (AX), R12 \
	ADDQ  $8, AX    \
	IMULQ R14, R12  \
	ADDQ  R12, r    \
	ROLQ  $31, r    \
	IMULQ R13, r

// mergeRound applies a merge round on the two registers acc and val.
// It assumes that R13 has prime1v, R14 has prime2v, and DI has prime4v.
#define mergeRound(acc, val) \
	IMULQ R14, val \
	ROLQ  $31, val \
	IMULQ R13, val \
	XORQ  val, acc \
	IMULQ R13, acc \
	ADDQ  DI, acc

// func Sum64(b []byte) uint64
TEXT ·Sum64<ABIInternal>(SB), NOSPLIT, $0-32
	// Load fixed primes.
	MOVQ ·prime1v(SB), R13
	MOVQ ·prime2v(SB), R14
	MOVQ ·prime4v(SB), DI

	// Load slice.
	LEAQ (AX)(BX*1), DX

	// The first loop limit will be len(b)-32.
	SUBQ $32, DX

	// Check whether we have at least one block.
	CMPQ BX, $32
	JLT  noBlocks

	// Set up initial state (v1, v2, v3, v4).
	MOVQ R13, R8
	ADDQ R14, R8
	MOVQ R14, R9
	XORQ R10, R10
	XORQ R11, R11
	SUBQ R13, R11

	// Loop until AX > DX.
blockLoop:
	round(R8)
	round(R9)
	round(R10)
	round(R11)

	CMPQ AX, DX
	JLE  blockLoop

	MOVQ R8, SI
	ROLQ $1, SI
	MOVQ R9, R12
	ROLQ $7, R12
	ADDQ R12, SI
	MOVQ R10, R12
	ROLQ $12, R12
	ADDQ R12, SI
	MOVQ R11, R12
	ROLQ $18, R12
	ADDQ R12, SI

	mergeRound(SI, R8)
	mergeRound(SI, R9)
	mergeRound(SI, R10)
	mergeRound(SI, R11)

	JMP afterBlocks

noBlocks:
	MOVQ ·prime5v(SB), SI

afterBlocks:
	ADDQ BX, SI

	// Right now DX has len(b)-32, and we want to loop until AX > len(b)-8.
	ADDQ $24, DX

	CMPQ AX, DX
	JG   fourByte

wordLoop:
	// Calculate k1.
	MOVQ  (AX), R8
	ADDQ  $8, AX
	IMULQ R14, R8
	ROLQ  $31, R8
	IMULQ R13, R8

	XORQ  R8, SI
	ROLQ  $27, SI
	IMULQ R13, SI
	ADDQ  DI, SI

	CMPQ AX, DX
	JLE  wordLoop

fourByte:
	ADDQ $4, DX
	CMPQ AX, DX
	JG   singles

	MOVL  (AX), R8
	ADDQ  $4, AX
	IMULQ R13, R8
	XORQ  R8, SI

	ROLQ  $23, SI
	IMULQ R14, SI
	ADDQ  ·prime3v(SB), SI

singles:
	ADDQ $4, DX
	CMPQ AX, DX
	JGE  finalize

singlesLoop:
	MOVBQZX (AX), R12
	ADDQ    $1, AX
	IMULQ   ·prime5v(SB), R12
	XORQ    R12, SI

	ROLQ  $11, SI
	IMULQ R13, SI

	CMPQ AX, DX
	JL   singlesLoop

finalize:
	MOVQ  SI, R12
	SHRQ  $33, R12
	XORQ  R12, SI
	IMULQ R14, SI
	MOVQ  SI, R12
	SHRQ  $29, R12
	XORQ  R12, SI
	IMULQ ·prime3v(SB), SI
	MOVQ  SI, R12
	SHRQ  $32, R12
	XORQ  R12, SI

	XORPS X15, X15
	MOVQ (TLS), R14

	MOVQ SI, AX
	RET

// writeBlocks uses the same registers as above except that it uses SI to store
// the d pointer.

// func writeBlocks(d *Digest, b []byte) int
TEXT ·writeBlocks(SB), NOSPLIT, $0-40
	// Load fixed primes needed for round.
	MOVQ ·prime1v(SB), R13
	MOVQ ·prime2v(SB), R14

	// Load slice.
	MOVQ b_base+8(FP), AX
	MOVQ b_len+16(FP), BX
	LEAQ (AX)(BX*1), DX
	SUBQ $32, DX

	// Load vN from d.
	MOVQ d+0(FP), SI
	MOVQ 0(SI), R8   // v1
	MOVQ 8(SI), R9   // v2
	MOVQ 16(SI), R10 // v3
	MOVQ 24(SI), R11 // v4

	// We don't need to check the loop condition here; this function is
	// always called with at least one block of data to process.
blockLoop:
	round(R8)
	round(R9)
	round(R10)
	round(R11)

	CMPQ AX, DX
	JLE  blockLoop

	// Copy vN back to d.
	MOVQ R8, 0(SI)
	MOVQ R9, 8(SI)
	MOVQ R10, 16(SI)
	MOVQ R11, 24(SI)

	// The number of bytes written is AX minus the old base pointer.
	SUBQ b_base+8(FP), AX
	MOVQ AX, ret+32(FP)

	RET
