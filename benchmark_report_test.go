package xxhash

import (
	"fmt"
	"runtime"
	"testing"
)

// The harness behind BENCHMARK.md, which compares this tree against the commit
// before the optimization work started.
//
// It differs from bench_test.go in two ways that matter only when two builds
// are being compared rather than one being profiled:
//
// Every size is sliced out of one buffer. Allocating per size, as the
// benchmarks in bench_test.go do, makes the data pointer's alignment a function
// of the size and of the whole allocation sequence, which differs between two
// builds; it then shows up as swings on inputs whose code neither tree touched.
//
// Everything but BenchmarkReportKernel uses the public API alone, so the file
// drops into an older tree unchanged. BenchmarkReportKernel needs
// forEachImplBench, which older trees don't have; BENCHMARK.md carries the stub
// for that.
var (
	reportBuf = newReportBuf()
	reportStr = string(reportBuf)
)

func newReportBuf() []byte {
	b := make([]byte, 8<<20)
	// Fill it. A fresh anonymous mapping that has only ever been read from is
	// backed by one shared zero page, which would fit the megabyte sizes in L1
	// and turn those rows into a measurement of nothing.
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// Sizes either side of every threshold in the package: the 32-byte block, the
// 256-byte vector cutoff, and the 128-byte group the vector loops consume at a
// time. The last three sizes cross L2 and L3 on the machines this has been run
// on, where the block loop stops being what is measured.
var reportSizes = []int{
	0, 1, 4, 8, 16, 31, 32, 33, 64, 96, 128, 192, 224, 255, 256, 257, 384,
	512, 1024, 4096, 16384, 65536, 1 << 20, 8 << 20,
}

func reportVariants(b *testing.B, n int) {
	b.Run("Sum64", func(b *testing.B) {
		in := reportBuf[:n]
		var acc uint64
		b.SetBytes(int64(n))
		for i := 0; i < b.N; i++ {
			acc = Sum64(in)
		}
		runtime.KeepAlive(acc)
	})
	b.Run("Sum64String", func(b *testing.B) {
		in := reportStr[:n]
		var acc uint64
		b.SetBytes(int64(n))
		for i := 0; i < b.N; i++ {
			acc = Sum64String(in)
		}
		runtime.KeepAlive(acc)
	})
	b.Run("Digest", func(b *testing.B) {
		in := reportBuf[:n]
		var acc uint64
		b.SetBytes(int64(n))
		for i := 0; i < b.N; i++ {
			d := New()
			d.Write(in)
			acc = d.Sum64()
		}
		runtime.KeepAlive(acc)
	})
}

// BenchmarkReportDispatch measures what a caller actually gets, with the
// package picking its own block loop. This is the one to run in both trees.
func BenchmarkReportDispatch(b *testing.B) {
	for _, n := range reportSizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) { reportVariants(b, n) })
	}
}

// reportCutoff is the lower of sumCutoff and writeCutoff from xxhash_amd64.s.
// Below it the assembly takes the scalar block loop whatever the feature flags
// say, so a forced vector kernel there measures the scalar one and says nothing
// about the vector code. Between the two cutoffs that is still true of the
// Digest rows alone, which is why they read flat from here to writeCutoff.
//
// It is written out here rather than exported from the assembly on purpose: the
// two trees being compared don't have to agree on either cutoff, and a
// comparison has to hold the length constant across both.
const reportCutoff = 224

// BenchmarkReportKernel forces each block loop this CPU can run, so that a
// change to one of them can be read without the dispatch thresholds in the way.
func BenchmarkReportKernel(b *testing.B) {
	for _, n := range reportSizes {
		if n < reportCutoff {
			continue
		}
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			forEachImplBench(b, func(b *testing.B) { reportVariants(b, n) })
		})
	}
}

// BenchmarkReportChunks feeds a fixed amount of data to one Digest in pieces.
// Writes that don't fill out the 32-byte block are the common case for a
// streaming caller and take their own path through Write, which the single
// large write in reportVariants never reaches.
func BenchmarkReportChunks(b *testing.B) {
	const n = 4096
	in := reportBuf[:n]
	for _, chunk := range []int{1, 4, 7, 8, 16, 24, 32, 64, 256} {
		b.Run(fmt.Sprint(chunk), func(b *testing.B) {
			var acc uint64
			b.SetBytes(n)
			for i := 0; i < b.N; i++ {
				d := New()
				for j := 0; j < n; j += chunk {
					end := j + chunk
					if end > n {
						end = n
					}
					d.Write(in[j:end])
				}
				acc = d.Sum64()
			}
			runtime.KeepAlive(acc)
		})
	}
}
