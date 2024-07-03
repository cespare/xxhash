package xxhash

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"
)

func Test(t *testing.T) {
	for {
		var suffix string
		if useAvx512 {
			suffix = "-avx512"
		}

		t.Run("All"+suffix, testAll)
		t.Run("Reset"+suffix, testReset)
		t.Run("ResetWithSeed"+suffix, testResetWithSeed)
		t.Run("BinaryMarshaling"+suffix, testBinaryMarshaling)

		if useAvx512 {
			useAvx512 = false
			defer func() { useAvx512 = true }()
			continue
		}
		return
	}
}

func testAll(t *testing.T) {
	// Exactly 63 characters, which exercises all code paths.
	const s63 = "Call me Ishmael. Some years ago--never mind how long precisely-"
	for _, tt := range []struct {
		input string
		seed  uint64
		want  uint64
	}{
		{"", 0, 0xef46db3751d8e999},
		{"a", 0, 0xd24ec4f1a98c6e5b},
		{"as", 0, 0x1c330fb2d66be179},
		{"asd", 0, 0x631c37ce72a97393},
		{"asdf", 0, 0x415872f599cea71e},
		{s63, 0, 0x02a2e85470d6fd96},

		{"", 123, 0xe0db84de91f3e198},
		{"asdf", math.MaxUint64, 0x9a2fd8473be539b6},
		{s63, 54321, 0x1736d186daf5d1cd},
	} {
		lastChunkSize := len(tt.input)
		if lastChunkSize == 0 {
			lastChunkSize = 1
		}
		var name string
		if tt.input == "" {
			name = "input=empty"
		} else if len(tt.input) > 10 {
			name = fmt.Sprintf("input=len-%d", len(tt.input))
		} else {
			name = fmt.Sprintf("input=%q", tt.input)
		}
		if tt.seed != 0 {
			name += fmt.Sprintf(",seed=%d", tt.seed)
		}
		for chunkSize := 1; chunkSize <= lastChunkSize; chunkSize++ {
			name := fmt.Sprintf("%s,chunkSize=%d", name, chunkSize)
			t.Run(name, func(t *testing.T) {
				testDigest(t, tt.input, tt.seed, chunkSize, tt.want)
			})
		}
		if tt.seed == 0 {
			t.Run(name, func(t *testing.T) { testSum(t, tt.input, tt.want) })
		}
	}
}

func testDigest(t *testing.T, input string, seed uint64, chunkSize int, want uint64) {
	d := NewWithSeed(seed)
	ds := NewWithSeed(seed) // uses WriteString
	for i := 0; i < len(input); i += chunkSize {
		chunk := input[i:]
		if len(chunk) > chunkSize {
			chunk = chunk[:chunkSize]
		}
		n, err := d.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("Digest.Write: got (%d, %v); want (%d, nil)", n, err, len(chunk))
		}
		n, err = ds.WriteString(chunk)
		if err != nil || n != len(chunk) {
			t.Fatalf("Digest.WriteString: got (%d, %v); want (%d, nil)", n, err, len(chunk))
		}
	}
	if got := d.Sum64(); got != want {
		t.Fatalf("Digest.Sum64: got 0x%x; want 0x%x", got, want)
	}
	if got := ds.Sum64(); got != want {
		t.Fatalf("Digest.Sum64 (WriteString): got 0x%x; want 0x%x", got, want)
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], want)
	if got := d.Sum(nil); !bytes.Equal(got, b[:]) {
		t.Fatalf("Sum: got %v; want %v", got, b[:])
	}
}

func testSum(t *testing.T, input string, want uint64) {
	if got := Sum64([]byte(input)); got != want {
		t.Fatalf("Sum64: got 0x%x; want 0x%x", got, want)
	}
	if got := Sum64String(input); got != want {
		t.Fatalf("Sum64String: got 0x%x; want 0x%x", got, want)
	}
}

func testReset(t *testing.T) {
	parts := []string{"The quic", "k br", "o", "wn fox jumps", " ov", "er the lazy ", "dog."}
	d := New()
	for _, part := range parts {
		d.Write([]byte(part))
	}
	h0 := d.Sum64()

	d.Reset()
	d.Write([]byte(strings.Join(parts, "")))
	h1 := d.Sum64()

	if h0 != h1 {
		t.Errorf("0x%x != 0x%x", h0, h1)
	}
}

func testResetWithSeed(t *testing.T) {
	parts := []string{"The quic", "k br", "o", "wn fox jumps", " ov", "er the lazy ", "dog."}
	d := NewWithSeed(123)
	for _, part := range parts {
		d.Write([]byte(part))
	}
	h0 := d.Sum64()

	d.ResetWithSeed(123)
	d.Write([]byte(strings.Join(parts, "")))
	h1 := d.Sum64()

	if h0 != h1 {
		t.Errorf("0x%x != 0x%x", h0, h1)
	}
}

func testBinaryMarshaling(t *testing.T) {
	d := New()
	d.WriteString("abc")
	b, err := d.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	d = New()
	d.WriteString("junk")
	if err := d.UnmarshalBinary(b); err != nil {
		t.Fatal(err)
	}
	d.WriteString("def")
	if got, want := d.Sum64(), Sum64String("abcdef"); got != want {
		t.Fatalf("after MarshalBinary+UnmarshalBinary, got 0x%x; want 0x%x", got, want)
	}

	d0 := New()
	d1 := New()
	for i := 0; i < 64; i++ {
		b, err := d0.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		d0 = new(Digest)
		if err := d0.UnmarshalBinary(b); err != nil {
			t.Fatal(err)
		}
		if got, want := d0.Sum64(), d1.Sum64(); got != want {
			t.Fatalf("after %d Writes, unmarshaled Digest gave sum 0x%x; want 0x%x", i, got, want)
		}

		d0.Write([]byte{'a'})
		d1.Write([]byte{'a'})
	}
}

var sink uint64

func TestAllocs(t *testing.T) {
	const shortStr = "abcdefghijklmnop"
	// Sum64([]byte(shortString)) shouldn't allocate because the
	// intermediate []byte ought not to escape.
	// (See https://github.com/cespare/xxhash/pull/2.)
	t.Run("Sum64", func(t *testing.T) {
		runAllocs(t, func() {
			sink = Sum64([]byte(shortStr))
		})
	})
	// Creating and using a Digest shouldn't allocate because its methods
	// shouldn't make it escape. (A previous version of New returned a
	// hash.Hash64 which forces an allocation.)
	t.Run("Digest", func(t *testing.T) {
		b := []byte("asdf")
		runAllocs(t, func() {
			d := New()
			d.Write(b)
			sink = d.Sum64()
		})
	})
}

func runAllocs(t *testing.T, fn func()) {
	t.Helper()
	if allocs := int(testing.AllocsPerRun(10, fn)); allocs > 0 {
		t.Fatalf("got %d allocation(s) (want zero)", allocs)
	}
}
