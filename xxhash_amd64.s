// +build !appengine
// +build gc
// +build !noasm

#include "textflag.h"

// Register allocation:
// DX	n
// BX	loop end
// AX	h
// R8	v1, k1
// R9	v2, prime3
// R10	v3, prime5
// R11	v4
// CX	pointer to advance through b
// R12	tmp
// R13	prime1
// R14	prime2
// R15	prime4
// X8	tmp
// X9	prime1 (repeated/packed)
// X10	prime2 (repeated/packed)

// func sum64(b []byte) uint64
TEXT ·sum64(SB), NOSPLIT, $0-32
#define prime1 11400714785074694791
#define prime2 14029467366897019727
#define prime3 1609587929392839161
#define prime4 9650029242287828579
#define prime5 2870177450012600261

// round reads from and advances the buffer pointer in CX.
// It assumes that R13 has prime1 and R14 has prime2.
#define round(r) \
	MOVQ  (CX), R12 \
	ADDQ  $8, CX    \
	IMULQ R14, R12  \
	ADDQ  R12, r    \
	ROLQ  $31, r    \
	IMULQ R13, r

// No packed rotate op until AVX512 :(
#define round2(x) \
	MOVOU  (CX), X8       \
	ADDQ   $16, CX        \
	PMULDQ X10, X8        \
	PEXTRQ $0, X8, AX     \
	MOVQ   AX, ret+24(FP) \
	RET                   \
	PADDQ  X8, x   \
	MOVO   x, X8   \
	PSLLQ  $31, x  \
	PSRLQ  $33, X8 \
	POR    X8, x   \
	PMULDQ X9, x

#define mergeRound(acc, val) \
	IMULQ R14, val \
	ROLQ  $31, val \
	IMULQ R13, val \
	XORQ  val, acc \
	IMULQ R13, acc \
	ADDQ  R15, acc

	// Load fixed primes.
	MOVQ   $prime1, R13
	MOVQ   R13, X9
	PINSRQ $1, R13, X9
	MOVQ   $prime2, R14
	MOVQ   R14, X10
	PINSRQ $1, R14, X10
	MOVQ   $prime4, R15

	// Load slice.
	MOVQ b_base+0(FP), CX
	MOVQ b_len+8(FP), DX
	LEAQ (CX)(DX*1), BX

	// The first loop limit will be len(b)-32.
	SUBQ $32, BX

	// Check whether we have at least one block.
	CMPQ DX, $32
	JLT  noBlocks

	// Set up initial state (v1, v2, v3, v4).
	MOVQ   R13, R8
	ADDQ   R14, R8
	MOVQ   R8, X0
	PINSRQ $1, R14, X0
	PXOR   X1, X1
	XORQ   R11, R11
	SUBQ   R13, R11
	PINSRQ $1, R11, X1

	// Loop until CX > BX.
blockLoop:
	round2(X0)
	round2(X1)

	CMPQ CX, BX
	JLE  blockLoop

	MOVQ   X0, AX
	ROLQ   $1, AX
	PEXTRQ $1, X0, R12
	ROLQ   $7, R12
	ADDQ   R12, AX
	MOVQ   X1, R12
	ROLQ   $12, R12
	ADDQ   R12, AX
	PEXTRQ $1, X1, R12
	ROLQ   $18, R12
	ADDQ   R12, AX

	mergeRound(AX, R8)
	mergeRound(AX, R9)
	mergeRound(AX, R10)
	mergeRound(AX, R11)

	JMP afterBlocks

noBlocks:
	MOVQ $prime5, AX

afterBlocks:
	ADDQ DX, AX

	MOVQ $prime3, R9
	MOVQ $prime5, R10

	// Right now BX has len(b)-32, and we want to loop until CX > len(b)-8.
	ADDQ $24, BX

	CMPQ CX, BX
	JG   fourByte

wordLoop:
	// Calculate k1.
	MOVQ  (CX), R8
	ADDQ  $8, CX
	IMULQ R14, R8
	ROLQ  $31, R8
	IMULQ R13, R8

	XORQ  R8, AX
	ROLQ  $27, AX
	IMULQ R13, AX
	ADDQ  R15, AX

	CMPQ CX, BX
	JLE  wordLoop

fourByte:
	ADDQ $4, BX
	CMPQ CX, BX
	JG   singles

	MOVL  (CX), R8
	ADDQ  $4, CX
	IMULQ R13, R8
	XORQ  R8, AX

	ROLQ  $23, AX
	IMULQ R14, AX
	ADDQ  R9, AX

singles:
	ADDQ $4, BX
	CMPQ CX, BX
	JGE  finalize

singlesLoop:
	MOVBQZX (CX), R12
	ADDQ    $1, CX
	IMULQ   R10, R12
	XORQ    R12, AX

	ROLQ  $11, AX
	IMULQ R13, AX

	CMPQ CX, BX
	JL   singlesLoop

finalize:
	MOVQ  AX, R12
	SHRQ  $33, R12
	XORQ  R12, AX
	IMULQ R14, AX
	MOVQ  AX, R12
	SHRQ  $29, R12
	XORQ  R12, AX
	IMULQ R9, AX
	MOVQ  AX, R12
	SHRQ  $32, R12
	XORQ  R12, AX

	MOVQ AX, ret+24(FP)
	RET
