# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go implementation of XXH64 (the 64-bit xxHash). It is a fork of
github.com/cespare/xxhash/v2 — the module path is still `github.com/cespare/xxhash/v2` —
that adds seeded digests (`NewWithSeed`, `ResetWithSeed`).

Two hard constraints, both deliberate:

- **No dependencies.** `go.mod` has zero requires and declares `go 1.11`. This
  package is vendored nearly everywhere, so adding a module (e.g. klauspost/cpuid
  for feature detection, which would also force a Go version bump) is not on the
  table. CPU detection is a few lines of CPUID/XGETBV assembly in `cpu_amd64.s`.
- **Output is frozen.** Every code path must produce byte-identical XXH64 digests.
  Optimizations are the point of this repo; changing the hash is never acceptable.

## Commands

```bash
go test ./...                      # amd64: runs every block loop this CPU supports
go test -tags purego ./...         # force the pure-Go implementation
go test -tags appengine ./...      # force the non-unsafe string helpers
./testall.sh                       # the whole matrix, incl. arm64/386 via qemu

go test -run 'TestSum64Reference/avx2' -v .        # one test, one implementation
taskset -c 2 go test -run xxx -bench 'Sum64/4KB' -benchtime 300ms -count 10 .
```

Benchmarks are noisy at the few-percent level. Pin to a core with `taskset`, use
`-count 10`, and compare with `benchstat`. Anything under ~1.5% is probably code
alignment, not your change.

Useful when working on the Go paths:

```bash
go build -tags purego -gcflags='-d=ssa/check_bce/debug=1' ./   # bounds checks
go build -gcflags=-m -o /dev/null .                            # inlining
```

`TestInlining` asserts `Sum64String` and `(*Digest).WriteString` stay inlinable;
it shells out to `go build -gcflags=-m`, so it works under any GOARCH.

For assembly, `go tool objdump` cannot decode AVX beyond SSE — use binutils
`objdump -d` to check what the Go assembler actually emitted.

## Which file compiles where

The implementation is picked entirely by build tags; `Sum64` and `writeBlocks`
have exactly one definition per configuration.

| file | condition |
| --- | --- |
| `xxhash_asm.go` + `xxhash_amd64.s` / `xxhash_arm64.s` | `(amd64\|arm64) && !appengine && gc && !purego` |
| `xxhash_other.go` | everything else |
| `cpu_amd64.go` + `cpu_amd64.s` | amd64 asm builds only |
| `xxhash_unsafe.go` / `xxhash_safe.go` | `!appengine` / `appengine` |

`xxhash.go` is shared by all of them: `Digest`, its `Write`/`Sum64`, the round
helpers, and the `primes` array (which exists so the assembly has a contiguous
block of constants to load).

`initV1`/`initV4` in `xxhash.go` are the wrapping values `prime1+prime2` and
`-prime1`. Go rejects a constant expression that overflows its type, so they are
written in a roundabout but constant-foldable way; `TestInitConstants` checks
them against the real arithmetic. Don't "simplify" them back.

## amd64 assembly

`xxhash_amd64.s` has four entry points. `Sum64` and `writeBlocks` are frameless
and handle short inputs with the plain scalar block loop; for inputs of at least
`vecCutoff` (256) bytes they **tail-jump** (`JMP ·sum64Vec(SB)`) to `sum64Vec` /
`writeBlocksVec`, which have a stack frame for the group buffer. The split exists
so short inputs never pay for the frame. The `Vec` functions are declared in
`cpu_amd64.go` purely so `go vet` can check their `FP` references; nothing calls
them from Go.

The vector loops don't run XXH64 in SIMD — they can't, the accumulators are
serially dependent. They split each round

    acc = rol31(acc + x*prime2) * prime1

and let the vector units precompute the input-only half, `x*prime2`, which takes
half the 64-bit multiplies off the CPU's single multiply port. Products go
through a stack buffer, the loop is pipelined by one group so a store is a whole
group ahead of its load, and `stepAVX2`/`stepAVX512` interleave the two halves
instruction by instruction to keep the ports balanced. AVX2 synthesizes the
64x64 multiply from three `VPMULUDQ`s; AVX512 uses `VPMULLQ` on 256-bit vectors
(DQ+VL — 512-bit buys nothing here and costs frequency on some parts).

Things that will bite you when editing this file:

- Labels are per-`TEXT`, but a macro containing labels can only be expanded once
  per function. That is why `tail()` appears once and both paths jump to it.
- `#define`s are plain token substitution: a register macro named `d` would
  rewrite `d+0(FP)` into garbage. Hence `dig`.
- `off(SP)` without a symbol is the *hardware* SP, i.e. the local frame. That is
  what the group buffer uses.
- `vecGroupSize` must stay a power of two (the loop bound is a mask), and
  `vecCutoff` a multiple of it. Both were tuned by measurement: smaller groups
  beat larger ones, and entering the vector path below 256 bytes loses.
- Emit `VZEROUPPER` before leaving a vector path.

**The AVX512 path has never been executed.** It was validated by disassembling
the encoding and by temporarily substituting the AVX2 kernel into the AVX512
loops to exercise the surrounding control flow. If you have AVX512 hardware,
`go test` covers it automatically — say so if you run it.

arm64 is intentionally untouched by the vectorization work: NEON has no 64x64
multiply, and the existing `MADD`-based loop was tuned on real hardware. qemu
gives correctness, not timing, so don't "optimize" it blind.

## Testing

`xxhash_ref_test.go` holds a self-contained XXH64 implementation that shares no
code or constants with the package, and checks `Sum64`, `Sum64String`, and
`Digest` against it for every length up to 1100 (plus larger), four seeds, many
chunkings, unaligned inputs, and every two-part write split.

Every one of those tests runs through `forEachImpl`, which on amd64 flips
`useVec`/`useAVX2`/`useAVX512` and re-runs the body once per code path the CPU
supports. **Any new fast path must be reachable that way**, or it ships untested.
`xxhash_test.go` keeps the known-answer vectors; those are what pin the
algorithm itself.
