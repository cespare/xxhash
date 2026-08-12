//go:build (!amd64 && !arm64) || appengine || !gc || purego
// +build !amd64,!arm64 appengine !gc purego

package xxhash

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
// the statements apart all generate the same code -- and Go 1.26 on arm64
// chooses correctly for all four accumulators in Sum64 but only three of the
// four in writeBlocks, which is why the measured gain is ~+30% for Sum64 and
// ~+5% for Digest.Write rather than one number for both. Worth re-checking, but
// bounded on both sides: chosen the wrong way the chain is the length it was
// before this rewrite, so no version of it is slower than not doing it at all.
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
		for len(b) >= 32 {
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
	// loop: reslicing costs several instructions each time around, since the
	// compiler has to keep the data pointer from moving past the end of the
	// slice, and there are at most three 8-byte rounds to do.
	if len(b) >= 16 {
		h = tailRound8(h, u64(b[0:8]))
		h = tailRound8(h, u64(b[8:16]))
		b = b[16:len(b):len(b)]
	}
	if len(b) >= 8 {
		h = tailRound8(h, u64(b[0:8]))
		b = b[8:len(b):len(b)]
	}
	if len(b) >= 4 {
		h ^= uint64(u32(b[0:4])) * prime1
		h = rol23(h)*prime2 + prime3
		b = b[4:len(b):len(b)]
	}
	for _, c := range b {
		h ^= uint64(c) * prime5
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
	for len(b) >= 32 {
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
