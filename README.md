# xxhash

[![GoDoc](https://godoc.org/github.com/cespare/xxhash?status.svg)](https://godoc.org/github.com/cespare/xxhash)
[![Build Status](https://travis-ci.org/cespare/xxhash.svg?branch=master)](https://travis-ci.org/cespare/xxhash)

xxhash is a Go implementation of the 64-bit
[xxHash](http://cyan4973.github.io/xxHash/) algorithm, XXH64. This is a
high-quality hashing algorithm that is much faster than anything in the Go
standard library.

The API is fairly small:

```
$ go doc github.com/cespare/xxhash
package xxhash // import "github.com/cespare/xxhash"

Package xxhash implements the 64-bit variant of xxHash (XXH64) as described
at http://cyan4973.github.io/xxHash/.

func Sum64(b []byte) uint64
func Sum64String(s string) uint64
type Digest struct{ ... }
    func New() *Digest
```

The type Digest implements hash.Hash64. Its key methods are:

```
func (*Digest) Write([]byte) (int, error)
func (*Digest) WriteString(string) (int, error)
func (*Digest) Sum64() uint64
```

This implementation provides a fast pure-Go implementation and an even faster
assembly implementation for amd64.

## Benchmarks

Here are some quick benchmarks comparing the pure-Go and assembly
implementations of Sum64 against another popular Go XXH64 implementation,
[github.com/OneOfOne/xxhash](https://github.com/OneOfOne/xxhash):

| input size | OneOfOne | cespare (purego) | cespare |
| --- | --- | --- | --- |
| 5 B   |  416 MB/s | 720 MB/s |  872 MB/s  |
| 100 B | 3980 MB/s | 5013 MB/s | 5252 MB/s  |
| 4 KB  | 12727 MB/s | 12999 MB/s | 13026 MB/s |
| 10 MB | 9879 MB/s | 10775 MB/s | 10913 MB/s  |

These numbers were generated with:

```
$ go test -benchtime 10s -bench '/OneOfOne,'
$ go test -tags purego -benchtime 10s -bench '/xxhash,'
$ go test -benchtime 10s -bench '/xxhash,'
```

## Projects using this package

- [InfluxDB](https://github.com/influxdata/influxdb)
- [Prometheus](https://github.com/prometheus/prometheus)
- [FreeCache](https://github.com/coocood/freecache)
