# xxhash

[![GoDoc](https://godoc.org/github.com/cespare/mph?status.svg)](https://godoc.org/github.com/cespare/xxhash)

xxhash is a Go implementation of the 64-bit
[xxHash](http://cyan4973.github.io/xxHash/) algorithm, XXH64. This is a
high-quality hashing algorithm that is much faster than anything in the Go
standard library.

On amd64 there is an even faster assembly implementation that runs at over 10
GB/s on my laptop.
