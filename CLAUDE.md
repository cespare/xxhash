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

On a laptop this is worse than it sounds. On a Ryzen 8840HS with a `powersave`
governor, a single `-count 10` run of one build against another moved by more
than the effects being chased: the clock swings with thermals, and an SMT
sibling can take half the core. What eventually worked:

```bash
# Alternate the two builds, and alternate which one goes first, so that
# whatever drifts over the run drifts over both equally.
for i in $(seq 1 20); do
  if (( i % 2 )); then old.test ... >> old.txt; new.test ... >> new.txt
  else                new.test ... >> new.txt; old.test ... >> old.txt; fi
done
benchstat old.txt new.txt   # ±2% at n=20, enough to see 3%
```

Two things that look like better methodology and are not:

- **Linking both versions into one binary** (rename the old `TEXT` symbols out
  of `git show HEAD:xxhash_amd64.s`, call both from a scratch `_test.go`) and
  measuring them in interleaved bursts. It removes the clock drift, but the two
  functions then alternate in one instruction stream and their relative
  placement is its own effect: two harnesses disagreed by 5 percentage points
  about the *same machine code*, verified identical by disassembling both test
  binaries. If you do this anyway, run the control — HEAD against itself — in
  the same harness, and believe nothing until it reads 1.000.
- **Normalizing to cycles per block** by timing the scalar loop in the same
  sequence (it is throughput-locked at 8 cycles per block, so it calibrates the
  clock). Useful for reading absolute cost, but it inherits the bias above.

Whatever the harness, sweep the input's address before believing a result:
the buffer's stores and the input's loads 4K-alias each other, so one address
is one draw from that lottery rather than a measurement of the change.

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

The remaining accumulator chain is add, rotate, multiply: 1+1+3 = 5 cycles per
32-byte block, and that is the floor for XXH64 on one stream. Measured on Zen 4
the vector loops sit at about 5.1-5.3, so there is only a few percent left in
the block loop itself; the scalar loop's eight multiplies put it at 8.

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

**The AVX512 path has been executed.** It was first run on 2026-08-12 on an AMD
Ryzen 7 8840HS (Zen 4, AVX512F/DQ/VL/BW/VBMI2): the full suite including every
`forEachImpl` subtest, `-race`, the arch/tag matrix in `testall.sh`, and a
20,000-case randomized pass over sizes up to 1 MB with random offsets, seeds and
write splits. It is the fastest path there, a little ahead of AVX2 at every size
past the cutoff.

Tried on Zen 4 and rejected, so as not to be tried again without a reason:

- **512-bit products** (one `VPMULLQ` and one store per two blocks, buffer
  64-byte aligned). Fewer instructions, but 1-2% *slower* than the 256-bit loop.
  Zen 4 splits 512-bit ops into two 256-bit halves, and only the accumulators
  are on the critical path anyway.
- **An all-vector round** (`VPADDQ`/`VPROLQ`/`VPMULLQ` on the four accumulators
  in one YMM). Measured 6 cycles per block against the current 5.1-5.3: on Zen 4
  `VPROLQ` is 2 cycles where `ROLQ` is 1. It needs no stack buffer and starts up
  much faster, so it is worth re-checking on a part with a 1-cycle vector
  rotate, but it is not free money.
- **Scalar warm-up blocks plus a vectorized tail**, to hide the ~13-cycle
  pipeline fill and to stop the last `blocks % vecBlocks` blocks falling back to
  the 8-cycle scalar loop. A wash: back to back, those leftover multiplies land
  in port slots the next call's vector startup leaves idle, so they were nearly
  free already.
- **Pinning the group buffer's alignment.** Every product store is 32 bytes wide
  and is read back as four 8-byte loads a group later, and off SP its alignment
  is not ours to pick — `sum64Vec` is tail-jumped into, so SP is whatever the
  frame of `Sum64`'s caller left. Rounding a base register up to 32 measured
  neutral at 1 KB and 64 KB and 3% *worse* at 256 bytes, where the two extra
  instructions sit on the path to the pipeline's first store. Zen 4 evidently
  does not care whether a 32-byte store straddles a line. Worth re-testing on a
  part that does, but it is not free: a 32-byte-aligned buffer shares its low
  bits with 32-byte-aligned input loads, and then a load 4K-aliases a pending
  store once per 4 KB of input.
- **`PCALIGN $64` on the loop heads** (16% slower), and **`PREFETCHT0`** ahead of
  the loop (slower past L2).

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
