//go:build (!amd64 && !arm64) || appengine || !gc || purego
// +build !amd64,!arm64 appengine !gc purego

package xxhash

import (
	"encoding/binary"
	"math/bits"
)

// The block loops below carry acc+input*prime2 rather than acc itself, which is
// the same rearrangement xxhash_arm64.s makes and is explained at length there.
// Substituting u = acc + input*prime2, so that acc = rol31(u_prev) * prime1,
// turns the round
//
//	acc = rol31(acc + input*prime2) * prime1
//
// into
//
//	u = rol31(u_prev) * prime1 + input*prime2
//
// which on a machine with a fused integer multiply-add is a rotate and one
// instruction, taking the loop-carried chain from multiply-add, rotate,
// multiply down to rotate, multiply-add. preRound establishes u from the first
// block, carryRound is the loop body, and finishRound converts back.
//
// Elsewhere the chain is the same length either way -- x86 has no integer
// multiply-add and spells both forms as three dependent instructions -- but that
// does not make the rearrangement free there, because the loop is not bounded by
// the chain. Eight multiplies a block against one multiply port floor it at 8
// cycles where the chain is 5, and carryRound needs input*prime2 live in its own
// register per accumulator where the plain round consumes each product straight
// away. Measured on Zen 4 with go1.26.5, purego Sum64 is 10-17% slower this way.
// See "The pure-Go block loops" in CLAUDE.md and the table in BENCHMARK.md.
//
// How much of the win arrives is up to the compiler. carryRound is a*b + c*d and
// only one of those multiplies is on the dependency chain; fusing the other one
// into the multiply-add leaves the chain exactly as long as it started. Nothing
// in the source picks which -- operand order, naming the products, and splitting
// the statements apart all generate the same code, and that was re-checked
// against go1.26.5 after the loops were written out by hand below. As it stands
// arm64 fuses the input multiply in both functions, so the carried value arrives
// as the multiply-add's addend and the link is rotate (1), multiply (2),
// multiply-add through the addend (1). That is a cycle longer than the best case
// and it does not matter: the loop measures 5.6 cycles a block against a
// four-cycle chain, so what binds is throughput, not the chain. Bounded on both
// sides either way -- chosen the wrong way the chain is the length it was before
// this rewrite, so no version of it is slower than not doing it at all.
//
// The block loops themselves do not call carryRound, u64 or rol31, and are
// written out in terms of bits.RotateLeft64 and binary.LittleEndian.Uint64
// instead. That is not a style choice and it is worth a great deal more than it
// looks. Go emits an inline mark per inlined call so that a traceback can name
// the inlined frame, and a mark that does not land on the address of some
// instruction the function was going to emit anyway survives into the binary as
// a NOP. Four accumulators' worth of carryRound and u64 left twelve of them in
// the loop on arm64 -- a third of the whole instruction stream, and about the
// same on amd64, ppc64le and loong64 -- for a loop whose real work is eighteen
// instructions. Calling nothing removes them all. Measured on a Neoverse N2 it
// is worth 4% of Sum64 on its own, and it is what lets the two changes either
// side of it be read at all.
//
// Between the three of them -- the unconditional pointer advance described in
// Sum64, prime1 held in a register, and calling nothing -- a 32-byte block went
// from 37 retired instructions to 18, and from 7.80 cycles to 5.64 for Sum64 and
// 7.04 to 5.61 for Digest on a Neoverse N2. Every other target this file
// compiles for retires fewer instructions per block as well; see CLAUDE.md.
func preRound(acc, input uint64) uint64 {
	return acc + input*prime2
}

func carryRound(u, input uint64) uint64 {
	return rol31(u)*prime1 + input*prime2
}

func finishRound(u uint64) uint64 {
	return rol31(u) * prime1
}

// Sum64 computes the 64-bit xxHash digest of b with a zero seed.
func Sum64(b []byte) uint64 {
	// A simpler version would be
	//   d := New()
	//   d.Write(b)
	//   return d.Sum64()
	// but this is faster, particularly for small inputs.

	n := len(b)
	var h uint64

	if n >= 32 {
		v1 := initV1
		v2 := prime2
		v3 := uint64(0)
		v4 := initV4
		v1 = preRound(v1, u64(b[0:8:len(b)]))
		v2 = preRound(v2, u64(b[8:16:len(b)]))
		v3 = preRound(v3, u64(b[16:24:len(b)]))
		v4 = preRound(v4, u64(b[24:32:len(b)]))
		b = b[32:len(b):len(b)]
		// prime1 comes out of the array rather than being spelled as the
		// constant, because a rematerializable value is exactly what the
		// register allocator will not keep live across a loop: go1.26.5 rebuilt
		// it on arm64 every iteration out of a MOVD and three MOVKs, four of the
		// loop's instructions to produce a number that never changes. A load is
		// not rematerializable, so the allocator has to hold it, and the loop
		// pays for it once. Worth 10% of the loop on a Neoverse N2, and fewer
		// instructions per block on every other target too -- including the
		// 32-bit ones, where holding a uint64 costs a register pair and might
		// have been expected to spill instead. It does not: see CLAUDE.md.
		// The guard is the loop's own entry test, which the compiler folds into
		// it; it is written out so that the load lands on the path that has a
		// loop to run, and inputs of a single block don't pay for it.
		if len(b) >= 64 {
			p1 := primes[0]
			_ = p1
		}
		// The loop stops with a whole block still in hand, which the block after
		// it consumes. That shape is not for the rounds' sake but for the pointer
		// advance: Go won't leave a pointer one past the end of an object, so a
		// b = b[32:] whose result the compiler can't prove non-empty compiles the
		// advance conditionally on the new length -- NEG, ASR, AND and then the
		// ADD on arm64, five instructions where one would do. Bounding the loop
		// at 64 leaves at least a block behind, which makes the result provably
		// non-empty and collapses it to that one ADD. The cost is a copy of the
		// round and one more test per call; the saving is three instructions on
		// every iteration.
		//
		// An index instead of a reslice does not work: p+32 can overflow in
		// principle, so the prove pass won't chain the loop guard to the loads
		// and the bounds checks come back. Reslicing is the form Go handles.
		//
		// Taking two blocks an iteration on top of this, the way xxhash_arm64.s
		// does, was measured and rejected: it retires 16 instructions a block
		// rather than 18 and still ran Sum64 7% slower on a Neoverse N2, which is
		// the signature of code placement rather than of the change, for five
		// copies of the round in the source.
		if len(b) >= 64 {
			// This guard is the loop's own entry test, which the compiler folds
			// away; writing it out gives the load above a home on the path that
			// has a loop to run, so a one-block input doesn't pay for it.
			p1 := primes[0]
			for len(b) >= 64 {
				v1 = bits.RotateLeft64(v1, 31)*p1 + binary.LittleEndian.Uint64(b[0:8:len(b)])*prime2
				v2 = bits.RotateLeft64(v2, 31)*p1 + binary.LittleEndian.Uint64(b[8:16:len(b)])*prime2
				v3 = bits.RotateLeft64(v3, 31)*p1 + binary.LittleEndian.Uint64(b[16:24:len(b)])*prime2
				v4 = bits.RotateLeft64(v4, 31)*p1 + binary.LittleEndian.Uint64(b[24:32:len(b)])*prime2
				b = b[32:len(b):len(b)]
			}
		}
		// The block the loop left behind. There is at most one, so this is the
		// one place the conditional advance above is still paid, and it can call
		// carryRound like everything else that runs once a call.
		if len(b) >= 32 {
			v1 = carryRound(v1, u64(b[0:8:len(b)]))
			v2 = carryRound(v2, u64(b[8:16:len(b)]))
			v3 = carryRound(v3, u64(b[16:24:len(b)]))
			v4 = carryRound(v4, u64(b[24:32:len(b)]))
			b = b[32:len(b):len(b)]
		}
		v1 = finishRound(v1)
		v2 = finishRound(v2)
		v3 = finishRound(v3)
		v4 = finishRound(v4)
		h = rol1(v1) + rol7(v2) + rol12(v3) + rol18(v4)
		h = mergeRound(h, v1)
		h = mergeRound(h, v2)
		h = mergeRound(h, v3)
		h = mergeRound(h, v4)
	} else {
		h = prime5
	}

	h += uint64(n)

	// The remaining bytes, fewer than 32 of them, are folded in without a
	// loop, keeping an offset into b rather than reslicing it: there are at
	// most three 8-byte rounds to do, and each reslice costs five instructions,
	// since Go won't leave a pointer one past the end of an object and so makes
	// the advance conditional on the remaining length. b[p:p+8] has a length
	// the compiler knows, so it advances unconditionally; the guards are
	// written p+8 <= len(b) rather than len(b)-p >= 8 because that is the fact
	// the prove pass needs to drop the bounds check.
	p := 0
	if len(b) >= 16 {
		h = tailRound8(h, u64(b[0:8]))
		h = tailRound8(h, u64(b[8:16]))
		p = 16
	}
	if p+8 <= len(b) {
		h = tailRound8(h, u64(b[p:p+8]))
		p += 8
	}
	if p+4 <= len(b) {
		h ^= uint64(u32(b[p:p+4])) * prime1
		h = rol23(h)*prime2 + prime3
		p += 4
	}
	for ; p < len(b); p++ {
		h ^= uint64(b[p]) * prime5
		h = rol11(h) * prime1
	}

	h ^= h >> 33
	h *= prime2
	h ^= h >> 29
	h *= prime3
	h ^= h >> 32

	return h
}

// writeBlocks is only ever called with at least one whole block, which the
// peeled first round below relies on; the check keeps that a return rather than
// a bounds panic, and tells the compiler what it needs to elide the peel's
// bounds checks.
func writeBlocks(d *Digest, b []byte) int {
	if len(b) < 32 {
		return 0
	}
	v1, v2, v3, v4 := d.v1, d.v2, d.v3, d.v4
	n := len(b)
	v1 = preRound(v1, u64(b[0:8:len(b)]))
	v2 = preRound(v2, u64(b[8:16:len(b)]))
	v3 = preRound(v3, u64(b[16:24:len(b)]))
	v4 = preRound(v4, u64(b[24:32:len(b)]))
	b = b[32:len(b):len(b)]
	// Bounded at 64 so that the advance is unconditional, prime1 out of the array
	// so that it stays in a register, and the round written out so that inlining
	// leaves no NOPs behind: all three are explained in Sum64.
	if len(b) >= 64 {
		p1 := primes[0]
		for len(b) >= 64 {
			v1 = bits.RotateLeft64(v1, 31)*p1 + binary.LittleEndian.Uint64(b[0:8:len(b)])*prime2
			v2 = bits.RotateLeft64(v2, 31)*p1 + binary.LittleEndian.Uint64(b[8:16:len(b)])*prime2
			v3 = bits.RotateLeft64(v3, 31)*p1 + binary.LittleEndian.Uint64(b[16:24:len(b)])*prime2
			v4 = bits.RotateLeft64(v4, 31)*p1 + binary.LittleEndian.Uint64(b[24:32:len(b)])*prime2
			b = b[32:len(b):len(b)]
		}
	}
	// The block the loop left behind; see Sum64.
	if len(b) >= 32 {
		v1 = carryRound(v1, u64(b[0:8:len(b)]))
		v2 = carryRound(v2, u64(b[8:16:len(b)]))
		v3 = carryRound(v3, u64(b[16:24:len(b)]))
		v4 = carryRound(v4, u64(b[24:32:len(b)]))
		b = b[32:len(b):len(b)]
	}
	v1 = finishRound(v1)
	v2 = finishRound(v2)
	v3 = finishRound(v3)
	v4 = finishRound(v4)
	d.v1, d.v2, d.v3, d.v4 = v1, v2, v3, v4
	return n - len(b)
}
