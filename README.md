# xxhash

[![Go Reference](https://pkg.go.dev/badge/github.com/cespare/xxhash/v2.svg)](https://pkg.go.dev/github.com/cespare/xxhash/v2)
[![Test](https://github.com/cespare/xxhash/actions/workflows/test.yml/badge.svg)](https://github.com/cespare/xxhash/actions/workflows/test.yml)

xxhash is a Go implementation of the 64-bit [xxHash] algorithm, XXH64. This is a
high-quality hashing algorithm that is much faster than anything in the Go
standard library.

This package provides a straightforward API:

```
func Sum64(b []byte) uint64
func Sum64String(s string) uint64
type Digest struct{ ... }
    func New() *Digest
```

The `Digest` type implements hash.Hash64. Its key methods are:

```
func (*Digest) Write([]byte) (int, error)
func (*Digest) WriteString(string) (int, error)
func (*Digest) Sum64() uint64
```

The package is written with optimized pure Go and also contains even faster
assembly implementations for amd64 and arm64. If desired, the `purego` build tag
opts into using the Go code even on those architectures.

On amd64, long inputs additionally use a vectorized block loop, selected at
startup from the CPU's features: AVX2, or AVX512 (F+DQ+VL) where available.
XXH64's block loop is a serial chain of 64-bit multiplies, so the vector units
don't run it directly; they precompute the half of each round that depends only
on the input, which takes the scalar half off the CPU's single 64-bit multiply
port. Short inputs are unaffected and still take the scalar path.

[xxHash]: https://xxhash.com/

## Compatibility

This package is in a module and the latest code is in version 2 of the module.
You need a version of Go with at least "minimal module compatibility" to use
github.com/cespare/xxhash/v2:

* 1.9.7+ for Go 1.9
* 1.10.3+ for Go 1.10
* Go 1.11 or later

I recommend using the latest release of Go.

## Benchmarks

Here are some quick benchmarks comparing the pure-Go and assembly
implementations of Sum64.

| input size | purego    | asm       |
| ---------- | --------- | --------- |
| 4 B        |  2.3 GB/s |  2.3 GB/s |
| 16 B       |  6.2 GB/s |  7.7 GB/s |
| 100 B      | 11.0 GB/s | 12.0 GB/s |
| 4 KB       | 18.2 GB/s | 26.9 GB/s |
| 10 MB      | 17.8 GB/s | 26.7 GB/s |

These numbers were generated on Ubuntu 26.04 with an Intel Core Ultra 9 185H CPU
(which has AVX2 but not AVX512) using the following commands under Go 1.26.5:

```
benchstat <(go test -tags purego -benchtime 500ms -count 15 -bench 'Sum64$')
benchstat <(go test -benchtime 500ms -count 15 -bench 'Sum64$')
```

Earlier numbers, measured on an Intel Xeon Platinum 8252C under Go 1.19.2 before
the vectorized block loop, were 11.7 GB/s (purego) and 16.7 GB/s (asm) at 4 KB.
Both machines and both Go versions differ, so those are not directly comparable;
on this machine the same benchmark went from 19.0 to 26.9 GB/s.

The same benchmarks on arm64, using a Neoverse N2 (Azure Cobalt 100) under Go
1.26.5:

| input size | purego    | asm       |
| ---------- | --------- | --------- |
| 4 B        |  1.2 GB/s |  1.4 GB/s |
| 16 B       |  3.5 GB/s |  4.3 GB/s |
| 100 B      |  6.7 GB/s |  8.9 GB/s |
| 4 KB       | 16.4 GB/s | 25.1 GB/s |
| 10 MB      | 16.6 GB/s | 26.1 GB/s |

## Projects using this package

- [InfluxDB](https://github.com/influxdata/influxdb)
- [Prometheus](https://github.com/prometheus/prometheus)
- [VictoriaMetrics](https://github.com/VictoriaMetrics/VictoriaMetrics)
- [FreeCache](https://github.com/coocood/freecache)
- [FastCache](https://github.com/VictoriaMetrics/fastcache)
- [Ristretto](https://github.com/dgraph-io/ristretto)
- [Badger](https://github.com/dgraph-io/badger)
