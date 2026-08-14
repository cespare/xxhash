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
./testall.sh                       # the whole matrix, the other arch via qemu

go test -run 'TestSum64Reference/avx2' -v .        # one test, one implementation
taskset -c 2 go test -run xxx -bench 'Sum64/4KB' -benchtime 300ms -count 10 .
taskset -c 2 go test -run '^$' -bench BenchmarkReport -benchtime 100ms .
```

Useful when working on the Go paths:

```bash
go build -tags purego -gcflags='-d=ssa/check_bce/debug=1' ./   # bounds checks
go build -gcflags=-m -o /dev/null .                            # inlining

# What a block loop actually costs: the span from the backward branch's target to
# the branch. Count the NOOPs separately -- they are Go's inline marks, one per
# inlined call that didn't land on a real instruction, and they were a third of
# this loop's instruction stream before it was written out to call nothing.
go test -tags purego -c -o /tmp/pg.bin .
go tool objdump -s 'xxhash/v2.writeBlocks$' /tmp/pg.bin
```

`TestInlining` asserts `Sum64String` and `(*Digest).WriteString` stay inlinable;
it shells out to `go build -gcflags=-m`, so it works under any GOARCH.

For assembly, `go tool objdump` cannot decode AVX beyond SSE — use binutils
`objdump -d` to check what the Go assembler actually emitted.

`testall.sh` runs natively on whichever of amd64/arm64 it finds itself on and
sends the other through `qemu-user`, plus 386, arm and riscv64 for the pure-Go
loops on a 32-bit and a no-assembly 64-bit target. It needs `qemu-user-static`.

**qemu cannot reach the AVX512 block loop.** QEMU's TCG implements no AVX512 at
all: under `qemu-x86_64-static`, even with `-cpu max`, `CPUID(7,0)` reports
neither AVX512F/DQ/VL nor the ZMM bits in `XCR0` (`xcr0=0x21f`), so
`forEachImpl` runs scalar and AVX2 and logs a skip for avx512. Cross-testing amd64
from an arm64 host therefore covers two of the three block loops; the third needs
hardware, and the last time it had it is recorded under "The AVX512 path has been
executed" below.

## Benchmarks

`bench_test.go` is for profiling one build. `benchmark_report_test.go` is for
comparing two, and BENCHMARK.md is what it produced: this tree against
`998dce2`, the last commit before the optimization work started.

- `BenchmarkReportDispatch` — what a caller gets, with the package choosing its
  own block loop. It uses nothing but the public API, so the file drops into an
  older tree unchanged; this is the comparison.
- `BenchmarkReportKernel` — forces each block loop in turn, so that a change to
  one of them can be read without the dispatch thresholds in the way. It starts
  at 224 bytes, the lower of the two cutoffs: below a cutoff the assembly runs
  the scalar loop whatever the feature flags say, so a forced vector kernel there
  measures the scalar one and reads as a suspiciously flat row. Between the two
  cutoffs that is still true of the `Digest` rows, which is why they read flat
  from 224 to 256 while the `Sum64` rows move. That threshold is written out in
  the test rather than exported from the assembly, because the two trees being
  compared need not agree on it and the comparison has to hold the length
  constant.
- Forcing a kernel is also how to settle *where* a cutoff belongs, and it is the
  only way that works: comparing two builds with different cutoffs measures code
  placement as much as the change — in one such run the 96-byte row, whose code
  is identical in both, moved 5%. Forcing scalar against vector at one length
  inside one binary has no such problem.
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

**Counting cycles per op without writing a `.S` kernel.** Run the same Go
benchmark at two iteration counts and difference them: every fixed cost — process
startup, package init, the testing package, the warm-up iteration — cancels, and
what is left is the loop.

```bash
run() { taskset -c 3 perf stat -x, -e cpu_core/cycles/,cpu_core/instructions/ \
          ./x.bin -test.run '^$' -test.bench "$1" -test.benchtime "$2x" 2>&1; }
# per-op = (cycles(N2) - cycles(N1)) / (N2 - N1)
```

Four things this needs to be right. Anchor the `-test.bench` regex
(`'^BenchmarkReportDispatch$/^4096$/^Sum64$'`), or it also runs the sibling
benchmarks and the difference is of the wrong thing. Minimise each iteration
count over several rounds *separately* and difference the two minima — the
minimum of a difference is biased by noise in the subtrahend, and will happily
report a negative cycle count. Make N2 large enough that the delta dwarfs the
fixed part; with `benchmark_report_test.go`'s 8 MB buffer, package init alone is
tens of millions of cycles, so at 20k/200k iterations a 54-instruction benchmark
still carries ±1% of instruction noise, and 200k/2M is what makes the untouched
rows read as ±0.6%. And on a hybrid Intel part the event must name the PMU
(`cpu_core/cycles/`, not `cycles`) and the process must be pinned to a P-core,
or half the counts come back `<not counted>`.

Cycles from the PMU don't care about the clock, which is what makes this work on
a laptop under `powersave` where ns/op does not. Instructions per op comes back
stable to about 0.04% at 4 KB, and is the metric to trust below 32 bytes, where
the benchmark loop overlaps consecutive independent calls and the cycle figures
stop being additive — a 4-byte `Sum64` measuring *fewer* cycles than a 0-byte one
is that, not a mistake. `perf_event_paranoid` must be 1 or lower.

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

That boundary was re-tried at 16 on a Neoverse N2, where `memmove` is a call
rather than a couple of moves, and it loses there too — for a reason worth
knowing. Widening the first arm from `n == 8` to `n >= 8`, so that one case
covers 8 to 16 by writing the front and the back, makes the *common* eight-byte
write store the same word twice: 9% worse on a stream of them, against under 1%
gained at 16, which is itself inside the noise. Keeping `n == 8` exact and adding
a third arm for 9 to 16 would fix that and put another branch on the shortest
path. The boundary is where it is on purpose.

**`Write` skips its trailing `copy` when there is nothing left to copy**, which
is not a micro-optimization but a call removed from a common path. The last thing
`Write` does is buffer the sub-block remainder, and the remainder is empty for
every write that ends on a 32-byte boundary — every write of a multiple of 32,
and every second write of 16. `copy` of an unknown length is a call to
`runtime.memmove`, so that was a call to move zero bytes. Guarding it costs one
instruction when the remainder is non-empty and saves about twelve when it is
not, and it is worth -5.6% on a stream of 16-byte writes and -5.1% to -9.6% on
32- and 64-byte ones (`BenchmarkReportChunks`). This is the one part of the
pure-Go work that the assembly builds also get, since `Write` is in `xxhash.go`.

**The tails in `Digest.Sum64` and in `xxhash_other.go`'s `Sum64` keep an offset
into the buffer rather than reslicing it**, for the reason in the pure-Go
section: a reslice whose result the compiler can't prove non-empty costs five
instructions, and the old tail paid that twice. Two details are load-bearing.
The guards are spelled `p+8 <= len(b)` and not `len(b)-p >= 8`, because only the
first is the fact the prove pass needs to drop the bounds check — the second
form compiles with two of them. And the reads are `b[p:p+8]`, a window whose
length the compiler knows, so the pointer advance is unconditional.

It is a trade, not a free win, and the direction depends on the remainder. Each
step that runs saves its five-instruction clamp, but each step that is *skipped*
now costs a `LEAQ` and a merge copy, because `p` is a variable where the old
guards compared against constants. Measured on Redwood Cove: `Digest` retires
4.6% fewer instructions on a 4-byte remainder, 4.9% on 8 and 7.1% on 31, and
pure-Go `Sum64` 12.3% fewer at 31 bytes (-9.0% cycles) and 7.4% at 4 (-3.2%);
against that, a remainder of 0 to 3 bytes — which includes every length that is
a multiple of 32 — pays about 6 instructions, and pure-Go `Sum64` of 1 byte is
8.9% worse. Averaged over the 32 possible remainders it is about -5
instructions. In the assembly build the cycle counts barely move either way:
that tail is a serial chain of `tailRound8`s, so removing address arithmetic
from around it buys nothing on a wide out-of-order core. It is retired
instructions and I-cache, and it is the pure-Go build that shows it as time.

## amd64 assembly

`xxhash_amd64.s` has four entry points. `Sum64` and `writeBlocks` are frameless
and handle short inputs with the plain scalar block loop; for long enough inputs
— at least `sumCutoff` (224) bytes and `writeCutoff` (256) respectively — they
**tail-jump** (`JMP ·sum64Vec(SB)`) to `sum64Vec` / `writeBlocksVec`, which have
a stack frame for the group buffer. The split exists so short inputs never pay
for the frame. The `Vec` functions are declared in `cpu_amd64.go` purely so
`go vet` can check their `FP` references; nothing calls them from Go.

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

Both floors hold on Intel as well. Probed with `perf` on a Redwood Cove P-core
(Core Ultra 9 185H), `IMULQ r64,r64` is 3 cycles' latency and exactly one per
cycle, `ROLQ` by an immediate is 1 and two per cycle, and an `ADDQ`/`ROLQ`/
`IMULQ` chain measures 5.015 cycles a round. The AVX2 loop's marginal cost there
is 5.16 cycles a block at 4 KB and the scalar loop's is 8.0 to three figures, so
neither has anything left worth chasing. `VPMULUDQ` is two per cycle and
`VPMULLD` one, which is why the AVX2 product stays three `VPMULUDQ`s: folding
the two cross terms into one `VPMULLD` would trade three cheap multiplies for
one that costs a whole port-cycle.

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
- `vecGroupSize` must stay a power of two (the loop bound is a mask), and both
  cutoffs at least as large as it, since the pipeline prologue reads a whole
  group before the first bound is tested. All three were tuned by measurement:
  four blocks a group beats two (5% at 1 KB) and eight (7% at 512 bytes), and
  entering the vector path too early loses.
- **The two cutoffs are not the same number, and that is deliberate.** The
  crossover is a property of the entry point as well as the core. Forcing each
  kernel at a fixed length on a Redwood Cove P-core, the vector loop overtakes
  the scalar one for `Sum64` at 224 bytes (5.5% ahead there, level at 192, 3.8%
  behind at 160) but not for `writeBlocks` until somewhere past 256 — still 2%
  behind at 255, 4% behind at 256, and only 4% ahead by 384. Same block loop,
  same length, opposite verdict. That is not a draw in the 4K-aliasing lottery:
  it holds at every input offset from 0 to 3072. So `Sum64` takes the lower
  crossover and `writeBlocks` keeps the 256 that was tuned on Zen 4, which is
  the one of the two that no measurement has argued down. Why `writeBlocks`
  starts paying so much later is not understood; whoever finds out should also
  re-check whether 256 is still right for it.
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

The two block loops themselves don't call `carryRound`, and are spelled out in
terms of `bits.RotateLeft64` and `binary.LittleEndian.Uint64`. That is not a
style lapse and it is not negotiable for readability: each inlined call leaves a
NOP in the loop, and there were twelve of them. `preRound`, `carryRound` and
`finishRound` are still what the peel, the leftover block and the conversion use,
because those run once a call. See "What the loop's instruction count was made
of" below.

The catch is that `carryRound` is `a*b + c*d`, and only one of those multiplies
is the one on the dependency chain. Which one the compiler folds into the
multiply-add is its choice and not expressible in the source — operand order,
naming the products in their own statements, and splitting the assignments apart
all produce identical code, and removing an unrelated line *after* the loop
flipped all four accumulators at once. Re-checked against go1.26.5 after the
loops were written out by hand (see below), including swapping the two operands
of the addition, which changes nothing: the fusion is picked after Go has
canonicalised the operand order, so the source can't reach it.

As it stands both loops fuse the *input* multiply, so the carried value arrives
as the multiply-add's addend and the link is rotate (1), multiply (2),
multiply-add through the addend (1) — a cycle longer than the best case. **It
does not matter, and that is the useful part.** The loop measures 5.6 cycles a
block against a four-cycle chain, so what binds is throughput; a cycle of chain
either way is invisible. The earlier reading of this — that `Digest.Write` gained
~5% where `Sum64` gained ~30% because the fusion went three-of-four rather than
four-of-four — attributed to the chain what was really instruction count. Both
functions now compile to the same 18-instruction loop and measure within 0.1
cycles a block of each other.

Don't chase the last accumulator by rotating the loop so the products arrive
through a phi (which would pin the choice). It costs four more values live
across the back edge, and the targets that have no assembly at all are the
32-bit ones, where 64-bit values take register pairs and that is a spill.

Nor does the arm64 assembly's rotate placement carry over here. Writing
`carryRound` as `rol31(w*prime1 + input*prime2)` — the same substitution the
assembly makes, which is worth 12% there — measured 8% slower for `Sum64` and
20% slower for `Digest` at 4KB and up on a Neoverse N2, the machine the
assembly form was tuned on. It retires the same instructions to do it: at 4KB,
5210 against 5235 per call for `Sum64` and 5336 against 5398 for `Digest`, so
under 1.5% either way, while cycles go up 7.8% and 16.8% and IPC drops from
5.72 to 5.33 and from 5.25 to 4.55. Same work, more waiting, and for two
different reasons:

- **In `Sum64` the products lose their slack.** Both loops are 25 real
  instructions with the same mix and the same three-cycle chain. The difference
  is spacing: the current spelling computes all four `input*prime2` up front and
  consumes them in four back-to-back `MADD`s eight to fourteen instructions
  later, where the other emits `MUL`/`MADD`/`MUL`/`MADD` and every `MADD` sits
  one instruction behind the multiply feeding its addend.
- **In `writeBlocks` the compiler fuses the wrong multiply.** For two of the
  four accumulators it emits `v*prime1` as a standalone `MUL` and folds the
  *input* multiply into the `MADD`, so the loop-carried value arrives as the
  addend and the link is multiply (2), multiply-add (1), rotate (1) rather than
  three. The current spelling gets that wrong for one accumulator of four,
  which is the "three of four" above, confirmed by disassembly rather than
  inferred; the rearranged one gets it wrong for two, and that is the whole of
  why `Digest` regresses twice as hard as `Sum64`.

Underneath both: this loop is not chain-bound and has no business being tuned
as though it were. It used to run at 7.8 cycles a block on N2 against the
assembly's 4.1, nowhere near the four-cycle multiply-add floor, because it
carried 37 slots a block against the assembly's 15. Shortening a chain with that
much headroom buys nothing and the scheduling it disturbs costs something; the
instructions were where the time was, and taking 19 of them out is worth -20% to
-28%. That work is done — see "What is actually left in this file" below for what
it was and what remains.

To check which way the fusion went, disassemble rather than read the source:
`go tool objdump -s 'xxhash/v2.writeBlocks$'` on a `-tags purego` test binary,
find the backward branch, and look at whether each `MADD`'s loop-carried
operand is one of the two multiplicands (good) or the addend (a cycle worse).

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

**It is a loss on that Zen 4 and not on x86 in general, so don't split it by
build tag.** The same two builds, compared on a Redwood Cove P-core with `perf`
cycle counters, come out the other way round: at 1 KB, 4 KB and 64 KB the
carried form is within 1.4% of the plain one for `Sum64` (-0.8%, -1.4%, +0.1%)
and 4-6% *ahead* for `Digest`. That is the same machine code both places, so the
disagreement is the core, not the compiler.

The reason neither form wins there is that both are already on the floor: 8.10
and 8.13 cycles a block at 64 KB, against the 8.0 that one multiply port and
eight multiplies allow. What differs is only how much slack each leaves — the
plain form retires 48 instructions a block against the carried form's 41, which
is IPC 5.9 against 5.1 on a machine that renames six a cycle. So on Redwood Cove
the extra instructions fit in the shadow of the multiply port and cost nothing,
and on Zen 4 something about them evidently does not. A build tag can only tell
`amd64` from `arm64`; it cannot tell these two apart, and picking either form
for all of x86 would be picking against one of them.

Note also that the "29 instructions against 30" above counts only the real ones.
Both loops carry a lot of single-byte `NOPL` that the compiler inserts for
statement boundaries — 9 a block in the carried form and 16 in the plain one —
and those are a third of the plain loop's instruction stream. Anyone re-opening
this question should count what `objdump` actually shows rather than what the
source suggests.

That makes this a trade rather than a free win: +29/+36% on an M2, -10/-17% on
Zen 4, roughly neutral to +4% on Redwood Cove, and unmeasured on the targets that
have no assembly at all, which are the ones that actually run this file in
production. Three cores, three answers, and no lever that can express that.

### What the loop's instruction count was made of

Not the round. On Redwood Cove the loop is at its 8-cycle multiply-port floor, so
nothing about the arithmetic can move it there, and on N2 it was 3.7 cycles above
a floor it had no business being near. What was actually in it was instruction
count, which only matters on cores narrow enough to notice — and it was 37 slots
a block against the assembly's 15. Three things accounted for 19 of them, and all
three are now taken. Measured on a Neoverse N2, they bring the loop to 18
instructions and 5.59 cycles a block, `Sum64` -23% at 4 KB and -28% at 64 KB and
`Digest` -21%; BENCHMARK.md has the tables.

- **A reslice the compiler can't prove non-empty costs five instructions** (-3).
  Go won't leave a pointer one past the end of an object, so `b = b[32:]`
  compiles to `ADDQ`/`MOVQ`/`NEGQ`/`SARQ`/`ANDL`/`ADDQ` on amd64 and
  `SUB`/`NEG`/`ASR`/`AND`/`ADD` on arm64 — an advance made conditional on the new
  length. Bounding the loop at 64 so it leaves a whole block behind, and doing
  that block after it, makes the result provably non-empty and collapses the
  advance to one add. The leftover block pays the conditional form once a call.
- **A constant is what the register allocator will not keep live** (-4). go1.26.5
  rebuilt `prime1` on arm64 out of a `MOVD` and three `MOVK`s every iteration,
  because a rematerializable value is cheaper to recompute than to hold — which
  is true when recomputing it is one instruction, as on amd64, and false when it
  is four. Reading it out of the `primes` array instead gives the allocator a
  load, which it cannot rematerialize and so must keep in a register. This is why
  `primes` now has two reasons to exist; `TestInitConstants` pins its order.
- **Every inlined call leaves a NOP behind** (-12). Go emits an inline mark per
  inlined call so a traceback can name the frame, and a mark that doesn't land on
  the address of an instruction the function was emitting anyway survives as a
  NOP. Four accumulators' worth of `carryRound` and `u64` left **twelve NOPs in a
  loop whose real work is eighteen instructions** — a third of the stream, and
  the same order of magnitude on amd64, ppc64le and loong64. Writing the body out
  in terms of `bits.RotateLeft64` and `binary.LittleEndian.Uint64` so that it
  calls nothing removes all of them. This is the least obvious of the three and
  the one most likely to apply elsewhere: any hot loop in this package that calls
  an inlinable helper per iteration is carrying them. It is worth only ~4% by
  itself on N2 — NOPs are nearly free to issue — but it is what makes the other
  two readable, and on a narrower core it should be worth more.

Two things that do not work, so as not to be tried again:

- **Rewriting the loop with an index instead of a reslice.** `for p := 32;
  p+32 <= len(b); p += 32` with `b[p+24:p+32]` puts the bounds checks back:
  `p+32` can overflow in principle, so the prove pass won't chain the guard to
  the loads. Cutting a `blk := b[p : p+32]` window first doesn't rescue it —
  that slice bound is unproven too. The reslicing form is the one Go's prove
  pass handles.
- **Taking two blocks an iteration**, the way `xxhash_arm64.s` does, where it is
  worth 4.41 → 4.04 cycles a block. Here it retires 16 instructions a block
  rather than 18 and ran `Sum64` **7% slower** on N2 (`Digest` 1% faster), which
  by this repo's own rule — fewer instructions and more time — is code placement
  rather than the change. For five copies of the round in the source, on a loop
  that is no longer close to its overhead, it is not worth re-litigating without
  a core where the loop is front-end bound.

**Every target this file compiles for retires fewer instructions per block for
these three changes**, which is the check that matters given that the cores which
actually run it can't be measured here. `writeBlocks`' inner loop, in slots:
arm64 37 → 18, amd64 42 → 25, ppc64le 41 → 24, loong64 43 → 24, riscv64 108 →
101, arm 221 → 179, 386 311 → 282. The 32-bit targets were the worry — a `uint64`
held live costs a register pair there — and they gained two stack references
between them while shedding 29 and 42 slots, so the spill didn't happen. To
re-run that audit, cross-compile a `-tags purego` test binary per GOARCH and
disassemble `writeBlocks`; no execution is needed and `go test -c` is enough.

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
