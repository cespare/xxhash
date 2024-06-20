package main

import (
	"github.com/cespare/xxhash/v2"
	. "github.com/mmcloughlin/avo/build"
	. "github.com/mmcloughlin/avo/operand"
	"github.com/mmcloughlin/avo/reg"
)

const (
	prime1 uint64 = 11400714785074694791
	prime2 uint64 = 14029467366897019727
	prime3 uint64 = 1609587929392839161
	prime4 uint64 = 9650029242287828579
	prime5 uint64 = 2870177450012600261
)

func round0(v /*inout*/, p1, p2 reg.GPVirtual) {
	IMULQ(p2, v)
	ROLQ(Imm(31), v)
	IMULQ(p1, v)
}

func mergeRound(h /*inout*/, v /*in-destroy*/, p1, p2, p4 reg.GPVirtual) {
	round0(v, p1, p2)
	XORQ(v, h)
	IMULQ(p1, h)
	ADDQ(p4, h)
}

func round(state /*inout*/, p, yprime1, yprime2 reg.Register) {
	temp := YMM()
	VMOVDQU(Mem{Base: p}, temp)
	VPMULLQ(temp, yprime2, temp)
	VPADDQ(temp, state, state)
	VPROLQ(Imm(31), state, state)
	VPMULLQ(state, yprime1, state)
}

// blockLoop handles 32 bytes at a time in one YMM register.
// it assume n is 32 bytes or more.
// state represent v1, v2, v3, v4 as 4 × uint64.
func blockLoop(state /*inout*/, p /*inout*/, n, yprime1, yprime2 reg.Register) {
	endp := GP64()
	MOVL(U32(31), endp.As32())
	ANDNQ(n, endp, endp)
	ADDQ(p, endp)

	Label("loop_32")
	{
		// main block loop
		round(state, p, yprime1, yprime2)
		ADDQ(Imm(32), p)

		CMPQ(p, endp)
		JNE(LabelRef("loop_32"))
	}
}

func sum64() {
	initStateAvx512 := GLOBL("·initWideAvx512", NOSPLIT|NOPTR)
	prime1 := prime1
	DATA(0, U64(prime1+prime2))
	DATA(8, U64(prime2))
	DATA(16, U64(0))
	DATA(24, U64(-prime1))

	TEXT("sum64Avx512", NOSPLIT|NOFRAME, "func(b []byte) uint64")
	p := Load(Param("b").Base(), GP64())
	n := Load(Param("b").Len(), GP64())

	p1, p2, p3, p4, p5 := GP64(), GP64(), GP64(), GP64(), GP64()
	MOVQ(Imm(prime1), p1)
	MOVQ(Imm(prime2), p2)
	MOVQ(Imm(prime3), p3)
	MOVQ(Imm(prime4), p4)
	MOVQ(Imm(prime5), p5)

	h := GP64()
	LEAQ(Mem{Base: p5, Index: n, Scale: 1}, h)

	TESTQ(n, n)
	JZ(LabelRef("zero"))
	CMPQ(n, Imm(3))
	JBE(LabelRef("loop_1"))
	CMPQ(n, Imm(7))
	JBE(LabelRef("do_4"))
	CMPQ(n, Imm(31))
	JBE(LabelRef("loop_8"))

	{
		state := YMM()
		VMOVDQU(initStateAvx512, state)

		yprime1, yprime2 := YMM(), YMM()
		VPBROADCASTQ(p1, yprime1)
		VPBROADCASTQ(p2, yprime2)

		blockLoop(state, p, n, yprime1, yprime2)

		// This interleave two things: extracting v1,2,3,4 from state and computing h.
		v1, v2, v3, v4, temp := GP64(), GP64(), GP64(), GP64(), GP64()
		VMOVQ(state.AsX(), v1)
		MOVQ(v1, h)
		ROLQ(Imm(1), h)

		VPEXTRQ(Imm(1), state.AsX(), v2)
		MOVQ(v2, temp)
		ROLQ(Imm(7), temp)
		ADDQ(temp, h)

		VEXTRACTI128(Imm(1), state, state.AsX())
		VMOVQ(state.AsX(), v3)
		MOVQ(v3, temp)
		ROLQ(Imm(12), temp)
		ADDQ(temp, h)

		VPEXTRQ(Imm(1), state.AsX(), v4)
		VZEROUPPER()
		MOVQ(v4, temp)
		ROLQ(Imm(18), temp)
		ADDQ(temp, h)

		// We could do round0 in SIMD if it's worth doing the two decompositions.

		mergeRound(h, v1, p1, p2, p4)
		mergeRound(h, v2, p1, p2, p4)
		mergeRound(h, v3, p1, p2, p4)
		mergeRound(h, v4, p1, p2, p4)

		ADDQ(n, h)
		ANDQ(Imm(31), n)
	}

	// From this point on I didn't bothered writing SIMD code since this will handle at most 31 bytes.

	CMPQ(n, Imm(8))
	JB(LabelRef("skip_8"))
	Label("loop_8")
	{
		temp := GP64()
		MOVQ(Mem{Base: p}, temp)
		ADDQ(Imm(8), p)
		SUBQ(Imm(8), n)
		round0(temp, p1, p2)
		XORQ(temp, h)
		ROLQ(Imm(27), h)
		IMULQ(p1, h)
		ADDQ(p4, h)

		CMPQ(n, Imm(8))
		JAE(LabelRef("loop_8"))
	}
	Label("skip_8")

	CMPQ(n, Imm(4))
	JB(LabelRef("skip_4"))
	Label("do_4")
	{
		temp := GP64()
		MOVL(Mem{Base: p}, temp.As32())
		ADDQ(Imm(4), p)
		SUBQ(Imm(4), n)
		IMULQ(p1, temp)
		XORQ(temp, h)
		ROLQ(Imm(23), h)
		IMULQ(p2, h)
		ADDQ(p3, h)
	}
	Label("skip_4")

	TESTQ(n, n)
	JZ(LabelRef("skip_1"))
	Label("loop_1")
	{
		temp := GP64()
		MOVBLZX(Mem{Base: p}, temp.As32())
		INCQ(p)
		IMULQ(p5, temp)
		XORQ(temp, h)
		ROLQ(Imm(11), h)
		IMULQ(p1, h)

		DECQ(n)
		JNZ(LabelRef("loop_1"))
	}
	Label("skip_1")

	temp := GP64()
	MOVQ(h, temp)
	SHRQ(Imm(33), temp)
	XORQ(temp, h)

	IMULQ(p2, h)

	MOVQ(h, temp)
	SHRQ(Imm(29), temp)
	XORQ(temp, h)

	IMULQ(p3, h)

	MOVQ(h, temp)
	SHRQ(Imm(32), temp)
	XORQ(temp, h)

	Store(h, ReturnIndex(0))
	RET()

	Label("zero")
	MOVQ(U64(xxhash.Sum64([]byte{})), h)
	Store(h, ReturnIndex(0))
	RET()
}

func writeBlocks() {
	TEXT("writeBlocksAvx512", NOSPLIT|NOFRAME, "func(d *[4]uint64, extra *[32]byte, b []byte)")
	d := Load(Param("d"), GP64())
	extra := Load(Param("extra"), GP64())
	p := Load(Param("b").Base(), GP64())
	n := Load(Param("b").Len(), GP64())

	state := YMM()
	VMOVDQU(Mem{Base: d, Disp: 0}, state)

	p1, p2, yprime1, yprime2 := GP64(), GP64(), YMM(), YMM()
	MOVQ(Imm(prime1), p1)
	VPBROADCASTQ(p1, yprime1)
	MOVQ(Imm(prime2), p2)
	VPBROADCASTQ(p2, yprime2)

	TESTQ(extra, extra)
	JZ(LabelRef("skip_extra"))
	{
		round(state, extra, yprime1, yprime2)
	}
	Label("skip_extra")

	blockLoop(state, p, n, yprime1, yprime2)
	VMOVDQU(state, Mem{Base: d, Disp: 0})
	VZEROUPPER()
	RET()
}

func avx512() {
	sum64()
	writeBlocks()
	Generate()
}
