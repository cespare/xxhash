#!/bin/bash
set -eu -o pipefail

# Small convenience script for running the tests with various combinations of
# arch/tags. Whichever of amd64/arm64 is the host runs natively and the other
# goes through qemu-user, either registered with binfmt_misc or on $PATH as
# qemu-<arch> or qemu-<arch>-static.
#
# On amd64 the tests run every block loop the CPU can execute (scalar, AVX2,
# AVX512); see forEachImpl in xxhash_impl_amd64_test.go. Under qemu-user only
# scalar and AVX2 are reachable -- QEMU's TCG implements no AVX512, and reports
# neither the CPUID bits nor the ZMM state in XCR0 even with -cpu max -- so the
# AVX512 loop has to be run on hardware that has it.

# testarch runs the package tests for a GOARCH, going through qemu-user
# explicitly if binfmt_misc isn't set up for it.
testarch() {
	local goarch=$1 qemuarch=$2
	shift 2
	local qemu
	qemu=$(command -v "qemu-$qemuarch" || command -v "qemu-$qemuarch-static" || true)
	echo "==> GOARCH=$goarch ${*:-} ${qemu:+(via ${qemu##*/})}"
	if [[ -n $qemu ]]; then
		GOARCH=$goarch go test -exec "$qemu" "$@" .
	else
		GOARCH=$goarch go test "$@" .
	fi
}

host=$(go env GOHOSTARCH)

echo "==> host ($host)"
go test ./...
go test -tags purego ./...
go test -tags appengine ./...

# The assembly for the arch that isn't the host, plus the pure-Go loops on a
# 64-bit and a 32-bit target that have no assembly at all.
case $host in
amd64) testarch arm64 aarch64; testarch arm64 aarch64 -tags purego ;;
arm64) testarch amd64 x86_64; testarch amd64 x86_64 -tags purego ;;
esac
testarch 386 i386
testarch arm arm
testarch riscv64 riscv64
