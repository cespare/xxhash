//go:build !appengine && gc && !purego
// +build !appengine
// +build gc
// +build !purego

#include "textflag.h"

// Registers:
#define digest	R1
#define h	R2 // return value
#define p	R3 // input pointer
#define n	R4 // input length
#define nblocks	R5 // n / 32
#define prime1	R7
#define prime2	R8
#define prime3	R9
#define prime4	R10
#define prime5	R11
#define v1	R12
#define v2	R13
#define v3	R14
#define v4	R15
#define x1	R20
#define x2	R21
#define x3	R22
#define x4	R23

// The block loop is bounded by the latency of the four accumulator chains and
// by the one-per-cycle multiply-add, so a round is worth spelling out in
// whatever way makes one link of that chain shortest. Written directly, a
// round is
//
//	acc = rol31(acc + x*prime2) * prime1
//
// and as a MADD, a rotate and a multiply that is multiply-add (3 cycles),
// rotate (1), multiply (3): seven cycles per block.
//
// x*prime2 depends only on the input, so it need not be on the chain at all.
// Splitting the MADD into a separate multiply and add leaves add, rotate,
// multiply -- five cycles, with the multiply free to run many blocks ahead.
//
// The add comes off too, by carrying the loop rotated by part of a round.
// Substituting w = rol31(acc + x*prime2), so that acc = w_prev * prime1,
//
//	w = rol31(w_prev * prime1 + x*prime2)
//
// which is a MADD and a rotate: four cycles, with nothing left on the chain
// that isn't part of the hash. preRound puts the accumulators into w for the
// first block, round carries them, and finishRound converts them back with the
// multiply that the substitution left outstanding.
//
// Four cycles is also what the four MADDs need on their own: both Apple cores
// and Neoverse retire one MADD per cycle (against two plain multiplies), so
// the chain and the multiply pipelines saturate together. See the note in
// CLAUDE.md on why there is no NEON block loop here -- the input multiplies
// are not the constraint, so moving them to the vector units buys nothing.
//
// Which side of the MADD the rotate sits on is free on paper -- carrying
// acc + x*prime2 and rotating first is the same three instructions and the
// same four-cycle chain -- but it is not free in fact. On a Neoverse N2 the
// rotate-first form measures 5.0 cycles per block against 4.4 for this one,
// for no reason visible in the chain or the port counts; six other schedules
// of the rotate-first form were tried and none of them beat 5.0. Keep the
// rotate after the MADD.
#define round(w, x) \
	MUL  prime2, x       \
	MADD prime1, x, w, w \
	ROR  $64-31, w

// preRound folds the first block into the accumulator, leaving it in the
// carried form that round expects. This one runs once rather than per block,
// so unlike round it wants the MADD: there is no loop here for the multiply to
// run ahead of, and fusing it is both a cycle and an instruction cheaper.
#define preRound(w, x) \
	MADD prime2, w, x, w \
	ROR  $64-31, w

// finishRound converts the carried form back into the accumulator itself.
#define finishRound(w) \
	MUL prime1, w

// round0 performs the operation x = round(0, x).
#define round0(x) \
	MUL prime2, x \
	ROR $64-31, x \
	MUL prime1, x

#define mergeRound(acc, x) \
	round0(x)                     \
	EOR  x, acc                   \
	MADD acc, prime4, prime1, acc

// blockLoop processes as many 32-byte blocks as possible,
// updating v1, v2, v3, and v4. It assumes that n >= 32.
//
// The first block is peeled off to put the accumulators into the form round
// carries, and the multiply that the substitution leaves outstanding is done
// once at the end rather than once per block.
//
// The loop then takes two blocks at a time. The rounds themselves don't get
// any faster for being unrolled -- the four MADDs still need their four
// cycles -- but the decrement and the branch stop being a sixteenth of the
// instruction stream, which is worth most of a cycle a block on a Neoverse N2.
//
// When the count left after the peel is odd, one block is run on the way in.
// Written out rather than jumped to, because it is on the path of every input
// that reaches the loop at all: entering the pair loop at its midpoint instead
// costs an add and a taken branch, and inputs of a couple of blocks are short
// enough that those two instructions measured 2% of the whole call.
//
// The one test for an empty count sits after that block rather than before it,
// so that the odd and even paths share it. Testing before would need a second
// one, and then a two-block input -- which is peel, odd block, done, and never
// reaches the loop at all -- would run an instruction more than it did when
// the loop went a block at a time.
#define blockLoop() \
	LSR     $5, n, nblocks  \
	LDP.P   16(p), (x1, x2) \
	LDP.P   16(p), (x3, x4) \
	preRound(v1, x1)        \
	preRound(v2, x2)        \
	preRound(v3, x3)        \
	preRound(v4, x4)        \
	SUB     $1, nblocks     \
	TBZ     $0, nblocks, evenBlocks \
	LDP.P   16(p), (x1, x2) \
	LDP.P   16(p), (x3, x4) \
	round(v1, x1)           \
	round(v2, x2)           \
	round(v3, x3)           \
	round(v4, x4)           \
	SUB     $1, nblocks     \
	evenBlocks:             \
	CBZ     nblocks, blocksDone \
	PCALIGN $16             \
	loop:                   \
	LDP.P   16(p), (x1, x2) \
	LDP.P   16(p), (x3, x4) \
	round(v1, x1)           \
	round(v2, x2)           \
	round(v3, x3)           \
	round(v4, x4)           \
	LDP.P   16(p), (x1, x2) \
	LDP.P   16(p), (x3, x4) \
	round(v1, x1)           \
	round(v2, x2)           \
	round(v3, x3)           \
	round(v4, x4)           \
	SUB     $2, nblocks     \
	CBNZ    nblocks, loop   \
	blocksDone:             \
	finishRound(v1)         \
	finishRound(v2)         \
	finishRound(v3)         \
	finishRound(v4)

// func Sum64(b []byte) uint64
TEXT ·Sum64(SB), NOSPLIT|NOFRAME, $0-32
	LDP b_base+0(FP), (p, n)

	LDP  ·primes+0(SB), (prime1, prime2)
	LDP  ·primes+16(SB), (prime3, prime4)
	MOVD ·primes+32(SB), prime5

	CMP  $32, n
	CSEL LT, prime5, ZR, h // if n < 32 { h = prime5 } else { h = 0 }
	BLT  afterLoop

	ADD  prime1, prime2, v1
	MOVD prime2, v2
	MOVD $0, v3
	NEG  prime1, v4

	blockLoop()

	ROR $64-1, v1, x1
	ROR $64-7, v2, x2
	ADD x1, x2
	ROR $64-12, v3, x3
	ROR $64-18, v4, x4
	ADD x3, x4
	ADD x2, x4, h

	mergeRound(h, v1)
	mergeRound(h, v2)
	mergeRound(h, v3)
	mergeRound(h, v4)

afterLoop:
	ADD n, h

	TBZ   $4, n, try8
	LDP.P 16(p), (x1, x2)

	round0(x1)

	// NOTE: here and below, sequencing the EOR after the ROR (using a
	// rotated register) is worth a small but measurable speedup for small
	// inputs.
	ROR  $64-27, h
	EOR  x1 @> 64-27, h, h
	MADD h, prime4, prime1, h

	round0(x2)
	ROR  $64-27, h
	EOR  x2 @> 64-27, h, h
	MADD h, prime4, prime1, h

try8:
	TBZ    $3, n, try4
	MOVD.P 8(p), x1

	round0(x1)
	ROR  $64-27, h
	EOR  x1 @> 64-27, h, h
	MADD h, prime4, prime1, h

try4:
	TBZ     $2, n, try2
	MOVWU.P 4(p), x2

	MUL  prime1, x2
	ROR  $64-23, h
	EOR  x2 @> 64-23, h, h
	MADD h, prime3, prime2, h

try2:
	TBZ     $1, n, try1
	MOVHU.P 2(p), x3
	AND     $255, x3, x1
	LSR     $8, x3, x2

	MUL prime5, x1
	ROR $64-11, h
	EOR x1 @> 64-11, h, h
	MUL prime1, h

	MUL prime5, x2
	ROR $64-11, h
	EOR x2 @> 64-11, h, h
	MUL prime1, h

try1:
	TBZ   $0, n, finalize
	MOVBU (p), x4

	MUL prime5, x4
	ROR $64-11, h
	EOR x4 @> 64-11, h, h
	MUL prime1, h

finalize:
	EOR h >> 33, h
	MUL prime2, h
	EOR h >> 29, h
	MUL prime3, h
	EOR h >> 32, h

	MOVD h, ret+24(FP)
	RET

// func writeBlocks(d *Digest, b []byte) int
TEXT ·writeBlocks(SB), NOSPLIT|NOFRAME, $0-40
	LDP ·primes+0(SB), (prime1, prime2)

	// Load state. Assume v[1-4] are stored contiguously.
	MOVD d+0(FP), digest
	LDP  0(digest), (v1, v2)
	LDP  16(digest), (v3, v4)

	LDP b_base+8(FP), (p, n)

	blockLoop()

	// Store updated state.
	STP (v1, v2), 0(digest)
	STP (v3, v4), 16(digest)

	BIC  $31, n
	MOVD n, ret+32(FP)
	RET
