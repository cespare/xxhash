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
taskset -c 2 go test -run '^$' -bench BenchmarkReport -benchtime 100ms .
```

Useful when working on the Go paths:

```bash
go build -tags purego -gcflags='-d=ssa/check_bce/debug=1' ./   # bounds checks
go build -gcflags=-m -o /dev/null .                            # inlining
```

`TestInlining` asserts `Sum64String` and `(*Digest).WriteString` stay inlinable;
it shells out to `go build -gcflags=-m`, so it works under any GOARCH.

For assembly, `go tool objdump` cannot decode AVX beyond SSE — use binutils
`objdump -d` to check what the Go assembler actually emitted.

## Benchmarks

`bench_test.go` is for profiling one build. `benchmark_report_test.go` is for
comparing two, and BENCHMARK.md is what it produced: this tree against
`998dce2`, the last commit before the optimization work started.

- `BenchmarkReportDispatch` — what a caller gets, with the package choosing its
  own block loop. It uses nothing but the public API, so the file drops into an
  older tree unchanged; this is the comparison.
- `BenchmarkReportKernel` — forces each block loop in turn, so that a change to
  one of them can be read without the dispatch thresholds in the way. It starts
  at 256 bytes: below `vecCutoff` the assembly runs the scalar loop whatever the
  feature flags say, so a forced vector kernel below it measures the scalar one
  and reads as a suspiciously flat row. That cutoff is written out in the test
  rather than exported from the assembly, because the two trees being compared
  need not agree on it and the comparison has to hold the length constant.
- `BenchmarkReportChunks` — writes that don't fill out a block, the only
  benchmark here that reaches the short-write path in `Write`.

Benchmarks are noisy at the few-percent level. Pin to a core with `taskset`, use
`-count 10`, and compare with `benchstat`. Anything under ~1.5% is probably code
alignment, not your change.

On a laptop this is worse than it sounds. On a Ryzen 8840HS with a `powersave`
governor, a single `-count 10` run of one build against another moved by more
than the effects being chased: the clock swings with thermals, and an SMT
sibling can take half the core. What eventually worked is to build both trees up
front and then alternate them, alternating which one goes first, so that
whatever drifts over the run drifts over both equally:

```bash
git worktree add /tmp/base 998dce2
cp benchmark_report_test.go /tmp/base/
cat > /tmp/base/report_stub_test.go <<'EOF'   # that tree has one block loop
package xxhash

import "testing"

func forEachImplBench(b *testing.B, fn func(*testing.B)) { b.Run("scalar", fn) }
EOF
(cd /tmp/base && go test -vet=off -c -o /tmp/base.bin .)   # vet fails there, on
go test -c -o /tmp/new.bin .                               # asm that predates it
for i in $(seq 20); do
  if (( i % 2 )); then order="base new"; else order="new base"; fi
  for arm in $order; do
    taskset -c 2 /tmp/$arm.bin -test.run '^$' -test.bench BenchmarkReport \
      -test.benchtime=100ms >> /tmp/$arm.txt
  done
done
benchstat /tmp/base.txt /tmp/new.txt   # ±2% at n=20, enough to see 3%
```

Twenty rounds is for reading a few percent. BENCHMARK.md is five, which is
plenty for the effects in it and not enough for its shortest rows — which is
what makes those rows the noise floor.

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

None of that was needed on an M2 under macOS, where there is no `taskset` and
none was wanted: repeated `-count 8` runs moved by well under 1%, so plain
`benchstat` of one build against another was enough to see 3%. To read a result
in cycles per 32-byte block rather than MB/s you need the clock, which `sysctl`
won't give you on Apple silicon: time a long chain of dependent `ADD`s, which
retires one per cycle. An M2 performance core is ~3.44 GHz.

On a Linux box where `perf` can read the PMU — an Azure Cobalt 100 VM could,
which is not a given under virtualization; check with `perf stat -e cycles true`
— stop guessing from ns/op and count cycles instead. Two things it buys:

- **Cycles per block directly.** Write the candidate loops as bare `.S` kernels
  over one buffer, wrap each in `PERF_EVENT_IOC_RESET`/`READ` around a
  `perf_event_open` on `PERF_COUNT_HW_CPU_CYCLES`, and take the minimum of
  several runs. It reads the same to three decimal places run to run, which is a
  different world from `benchstat` on a 2-core VM, and it is how the round
  shapes in the arm64 section were separated — 5.00 against 4.41 against 4.04 is
  not a difference `-benchtime` alone would have settled quickly. Latency and
  throughput probes for individual instructions are a dozen lines each in the
  same harness, and worth writing first: they tell you what the loop's floor is
  before you try to reach it.
- **Whether a regression is work or placement.** `perf stat -e
  cycles,instructions` on the two test binaries, divided by the iteration count,
  gives instructions per op. A change that retires *fewer* instructions per call
  and still takes longer is code placement, not the change; that is what the
  64-byte row turned out to be, at 132.7 instructions against 129.4.

The Go-level A/B still needs the alternating harness above, and on a 2-core VM
pin to core 1 and leave core 0 to everything else.

Read the rows whose code the change cannot execute a single instruction of
before any other. They are the noise floor, and the cheapest way to catch a
harness that is lying to you: if they aren't flat, the run is invalid however
plausible the rest of the table looks. In BENCHMARK.md the sizes under 32 bytes
are that check — they never reach a block loop at all.

Two artifacts `benchmark_report_test.go` exists to avoid, both of which move the
untouched rows:

- **Allocating per size.** `bench_test.go` does `make([]byte, n)` for each size,
  which makes the data pointer's alignment a function of the size and of the
  whole allocation sequence, and across two builds those sequences differ. Slice
  every size out of one buffer instead.
- **Never writing the buffer.** A fresh anonymous mapping that has only been
  read from is backed by one shared zero page, so the megabyte sizes would sit
  in L1 and measure nothing. Fill it once.

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

`Write` moves writes of up to 8 bytes into the block buffer itself instead of
calling `copy`, and that shouldn't be simplified back either: `copy` of a length
the compiler doesn't know is a call to `runtime.memmove`, and on a path that
short the call costs more than the move — it also forces the receiver and the
length to be spilled around it. It is worth 12% of a stream of 8-byte writes and 30% of `Digest` on
4 bytes (`BenchmarkDigestChunks`, `BenchmarkDigestBytes/4B`). Longer writes are
left to `memmove`, which by 16 bytes is already down to a couple of SSE moves;
both open-coding those lengths and merely testing for the short case one branch
later measured worse.

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
32-byte block, and that is the floor here. Measured on Zen 4 the vector loops sit
at about 5.1-5.3, so there is only a few percent left in the block loop itself;
the scalar loop's eight multiplies put it at 8.

Five is the floor for x86 specifically, because x86 has no integer multiply-add.
The arm64 section below gets to four with one, by carrying `acc + x*prime2`
across the loop instead of `acc`, which folds the add into the multiply. There
is no way to spell that here: `LEA` doesn't multiply by `prime1`, and the
add has to happen after the rotate.

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

## arm64 assembly

`xxhash_arm64.s` is scalar throughout — one `blockLoop`, no vector path, no
feature detection. The block loop is bounded by accumulator latency and by the
one-per-cycle `MADD`, so the work went into the shape of the round instead.

The round is carried part-rotated. Substituting `w = rol31(acc + x*prime2)`, so
that `acc = w_prev * prime1`, turns

    acc = rol31(acc + x*prime2) * prime1

into

    w = rol31(w_prev * prime1 + x*prime2)

which is a `MADD` and a rotate — a four-cycle chain, against seven for the
straightforward `MADD`/rotate/multiply and five if you only split the `MADD` in
two. `preRound` establishes `w` for the first block, `round` carries it, and
`finishRound` converts back with the one multiply the substitution leaves
outstanding; `blockLoop` peels the first block and finishes after the last, so
the per-block instruction count is unchanged. Measured on an Apple M2, against
the straightforward form: +64% at 64KB, +59% at 4KB, +20% at 256 bytes, +3% at
64, and a wash at 32, where the peel and the finish are the whole loop. (That
last case is why `preRound` is a `MADD` and `round` is not — see the comment on
it.)

**Which side of the `MADD` the rotate sits on is not free**, though it reads
that way. Carrying `u = acc + x*prime2` and spelling the round rotate-then-`MADD`
is the same three instructions and the same four-cycle chain, and it is how this
loop was written until it was run on a Neoverse N2 — where it costs 5.0 cycles a
block against 4.4 for the form above. Nothing in the chain or the port counts
accounts for that, and six other schedules of the rotate-first form (products
hoisted a block ahead, products first, phase-ordered, unrolled by two, offset
addressing, counter early) all came in at 5.0 or worse. Keep the rotate after
the `MADD`.

**The loop takes two blocks an iteration.** The rounds don't get faster for it —
the four `MADD`s still need their four cycles — but the decrement and the branch
go from a sixteenth of the instruction stream to a thirtieth, and on N2 that is
4.41 cycles a block down to 4.04, which is the floor. An odd block runs on the
way in rather than jumping into the middle of the pair, and shares the pair
loop's test for an empty count; both of those are there so that a two-block
input, which never reaches the loop, executes no more instructions than it did
when the loop went one block at a time. Getting that wrong cost 2% at 64 bytes
in two different ways before it cost nothing.

### What a Neoverse N2 looks like from in here

Measured with `perf` cycle counters on an Azure Cobalt 100 (Neoverse N2, ~3.4
GHz), because the numbers above this line are all Apple numbers and two of them
do not carry:

| | latency | throughput |
| --- | --- | --- |
| `ADD`, `ROR` | 1 | 4/cycle |
| `MUL` (64-bit) | 2 | 2/cycle |
| `MADD` (64-bit) | 2 through a multiplicand, **1 through the addend** | **1/cycle** |

Two consequences:

- **Four `MADD`s a block is a four-cycle floor.** The loop measures 4.04-4.16
  cycles a block at 4KB and up, so there is nothing left in it. Anything that
  keeps four multiply-adds per block is done here, whatever else it does.
- **N2 forwards a `MADD`'s addend in a cycle and the M2 does not.** That makes
  the straightforward round a four-cycle chain on N2 (`MADD` 1, `ROR` 1, `MUL`
  2) rather than seven, and it measures 4.4-4.6 — faster than the *rotate-first*
  carried form, though not than the form in the file. The seven-cycle figure
  further up is an Apple number. Don't quote it as a property of arm64.

Tried on a Neoverse N2 and rejected, so as not to be tried again without a
reason:

- **SVE for the input products.** N2 has SVE2, and unlike anything in NEON,
  `MUL Zd.D` is a real 64x64 multiply. A pipelined loop that took the four
  `x*prime2` in two SVE multiplies and handed them to the scalar `MADD`s through
  a stack buffer measured 4.0 cycles a block — the floor the scalar loop now
  reaches anyway, for a great deal more machinery. It is also close to
  unshippable: Go's assembler doesn't accept SVE at all (`MUL Z2.D, Z0.D, Z1.D`
  is a parse error), so every instruction would be a hand-encoded `WORD`, and
  arm64 has no dependency-free way to detect SVE at run time — no CPUID, and
  `HWCAP` needs cgo or `/proc`. On the numbers it wouldn't be worth it even if
  it were free: SVE `MUL Zd.D` is 2 cycles per instruction on N2, one 64-bit
  product per cycle, against two for the scalar `MUL`.
- **Splitting the `MADD` into `MUL`+`ADD`** to dodge the one-per-cycle limit.
  Eight multiplies a block at two a cycle is also four cycles, but it is 20
  instructions against 16, and it measured 5.3-5.9.
- **Reaching the merge round without the accumulator's last multiply.**
  `mergeRound` wants `rol31(acc*prime2)*prime1`, and `blockLoop` leaves the
  accumulator one `*prime1` short, so multiplying what the loop left by
  `prime1*prime2` gets there two cycles earlier for the same four multiplies.
  Measured 1.4-3% *slower* at 32-256 bytes, whether the constant came from an
  immediate or from a sixth entry in `primes`. That path has more slack in it
  than its chain suggests.
- **Moving the loop head's `PCALIGN`.** 32 and 64 instead of 16, and dropping it
  altogether: all within about 1% of each other at every size, and the sizes
  that never reach the loop moved as much as the ones that do, which is the
  signature of measuring placement rather than alignment. The three `NOOP`s it
  emits cost nothing readable, so it stays as it was.

**Don't add a NEON block loop.** This was built and measured, not assumed:
NEON has no integer multiply of any width in Go's assembler (`VPMULL` is
carry-less), but the 64x64 can be synthesized in eight ops per block from
`UZP1`/`UZP2`, a 32-bit `MUL`/`MLA` for the cross term, `ZIP1`/`ZIP2` against
zero to place it (`USHLL` cannot shift a 32-bit element by 32), and
`UMLAL`/`UMLAL2`. A complete pipelined version of that, mirroring the amd64
group-buffer design, ran **5-27% slower** than the scalar loop at every length
that reached it, at group sizes 2, 4 and 8 blocks. The reason is structural:
Apple cores retire one `MADD` per cycle, so the four per block need four cycles
on their own — exactly the length of the chain. The input multiplies are not the
constraint, so moving them off the integer pipes buys nothing and the vector
half's extra ~20 uops per block displace `MADD` issue. This is the opposite of
amd64, where a single 1/cycle `IMUL` port *is* the constraint, which is why the
vector loops pay off there.

If you do revisit it: hand-assembled instructions go in as `WORD $0x...`, and
`go tool objdump` decodes them (unlike the AVX case above), so the annotations
can be checked against the built package.

Two cores have now been measured, an Apple M2 and a Neoverse N2, and they agree
on the shape of the round and disagree about why. Everything else — Graviton,
Ampere, the phone cores — is still unmeasured, and qemu gives correctness, not
timing, so don't "optimize" it blind. The carried round should be a win anywhere
`MADD` latency is at least that of `MUL`; four `MADD`s a block is the floor
anywhere `MADD` is one per cycle, which is both of the cores above.

## The pure-Go block loops

`xxhash_other.go` carries the same rearrangement, in `preRound`/`carryRound`/
`finishRound`. It went in there because that file is what arm64 compiles to
under `purego` or `appengine`, and what every architecture without assembly
gets: on an M2, `Sum64` goes +29% at 4KB and +36% at 64KB. On x86 it goes the
other way — see below.

The catch is that `carryRound` is `a*b + c*d`, and only one of those multiplies
is the one on the dependency chain. Which one the compiler folds into the
multiply-add is its choice and not expressible in the source — operand order,
naming the products in their own statements, and splitting the assignments apart
all produce identical code, and removing an unrelated line *after* the loop
flipped all four accumulators at once. Go 1.26 picks correctly for all four in
`Sum64` and three of four in `writeBlocks`, which is why `Digest.Write` only
gains ~5% where `Sum64` gains ~30%.

Don't chase the last accumulator by rotating the loop so the products arrive
through a phi (which would pin the choice). It costs four more values live
across the back edge, and the targets that have no assembly at all are the
32-bit ones, where 64-bit values take register pairs and that is a spill.

Nor does the arm64 assembly's rotate placement carry over here. Writing
`carryRound` as `rol31(w*prime1 + input*prime2)` — the same substitution the
assembly makes, which is worth 12% there — measured 8% slower for `Sum64` and
20% slower for `Digest` at 4KB and up on a Neoverse N2, the machine the
assembly form was tuned on. The assembly gets to choose the schedule; here the
compiler chooses it, and this spelling makes it choose worse. It is the same
lesson as the paragraph above, in the other direction.

**On x86 the rearrangement is a loss, and the argument that it was free was
wrong.** Measured natively on Zen 4 with go1.26.5, `-tags purego`, against
`c581032`, the commit before it: `Sum64` is 10-17% slower from 4 KB up (+14%
geomean, n=20, p=0.000 — 314 ns → 365 ns at 4 KB, 4.98 us → 5.50 us at 64 KB)
and `Digest` 4-10% slower. BENCHMARK.md has the table. Benchmarking the amd64
build under Rosetta does not catch this: it showed +5%, because the translation
runs on arm64 and can fuse what x86 cannot.

The half of the argument that held is the chain. x86 has no integer
multiply-add, so add/rotate/multiply and rotate/multiply/add are both three
dependent instructions, five cycles either way. What it missed is that the
pure-Go loop on x86 is nowhere near chain-bound: eight 64-bit multiplies per
block against one multiply port floor it at 8 cycles, and both forms measure at
least that anywhere in this part's 3.3-5.1 GHz range — never near 5. So the
shorter chain buys nothing, and the cost is real. `carryRound` needs
`input*prime2` live in a register of its own per accumulator, and go1.26.5 duly
keeps four products live and groups the four `ADDQ`s at the end of the body,
where the plain round feeds each product straight into its accumulator and
reuses one scratch register four times. The bodies are otherwise the same — 29
instructions against 30, eight `IMULQ` either way — so what is left is the
scheduling.

That makes this a trade rather than a free win: +29/+36% on an M2, -10/-17% on
Zen 4, and unmeasured on the targets that have no assembly at all, which are the
ones that actually run this file in production. If it is worth splitting, the
lever is a build tag; the two forms are three lines each.

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
