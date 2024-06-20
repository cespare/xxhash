//go:build !appengine && gc && !purego
// +build !appengine
// +build gc
// +build !purego

#include "textflag.h"

DATA ·initWideAvx512<>+0(SB)/8, $0x60ea27eeadc0b5d6
DATA ·initWideAvx512<>+8(SB)/8, $0xc2b2ae3d27d4eb4f
DATA ·initWideAvx512<>+16(SB)/8, $0x0000000000000000
DATA ·initWideAvx512<>+24(SB)/8, $0x61c8864e7a143579
GLOBL ·initWideAvx512<>(SB), NOSPLIT|NOPTR, $32

#define p      SI
#define h      AX
#define end    DI
#define temp   BX
#define prime1 DX
#define prime2 R8

#define state   Y0
#define xstate  X0
#define yprime1 Y1
#define yprime2 Y2
#define ytemp   Y3
#define xtemp   X3

#define yround()                  \
	VPMULLQ ytemp, yprime2, ytemp \
	VPADDQ  ytemp, state, state   \
	VPROLQ  $31, state, state     \
	VPMULLQ state, yprime1, state \

#define blockLoop(length)         \
	MOVL    $0x1f, end            \
	ANDNQ   length, end, end      \
	ADDQ    p, end                \
	PCALIGN $64                   \
loop_32:                          \
	VMOVDQU (p), ytemp            \
	yround()                      \
	ADDQ    $32, p                \
	CMPQ    p, end                \
	JNE     loop_32

#define n      CX
#define prime3 R9
#define prime4 R10
#define prime5 R11

// lateMergeRound performs mergeRound on h given the value from round0
#define lateMergeRound(v) \
	XORQ  v, h            \
	IMULQ prime1, h       \
	ADDQ  prime4, h

// func Sum64(b []byte) uint64
// Requires: AVX, AVX2, AVX512DQ, AVX512F, AVX512VL, BMI
TEXT ·Sum64(SB), NOSPLIT|NOFRAME, $0-32
	CMPB ·useAvx512(SB), $0x00
	JNE  do_avx512
	JMP  ·sum64Scalar(SB)
zero:
	MOVQ $0xef46db3751d8e999, h
	MOVQ h, ret+24(FP)
	RET

do_avx512:
	MOVQ         b_base+0(FP), p
	MOVQ         b_len+8(FP), n
	MOVQ         $0x9e3779b185ebca87, prime1
	MOVQ         $0xc2b2ae3d27d4eb4f, prime2
	MOVQ         $0x165667b19e3779f9, prime3
	MOVQ         $0x85ebca77c2b2ae63, prime4
	MOVQ         $0x27d4eb2f165667c5, prime5

	LEAQ         (prime5)(n*1), h // precompute h for the shortcuts
	JCXZQ        zero
	CMPQ         n, $3
	JBE          loop_1
	CMPQ         n, $7
	JBE          do_4
	CMPQ         n, $31
	JBE          loop_8

	VMOVDQU      ·initWideAvx512<>+0(SB), state
	VPBROADCASTQ prime1, yprime1
	VPBROADCASTQ prime2, yprime2

	blockLoop(n)

	VMOVQ        xstate, h
	ROLQ         $1, h

	VPEXTRQ      $1, xstate, temp
	ROLQ         $7, temp
	ADDQ         temp, h

	VEXTRACTI128 $1, state, xtemp
	VMOVQ        xtemp, temp
	ROLQ         $12, temp
	ADDQ         temp, h

	VPEXTRQ      $1, xtemp, temp
	ROLQ         $18, temp
	ADDQ         temp, h

	// round0 for mergeRound
	VPMULLQ      yprime2, state, state
	VPROLQ       $0x1f, state, state
	VPMULLQ      yprime1, state, state

	VMOVQ        xstate, temp
	lateMergeRound(temp)

	VPEXTRQ      $1, xstate, temp
	lateMergeRound(temp)

	VEXTRACTI128 $1, state, xtemp
	VMOVQ        xtemp, temp
	lateMergeRound(temp)

	VPEXTRQ      $1, xtemp, temp
	VZEROUPPER
	lateMergeRound(temp)
	
	ADDQ         n, h
	ANDQ         $0x1f, n

	CMPQ         n, $8
	JB           skip_8
loop_8:
	MOVQ  (p), temp
	ADDQ  $8, p
	SUBQ  $8, n
	IMULQ prime2, temp
	ROLQ  $31, temp
	IMULQ prime1, temp
	XORQ  temp, h
	ROLQ  $27, h
	IMULQ prime1, h
	ADDQ  prime4, h
	CMPQ  n, $8
	JAE   loop_8
skip_8:

	CMPQ n, $4
	JB   skip_4
do_4:
	MOVL  (p), temp
	ADDQ  $4, p
	SUBQ  $4, n
	IMULQ prime1, temp
	XORQ  temp, h
	ROLQ  $23, h
	IMULQ prime2, h
	ADDQ  prime3, h
skip_4:

	JCXZQ skip_1
loop_1:
	MOVBLZX (p), temp
	INCQ    p
	IMULQ   prime5, temp
	XORQ    temp, h
	ROLQ    $0x0b, h
	IMULQ   prime1, h
	DECQ    n
	JNZ	    loop_1 // could be a LOOP but go tool asm wont assemble it :'(
skip_1:

	MOVQ  h, temp
	SHRQ  $33, temp
	XORQ  temp, h
	IMULQ prime2, h
	MOVQ  h, temp
	SHRQ  $29, temp
	XORQ  temp, h
	IMULQ prime3, h
	MOVQ  h, temp
	SHRQ  $32, temp
	XORQ  temp, h
	MOVQ  h, ret+24(FP)
	RET

#define extrap CX
#define l      R9

// func writeBlocksAvx512(d *[4]uint64, extra *[32]byte, b []byte)
// Requires: AVX, AVX2, AVX512DQ, AVX512F, AVX512VL, BMI
TEXT ·writeBlocks(SB), NOSPLIT|NOFRAME, $0-40
	CMPB ·useAvx512(SB), $0x00
	JNE  do_avx512
	JMP  ·writeBlocksScalar(SB)

do_avx512:
	MOVQ         d+0(FP), h
	MOVQ         extra+8(FP), extrap
	MOVQ         b_base+16(FP), p
	MOVQ         b_len+24(FP), l
	VMOVDQU      (h), state
	MOVQ         $0x9e3779b185ebca87, prime1
	VPBROADCASTQ prime1, yprime1
	MOVQ         $0xc2b2ae3d27d4eb4f, prime2
	VPBROADCASTQ prime2, yprime2
	JCXZQ        skip_extra
	VMOVDQU      (extrap), ytemp
	yround()

skip_extra:
	blockLoop(l)
	VMOVDQU state, (h)
	VZEROUPPER
	RET
