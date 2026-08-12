#!/bin/bash
set -eu -o pipefail

# Small convenience script for running the tests with various combinations of
# arch/tags. This assumes we're running on amd64 and have qemu available, either
# registered with binfmt_misc or on $PATH as qemu-<arch> or qemu-<arch>-static.
#
# On amd64 the tests run every block loop the CPU can execute (scalar, AVX2,
# AVX512); see forEachImpl in xxhash_impl_amd64_test.go.

# testarch runs the package tests for a GOARCH, going through qemu-user
# explicitly if binfmt_misc isn't set up for it.
testarch() {
	local goarch=$1 qemuarch=$2
	shift 2
	local qemu
	qemu=$(command -v "qemu-$qemuarch" || command -v "qemu-$qemuarch-static" || true)
	if [[ -n $qemu ]]; then
		GOARCH=$goarch go test -exec "$qemu" "$@" .
	else
		GOARCH=$goarch go test "$@" .
	fi
}

go test ./...
go test -tags purego ./...
go test -tags appengine ./...

testarch arm64 aarch64
testarch arm64 aarch64 -tags purego
testarch 386 i386
