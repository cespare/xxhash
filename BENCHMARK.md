# Benchmarks

This tree against `998dce2`, the last commit before the optimization work, on
two machines: an amd64 one below and an arm64 one at the end of the file. Each
was measured when the code it covers last changed, and each says which commit
that was; a change to one architecture's assembly doesn't move the other's
numbers, and neither table is re-run when it hasn't.

# amd64

|           |                                                                          |
|-----------|--------------------------------------------------------------------------|
| CPU       | AMD Ryzen 7 8840HS (Zen 4, 8 cores / 16 threads, 16 MB L3)                |
| Go        | go1.26.5 linux/amd64                                                     |
| Baseline  | `998dce2` Add initial support for custom seeds                           |
| This tree | `fc098a7` Move short writes into the block buffer without calling memmove |

## Method

Both trees are built up front and run alternately, swapping which one goes first
each round, twenty rounds, each run pinned to one core. Every figure is the
median. `benchmark_report_test.go` is the harness; CLAUDE.md has the procedure
and the reasons for each part of it.

Read the ns/op and GB/s as indicative rather than as peak figures for this
hardware. It is a laptop part on a `powersave` governor whose benchmark core
ranges over roughly 3.3-5.1 GHz depending on thermal state and on what else the
desktop is doing. Interleaving is what makes the *comparison* hold; it is also
why nothing here is comparable to a number recorded in a different session.

## Reading the tables

Every cell is `baseline → this tree (change)`. The rate column is this tree's
throughput on `Sum64`.

**The noise floor is the scalar column** of the forced-kernel table. `round()`
and `blockLoop()` in `xxhash_amd64.s` are byte-identical in the two trees, so
whatever that column reports above 256 bytes is the harness rather than the
code. It comes in at ±1% from 384 bytes up and ±2% at 256, which is what makes
the rest of those tables readable.

Below 256 bytes there is no such control — the tail path was rewritten too — so
the substitute is `Sum64` against `Sum64String`, which are the same machine code
reached two ways. Their deltas disagree by up to 9 percentage points under 32
bytes and by at most 2.3 above 256. **Treat anything under 32 bytes as flat
unless it moves by more than about 10%.**

Three more things worth knowing before reading a row:

- Below 256 bytes (`vecCutoff`) there is no vector kernel at all, so the
  forced-kernel tables start there. Forcing one below that measures the scalar
  loop and says nothing.
- The 255-byte row is slower than the 256-byte row in both trees, which is not
  an artifact: 256 bytes is eight whole blocks and no tail, 255 is seven blocks
  plus 31 bytes of tail.
- 1 MB and 8 MB still fit in this part's 16 MB L3. The rate drops a little
  against 64 KB, but the block loop is still what is being measured.

## What moved

- **1 KB and up: -24% to -35%**, and all of it is the vector block loops — the
  scalar column is flat across the same range.
- **256 to 512 bytes: -11% to -20%**, part kernel and part the crossover that
  lets those lengths reach it at all.
- **Under 256 bytes: nothing clearly above the noise floor.** `Digest` reads
  -7% to -11% on 4 to 16 bytes, which is the short-write path in `Write`; on
  its own that sits at the edge of the floor, but the streaming table below
  exercises the same path with far more headroom and agrees.
- **Streaming writes below a block: -8% to -15%** for chunks of 1 to 8 bytes,
  which is that same change.
- **Pure Go went backwards: +10% to +17% from 384 bytes up.** See below.

## Dispatched

What a caller gets, with the package choosing its own block loop.

| Bytes | Sum64 | Sum64String | Digest | rate |
|------:|--------|--------|--------|-----:|
| 0 | 2.17 ns → 2.36 ns (+9%) | 1.97 ns → 2.14 ns (+9%) | 4.75 ns → 4.3 ns (-9%) | — |
| 1 | 2.29 ns → 2.58 ns (+13%) | 2.2 ns → 2.37 ns (+8%) | 6.77 ns → 7.04 ns (+4%) | 0.387 GB/s |
| 4 | 2.22 ns → 2.39 ns (+8%) | 2.19 ns → 2.18 ns (-1%) | 6.38 ns → 5.91 ns (-7%) | 1.67 GB/s |
| 8 | 2.4 ns → 2.6 ns (+8%) | 2.39 ns → 2.38 ns (-1%) | 7.44 ns → 6.66 ns (-11%) | 3.08 GB/s |
| 16 | 2.91 ns → 2.8 ns (-4%) | 2.93 ns → 2.79 ns (-5%) | 8.71 ns → 7.71 ns (-11%) | 5.71 GB/s |
| 31 | 5.58 ns → 5.49 ns (-2%) | 5.54 ns → 5.35 ns (-4%) | 18.2 ns → 17.6 ns (-3%) | 5.64 GB/s |
| 32 | 6.33 ns → 6.32 ns (-0%) | 6.35 ns → 6.06 ns (-5%) | 9.83 ns → 9.88 ns (+1%) | 5.06 GB/s |
| 33 | 6.82 ns → 6.31 ns (-7%) | 6.8 ns → 6.22 ns (-9%) | 10.7 ns → 10.6 ns (-0%) | 5.23 GB/s |
| 64 | 8 ns → 8.04 ns (+1%) | 7.99 ns → 7.87 ns (-1%) | 11.2 ns → 11.3 ns (+1%) | 7.96 GB/s |
| 96 | 10.1 ns → 10.1 ns (-0%) | 9.9 ns → 9.87 ns (-0%) | 12.9 ns → 13.2 ns (+2%) | 9.52 GB/s |
| 128 | 11.4 ns → 11.4 ns (-0%) | 11.1 ns → 11.7 ns (+5%) | 14.7 ns → 15.1 ns (+2%) | 11.2 GB/s |
| 192 | 14.6 ns → 14.3 ns (-2%) | 14.7 ns → 14.4 ns (-2%) | 18 ns → 18.6 ns (+3%) | 13.4 GB/s |
| 255 | 20.2 ns → 20.7 ns (+3%) | 20.5 ns → 20.8 ns (+1%) | 26.9 ns → 25.5 ns (-5%) | 12.3 GB/s |
| 256 | 18.4 ns → 16.1 ns (-13%) | 18.6 ns → 16.5 ns (-11%) | 21.3 ns → 20.5 ns (-4%) | 15.9 GB/s |
| 257 | 18.4 ns → 17.1 ns (-7%) | 18.4 ns → 17 ns (-8%) | 22.4 ns → 21.3 ns (-5%) | 15 GB/s |
| 384 | 24.7 ns → 20.6 ns (-17%) | 24.8 ns → 20.9 ns (-16%) | 27.9 ns → 24.7 ns (-12%) | 18.7 GB/s |
| 512 | 30.9 ns → 24.8 ns (-20%) | 31.5 ns → 25.1 ns (-20%) | 34.7 ns → 29.1 ns (-16%) | 20.7 GB/s |
| 1 KB | 58.8 ns → 41.9 ns (-29%) | 58.3 ns → 42.7 ns (-27%) | 61.7 ns → 46.7 ns (-24%) | 24.5 GB/s |
| 4 KB | 220 ns → 145 ns (-34%) | 220 ns → 149 ns (-32%) | 223 ns → 153 ns (-31%) | 28.2 GB/s |
| 16 KB | 863 ns → 559 ns (-35%) | 863 ns → 579 ns (-33%) | 870 ns → 577 ns (-34%) | 29.3 GB/s |
| 64 KB | 3.44 us → 2.26 us (-34%) | 3.43 us → 2.32 us (-32%) | 3.45 us → 2.34 us (-32%) | 29 GB/s |
| 1 MB | 55.7 us → 39.2 us (-30%) | 55.6 us → 38.6 us (-31%) | 55.8 us → 40.3 us (-28%) | 26.7 GB/s |
| 8 MB | 447 us → 314 us (-30%) | 446 us → 311 us (-30%) | 445 us → 324 us (-27%) | 26.7 GB/s |

## Block loop, forced

Each kernel in turn, against the baseline's single loop. The scalar column is
the control described above: same machine code both sides.

AVX512 leads AVX2 in every row it runs — by 2-7% on `Sum64` and 0-3% on
`Digest` — which is the "a little ahead of AVX2 at every size past the cutoff"
that CLAUDE.md records for it.

### Sum64

| Bytes | baseline | scalar | avx2 | avx512 | best rate |
|------:|---------:|------:|------:|------:|----------:|
| 256 | 18.4 ns | 17.9 ns (-2%) | 17.3 ns (-6%) | 16.1 ns (-12%) | 15.9 GB/s |
| 257 | 18.4 ns | 17.9 ns (-2%) | 18.2 ns (-1%) | 17.1 ns (-7%) | 15 GB/s |
| 384 | 24.7 ns | 24.6 ns (-0%) | 21.8 ns (-12%) | 20.6 ns (-17%) | 18.7 GB/s |
| 512 | 30.9 ns | 31.3 ns (+1%) | 25.8 ns (-17%) | 24.7 ns (-20%) | 20.7 GB/s |
| 1 KB | 58.8 ns | 58 ns (-1%) | 43.4 ns (-26%) | 41.9 ns (-29%) | 24.4 GB/s |
| 4 KB | 220 ns | 220 ns (+0%) | 147 ns (-33%) | 144 ns (-34%) | 28.4 GB/s |
| 16 KB | 863 ns | 868 ns (+1%) | 571 ns (-34%) | 562 ns (-35%) | 29.2 GB/s |
| 64 KB | 3.44 us | 3.45 us (+0%) | 2.36 us (-31%) | 2.26 us (-34%) | 29 GB/s |
| 1 MB | 55.7 us | 56 us (+1%) | 40.9 us (-27%) | 39.5 us (-29%) | 26.5 GB/s |
| 8 MB | 447 us | 447 us (-0%) | 325 us (-27%) | 315 us (-30%) | 26.6 GB/s |

### Digest

`writeBlocksVec` rather than `sum64Vec`, and it tracks it within a couple of
percent.

| Bytes | baseline | scalar | avx2 | avx512 | best rate |
|------:|---------:|------:|------:|------:|----------:|
| 256 | 21.3 ns | 21.8 ns (+2%) | 20.5 ns (-4%) | 20.5 ns (-4%) | 12.5 GB/s |
| 257 | 22.4 ns | 21.9 ns (-2%) | 21.7 ns (-3%) | 21.3 ns (-5%) | 12.1 GB/s |
| 384 | 27.9 ns | 28.4 ns (+2%) | 24.8 ns (-11%) | 24.7 ns (-11%) | 15.5 GB/s |
| 512 | 34.7 ns | 35.4 ns (+2%) | 29.1 ns (-16%) | 29 ns (-16%) | 17.6 GB/s |
| 1 KB | 61.7 ns | 62.2 ns (+1%) | 46.9 ns (-24%) | 46.8 ns (-24%) | 21.9 GB/s |
| 4 KB | 223 ns | 222 ns (-0%) | 155 ns (-30%) | 152 ns (-32%) | 26.9 GB/s |
| 16 KB | 870 ns | 870 ns (+0%) | 590 ns (-32%) | 580 ns (-33%) | 28.3 GB/s |
| 64 KB | 3.45 us | 3.45 us (+0%) | 2.42 us (-30%) | 2.34 us (-32%) | 28 GB/s |
| 1 MB | 55.8 us | 56 us (+1%) | 40.9 us (-27%) | 40.4 us (-28%) | 26 GB/s |
| 8 MB | 445 us | 445 us (-0%) | 324 us (-27%) | 324 us (-27%) | 25.9 GB/s |

## Streaming writes

4 KB fed to one `Digest` in chunks of the given size. Chunks of 8 bytes and
under are the short-write path in `Write`; from 16 up the write goes to
`memmove` as it always did, and the rows are flat through 64.

| Chunk | 4 KB in chunk-sized writes | rate |
|------:|----------------------------|-----:|
| 1 | 14.4 us → 12.3 us (-15%) | 0.332 GB/s |
| 4 | 3.94 us → 3.78 us (-4%) | 1.08 GB/s |
| 7 | 2.59 us → 2.39 us (-8%) | 1.71 GB/s |
| 8 | 2.17 us → 1.89 us (-13%) | 2.17 GB/s |
| 16 | 1.31 us → 1.32 us (+1%) | 3.11 GB/s |
| 24 | 1.15 us → 1.14 us (-1%) | 3.61 GB/s |
| 32 | 834 ns → 819 ns (-2%) | 5 GB/s |
| 64 | 491 ns → 499 ns (+2%) | 8.21 GB/s |
| 256 | 286 ns → 304 ns (+6%) | 13.5 GB/s |

The 256-byte chunk is the one row worth a second look: it is `vecCutoff`
exactly, so each of those sixteen writes now enters the vector path, pays its
pipeline fill and leaves again. At +6% it is well outside the ±2% the large
inputs settle at, and it is the only chunk size at which a streaming caller is
worse off than on the baseline.

## Pure Go

`-tags purego`: what every architecture without assembly runs, and what amd64
and arm64 run under `purego` or `appengine`.

| Bytes | Sum64 | Sum64String | Digest | rate |
|------:|--------|--------|--------|-----:|
| 0 | 2.12 ns → 2.54 ns (+20%) | 1.92 ns → 2.54 ns (+32%) | 4.73 ns → 4.11 ns (-13%) | — |
| 1 | 2.57 ns → 2.77 ns (+8%) | 2.36 ns → 2.75 ns (+17%) | 6.53 ns → 5.46 ns (-16%) | 0.361 GB/s |
| 4 | 2.14 ns → 2.36 ns (+11%) | 2.13 ns → 2.35 ns (+10%) | 6.35 ns → 5.79 ns (-9%) | 1.69 GB/s |
| 8 | 2.57 ns → 2.56 ns (-0%) | 2.56 ns → 2.56 ns (-0%) | 7.44 ns → 7.08 ns (-5%) | 3.13 GB/s |
| 16 | 3.5 ns → 2.99 ns (-15%) | 3.64 ns → 3.07 ns (-16%) | 9.26 ns → 7.6 ns (-18%) | 5.35 GB/s |
| 31 | 7.91 ns → 6.47 ns (-18%) | 7.99 ns → 6.4 ns (-20%) | 18.1 ns → 17.6 ns (-3%) | 4.79 GB/s |
| 32 | 6.87 ns → 6.83 ns (-1%) | 7 ns → 6.73 ns (-4%) | 9.59 ns → 9.63 ns (+0%) | 4.68 GB/s |
| 33 | 7.61 ns → 6.97 ns (-8%) | 8.03 ns → 6.95 ns (-13%) | 10.7 ns → 10.3 ns (-4%) | 4.74 GB/s |
| 64 | 10.2 ns → 9.3 ns (-9%) | 10.6 ns → 9.23 ns (-13%) | 12.8 ns → 12.2 ns (-5%) | 6.88 GB/s |
| 96 | 12.4 ns → 12.2 ns (-2%) | 12.5 ns → 13 ns (+4%) | 14.5 ns → 14.4 ns (-0%) | 7.89 GB/s |
| 128 | 14 ns → 15.1 ns (+8%) | 14.3 ns → 15.2 ns (+7%) | 16.7 ns → 17.5 ns (+5%) | 8.48 GB/s |
| 192 | 19.5 ns → 20.7 ns (+6%) | 19.6 ns → 20.6 ns (+5%) | 22.1 ns → 22.5 ns (+2%) | 9.27 GB/s |
| 255 | 29.2 ns → 28.8 ns (-2%) | 29.2 ns → 28.8 ns (-2%) | 31.5 ns → 31 ns (-1%) | 8.86 GB/s |
| 256 | 25.2 ns → 26.4 ns (+5%) | 25.6 ns → 26 ns (+2%) | 27.8 ns → 28.1 ns (+1%) | 9.68 GB/s |
| 257 | 25.6 ns → 25.8 ns (+1%) | 25.9 ns → 25.8 ns (-1%) | 27.5 ns → 28.9 ns (+5%) | 9.96 GB/s |
| 384 | 34 ns → 37.8 ns (+11%) | 34.2 ns → 36.9 ns (+8%) | 36.7 ns → 38.1 ns (+4%) | 10.2 GB/s |
| 512 | 43.2 ns → 49.3 ns (+14%) | 43.3 ns → 47.8 ns (+10%) | 46 ns → 49.2 ns (+7%) | 10.4 GB/s |
| 1 KB | 83.6 ns → 95.3 ns (+14%) | 83.9 ns → 90.5 ns (+8%) | 87.4 ns → 91.3 ns (+4%) | 10.7 GB/s |
| 4 KB | 318 ns → 364 ns (+15%) | 316 ns → 369 ns (+17%) | 320 ns → 344 ns (+8%) | 11.3 GB/s |
| 16 KB | 1.25 us → 1.46 us (+17%) | 1.24 us → 1.46 us (+18%) | 1.24 us → 1.36 us (+10%) | 11.2 GB/s |
| 64 KB | 5.01 us → 5.52 us (+10%) | 5.01 us → 5.53 us (+10%) | 4.99 us → 5.43 us (+9%) | 11.9 GB/s |
| 1 MB | 80.5 us → 91.3 us (+13%) | 80.5 us → 91.7 us (+14%) | 80.5 us → 88.5 us (+10%) | 11.5 GB/s |
| 8 MB | 641 us → 728 us (+14%) | 643 us → 731 us (+14%) | 643 us → 708 us (+10%) | 11.5 GB/s |

The short inputs gained from the rewritten tail. Everything from 384 bytes up
lost, and the loss is the carried round in `xxhash_other.go`, not the tail.
Isolated against `c581032`, the commit immediately before it, with nothing else
in between:

```
                           │   pre.txt   │              head.txt               │
                           │   sec/op    │   sec/op     vs base                │
ReportDispatch/4096/Sum64    313.8n ± 0%   365.1n ± 1%  +16.33% (p=0.000 n=20)
ReportDispatch/16384/Sum64   1.238µ ± 0%   1.444µ ± 1%  +16.60% (p=0.000 n=20)
ReportDispatch/65536/Sum64   4.978µ ± 1%   5.497µ ± 1%  +10.43% (p=0.000 n=20)
geomean                      1.246µ        1.426µ       +14.42%
```

The rearrangement shortens the loop-carried chain, which is a win on a machine
with a fused integer multiply-add and is why it is in `xxhash_arm64.s`. x86 has
no such instruction, and the pure-Go loop there is not chain-bound in the first
place: eight 64-bit multiplies per block against one multiply port floor it at
8 cycles where the chain is 5. So the shorter chain buys nothing, and what is
left is the cost — `carryRound` needs `input*prime2` live in a register of its
own per accumulator, and go1.26.5 keeps all four products live and groups the
four adds at the end of the body, where the plain round consumed each product
immediately and reused one scratch register. "The pure-Go block loops" in
CLAUDE.md spells this out.

This affects amd64 and arm64 only under `purego` or `appengine`; on those two
the assembly is what runs. What it does to 386, riscv64, ppc64 or s390x, which
have no assembly here and are what this file exists for, has not been measured.

### Pure Go, streaming writes

| Chunk | 4 KB in chunk-sized writes | rate |
|------:|----------------------------|-----:|
| 1 | 14.5 us → 12.7 us (-12%) | 0.323 GB/s |
| 4 | 3.96 us → 3.78 us (-5%) | 1.08 GB/s |
| 7 | 2.57 us → 2.39 us (-7%) | 1.71 GB/s |
| 8 | 2.16 us → 1.88 us (-13%) | 2.18 GB/s |
| 16 | 1.3 us → 1.36 us (+5%) | 3 GB/s |
| 24 | 1.15 us → 1.17 us (+2%) | 3.51 GB/s |
| 32 | 805 ns → 782 ns (-3%) | 5.23 GB/s |
| 64 | 520 ns → 528 ns (+2%) | 7.76 GB/s |
| 256 | 370 ns → 389 ns (+5%) | 10.5 GB/s |

The short-write path is in `xxhash.go` and is shared, so the gains at 1 to 8
byte chunks are the same ones the assembly build shows. At these chunk sizes
`Write` is the cost and the block loop barely runs, which is why the pure-Go
regression does not show up until 256-byte chunks.

# arm64

A second machine, and a different story: on this core the tree got slower
before it got faster, and both moves are in the table.

|           |                                                                     |
|-----------|---------------------------------------------------------------------|
| CPU       | Neoverse N2 (Azure Cobalt 100, 2 vCPU, 3.39 GHz measured)           |
| Go        | go1.26.5 linux/arm64                                                |
| Baseline  | `998dce2` Add initial support for custom seeds                      |
| Previous  | `0678bf0` Record that the pure-Go carried round is a loss on x86    |
| This tree | `fbf899f` Put the arm64 round's rotate after its multiply-add       |

## Method

As above — both trees built up front and run alternately, pinned to core 1 —
except that there are three of them, rotated so each takes every position, ten
rounds. Every figure is the median. The clock came out of `perf`: 8.15e9 cycles
over 2.405 s.

The noise floor here is the sizes under 32 bytes, which never reach a block
loop. They read within 0.1% between the last two trees, and the whole table
came in at ±0-2%.

## Dispatched

`998dce2` → `0678bf0` → this tree, with the change from `998dce2` to this tree
in parentheses. The rate column is this tree's `Sum64`.

| Bytes | Sum64 | Sum64String | Digest | rate |
|------:|--------|--------|--------|-----:|
| 0 | 2.75 ns → 2.75 ns → 2.68 ns (-3%) | 2.69 ns → 2.69 ns → 2.66 ns (-1%) | 6.65 ns → 6.49 ns → 6.49 ns (-2%) | — |
| 1 | 3.13 ns → 3.13 ns → 3.07 ns (-2%) | 3.05 ns → 3.05 ns → 2.96 ns (-3%) | 8.34 ns → 7.54 ns → 7.53 ns (-10%) | 0.326 GB/s |
| 4 | 3.12 ns → 3.12 ns → 3.1 ns (-1%) | 3.04 ns → 3.04 ns → 3 ns (-1%) | 7.83 ns → 7.97 ns → 7.97 ns (+2%) | 1.29 GB/s |
| 8 | 3.34 ns → 3.34 ns → 3.34 ns (-0%) | 3.26 ns → 3.26 ns → 3.26 ns (-0%) | 8.28 ns → 7.56 ns → 7.57 ns (-9%) | 2.4 GB/s |
| 16 | 4.11 ns → 4.11 ns → 4.11 ns (-0%) | 3.99 ns → 3.99 ns → 3.99 ns (-0%) | 10.1 ns → 8.85 ns → 8.85 ns (-12%) | 3.9 GB/s |
| 31 | 7.81 ns → 7.81 ns → 7.81 ns (-0%) | 7.73 ns → 7.73 ns → 7.73 ns (-0%) | 19.1 ns → 15.1 ns → 15.1 ns (-21%) | 3.97 GB/s |
| 32 | 7.47 ns → 7.48 ns → 7.4 ns (-1%) | 7.43 ns → 7.42 ns → 7.35 ns (-1%) | 13.7 ns → 13.8 ns → 13.6 ns (-1%) | 4.33 GB/s |
| 33 | 8.12 ns → 8.07 ns → 8.14 ns (+0%) | 7.98 ns → 8.01 ns → 8 ns (+0%) | 15.2 ns → 15 ns → 15 ns (-2%) | 4.05 GB/s |
| 64 | 9 ns → 8.88 ns → 9.02 ns (+0%) | 8.88 ns → 8.75 ns → 8.94 ns (+1%) | 15.1 ns → 15 ns → 15.1 ns (+0%) | 7.09 GB/s |
| 96 | 10.9 ns → 10.7 ns → 10.8 ns (-1%) | 10.6 ns → 10.5 ns → 10.7 ns (+1%) | 16.4 ns → 16.4 ns → 16.6 ns (+2%) | 8.92 GB/s |
| 128 | 12.3 ns → 12.4 ns → 12.3 ns (-0%) | 12.4 ns → 12.3 ns → 12.3 ns (-1%) | 18 ns → 18.1 ns → 18 ns (+0%) | 10.4 GB/s |
| 192 | 15.4 ns → 15.4 ns → 15.2 ns (-2%) | 15.2 ns → 15.2 ns → 15 ns (-1%) | 20.6 ns → 20.8 ns → 20.7 ns (+0%) | 12.6 GB/s |
| 255 | 25.4 ns → 25.3 ns → 25.4 ns (-0%) | 25.4 ns → 25.3 ns → 25.1 ns (-1%) | 32.2 ns → 31.7 ns → 31.7 ns (-2%) | 10 GB/s |
| 256 | 18.6 ns → 18.4 ns → 18.5 ns (-1%) | 18.5 ns → 18.3 ns → 18.4 ns (-1%) | 23.2 ns → 23.5 ns → 23.1 ns (-1%) | 13.9 GB/s |
| 257 | 19.8 ns → 19.5 ns → 19.6 ns (-1%) | 19.9 ns → 19.6 ns → 19.6 ns (-1%) | 25 ns → 25.1 ns → 24.6 ns (-2%) | 13.1 GB/s |
| 384 | 24.5 ns → 24.3 ns → 24.1 ns (-2%) | 24.5 ns → 24.2 ns → 24.1 ns (-2%) | 28.5 ns → 29.2 ns → 27.9 ns (-2%) | 15.9 GB/s |
| 512 | 30.1 ns → 30.1 ns → 29.1 ns (-3%) | 30 ns → 30.2 ns → 29.1 ns (-3%) | 34 ns → 35 ns → 32.7 ns (-4%) | 17.6 GB/s |
| 1 KB | 51.1 ns → 53.9 ns → 48.9 ns (-4%) | 51.5 ns → 53.9 ns → 49 ns (-5%) | 55.6 ns → 58.7 ns → 52.4 ns (-6%) | 20.9 GB/s |
| 4 KB | 177 ns → 196 ns → 164 ns (-8%) | 180 ns → 196 ns → 164 ns (-9%) | 186 ns → 200 ns → 168 ns (-9%) | 25 GB/s |
| 16 KB | 679 ns → 764 ns → 625 ns (-8%) | 697 ns → 764 ns → 623 ns (-11%) | 710 ns → 772 ns → 630 ns (-11%) | 26.2 GB/s |
| 64 KB | 2.67 us → 3.04 us → 2.48 us (-7%) | 2.75 us → 3.04 us → 2.48 us (-10%) | 2.79 us → 3.04 us → 2.48 us (-11%) | 26.4 GB/s |
| 1 MB | 43.4 us → 49.7 us → 42 us (-3%) | 43.8 us → 49.6 us → 42.4 us (-3%) | 44.9 us → 49.9 us → 42.4 us (-6%) | 24.9 GB/s |
| 8 MB | 322 us → 390 us → 320 us (-0%) | 319 us → 389 us → 321 us (+0%) | 340 us → 389 us → 320 us (-6%) | 26.2 GB/s |

## What moved

**The middle column is the point.** `c581032`, which rearranged the round into a
rotate followed by a multiply-add, is worth +59% at 4 KB on an Apple M2 and
**cost 10% at 4 KB here** — `998dce2`'s straightforward round ran the block loop
at 4.7 cycles a block on this core and the rearranged one at 5.0. Nothing was
wrong with the reasoning; it was an Apple measurement applied to a core that
forwards a multiply-add's addend in one cycle, which the M2 does not, and that
makes the straightforward round chain-equal there. The arm64 section of
CLAUDE.md has the instruction timings.

This tree puts the rotate on the other side of the multiply-add, which is the
same three instructions and the same chain on both machines, and takes two
blocks an iteration. Against the middle column alone, `Sum64`:

| Bytes | `0678bf0` | this tree | | cycles/block |
|------:|----------:|----------:|--------:|----:|
| 512 | 30.1 ns | 29.1 ns | -3.4% | 6.39 → 6.17 |
| 1 KB | 53.9 ns | 48.9 ns | -9.2% | 5.71 → 5.18 |
| 4 KB | 196 ns | 164 ns | -16.2% | 5.18 → 4.34 |
| 16 KB | 764 ns | 625 ns | -18.2% | 5.06 → 4.14 |
| 64 KB | 3.04 us | 2.48 us | -18.3% | 5.03 → 4.11 |
| 1 MB | 49.7 us | 42.0 us | -15.4% | 5.14 → 4.35 |
| 8 MB | 390 us | 320 us | -17.7% | 5.04 → 4.14 |

4.11 cycles a block is the floor: four `MADD`s at one per cycle, and this core
has no way to issue them faster. The cycles/block column includes the peel, the
merge and the tail, which is why the short rows are so far above it — at 256
bytes those are most of the call.

**Under 512 bytes, against `998dce2`, nothing is clearly above the floor.**
`Sum64` reads -3% to +1% and most of that is where the function lands rather
than what it does. The one real movement down there is `Digest` at 1 to 31
bytes, -9% to -21%, which is the short-write path in `Write` from `fc098a7` and
is shared with amd64.

**8 MB reads -0% against `998dce2` and -18% against `0678bf0`.** 8 MB is past
this VM's 2 MB L2 into a 128 MB shared L3, and at 26 GB/s the block loop is no
longer the only thing being measured; the 64 KB row is the one to read for the
loop itself.
