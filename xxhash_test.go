package xxhash

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"hash/fnv"
	"strings"
	"testing"

	OneOfOne "github.com/OneOfOne/xxhash"
	"github.com/spaolacci/murmur3"
)

var result uint64

func BenchmarkStringHash(b *testing.B) {
	const s = "abcdefghijklmnop"
	var r uint64
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		r = Sum64([]byte(s))
	}
	result = r
}

func TestSum(t *testing.T) {
	for i, tt := range []struct {
		input string
		want  uint64
	}{
		{"", 0xef46db3751d8e999},
		{"a", 0xd24ec4f1a98c6e5b},
		{"as", 0x1c330fb2d66be179},
		{"asd", 0x631c37ce72a97393},
		{"asdf", 0x415872f599cea71e},
		{
			// Exactly 63 characters, which exercises all code paths.
			"Call me Ishmael. Some years ago--never mind how long precisely-",
			0x02a2e85470d6fd96,
		},
	} {
		for chunkSize := 1; chunkSize <= len(tt.input); chunkSize++ {
			x := New()
			y := New() // same as x but uses WriteString
			for j := 0; j < len(tt.input); j += chunkSize {
				end := j + chunkSize
				if end > len(tt.input) {
					end = len(tt.input)
				}
				chunk := []byte(tt.input[j:end])
				n, err := x.Write(chunk)
				if err != nil || n != len(chunk) {
					t.Fatalf("[i=%d,chunkSize=%d] Write: got (%d, %v); want (%d, nil)",
						i, chunkSize, n, err, len(chunk))
				}
				n, err = y.WriteString(string(chunk))
				if err != nil || n != len(chunk) {
					t.Fatalf("[i=%d,chunkSize=%d] WriteString: got (%d, %v); want (%d, nil)",
						i, chunkSize, n, err, len(chunk))
				}
			}
			if got := x.Sum64(); got != tt.want {
				t.Fatalf("[i=%d,chunkSize=%d] got 0x%x; want 0x%x",
					i, chunkSize, got, tt.want)
			}
			if got := y.Sum64(); got != tt.want {
				t.Fatalf("[i=%d,chunkSize=%d] string: got 0x%x; want 0x%x",
					i, chunkSize, got, tt.want)
			}
			var b [8]byte
			binary.BigEndian.PutUint64(b[:], tt.want)
			if got := x.Sum(nil); !bytes.Equal(got, b[:]) {
				t.Fatalf("[i=%d,chunkSize=%d] Sum: got %v; want %v",
					i, chunkSize, got, b[:])
			}
		}
		if got := Sum64([]byte(tt.input)); got != tt.want {
			t.Fatalf("[i=%d] Sum64: got 0x%x; want 0x%x", i, got, tt.want)
		}
		if got := Sum64String(tt.input); got != tt.want {
			t.Fatalf("[i=%d] Sum64String: got 0x%x; want 0x%x", i, got, tt.want)
		}
	}
}

func TestReset(t *testing.T) {
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

var (
	sink  uint64
	sinkb []byte
)

var benchmarks = []struct {
	name         string
	directBytes  func([]byte) uint64
	directString func(string) uint64
	digestBytes  func([]byte) uint64
	digestString func(string) uint64
}{
	{
		name:         "xxhash",
		directBytes:  Sum64,
		directString: Sum64String,
		digestBytes: func(b []byte) uint64 {
			h := New()
			h.Write(b)
			return h.Sum64()
		},
		digestString: func(s string) uint64 {
			h := New()
			h.WriteString(s)
			return h.Sum64()
		},
	},
	{
		name:         "OneOfOne",
		directBytes:  OneOfOne.Checksum64,
		directString: OneOfOne.ChecksumString64,
		digestBytes: func(b []byte) uint64 {
			h := OneOfOne.New64()
			h.Write(b)
			return h.Sum64()
		},
		digestString: func(s string) uint64 {
			h := OneOfOne.New64()
			h.WriteString(s)
			return h.Sum64()
		},
	},
	{
		name:        "murmur3",
		directBytes: murmur3.Sum64,
		directString: func(s string) uint64 {
			return murmur3.Sum64([]byte(s))
		},
		digestBytes: func(b []byte) uint64 {
			h := murmur3.New64()
			h.Write(b)
			return h.Sum64()
		},
		digestString: func(s string) uint64 {
			h := murmur3.New64()
			h.Write([]byte(s))
			return h.Sum64()
		},
	},
	{
		name: "CRC-32",
		directBytes: func(b []byte) uint64 {
			return uint64(crc32.ChecksumIEEE(b))
		},
		directString: func(s string) uint64 {
			return uint64(crc32.ChecksumIEEE([]byte(s)))
		},
		digestBytes: func(b []byte) uint64 {
			h := crc32.NewIEEE()
			h.Write(b)
			return uint64(h.Sum32())
		},
		digestString: func(s string) uint64 {
			h := crc32.NewIEEE()
			h.Write([]byte(s))
			return uint64(h.Sum32())
		},
	},
	{
		name: "fnv1a",
		digestBytes: func(b []byte) uint64 {
			h := fnv.New64()
			h.Write(b)
			return uint64(h.Sum64())
		},
		digestString: func(s string) uint64 {
			h := fnv.New64a()
			h.Write([]byte(s))
			return uint64(h.Sum64())
		},
	},
}

func BenchmarkHashes(b *testing.B) {
	for _, ht := range benchmarks {
		for _, nt := range []struct {
			name string
			n    int
		}{
			{"5 B", 5},
			{"100 B", 100},
			{"4 KB", 4e3},
			{"10 MB", 10e6},
		} {
			input := make([]byte, nt.n)
			for i := range input {
				input[i] = byte(i)
			}
			inputString := string(input)
			if ht.directBytes != nil {
				b.Run(fmt.Sprintf("%s,direct,bytes,n=%s", ht.name, nt.name), func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					for i := 0; i < b.N; i++ {
						sink = ht.directBytes(input)
					}
				})
			}
			if ht.directString != nil {
				b.Run(fmt.Sprintf("%s,direct,string,n=%s", ht.name, nt.name), func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					for i := 0; i < b.N; i++ {
						sink = ht.directString(inputString)
					}
				})
			}
			if ht.digestBytes != nil {
				b.Run(fmt.Sprintf("%s,digest,bytes,n=%s", ht.name, nt.name), func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					for i := 0; i < b.N; i++ {
						sink = ht.digestBytes(input)
					}
				})
			}
			if ht.digestString != nil {
				b.Run(fmt.Sprintf("%s,digest,string,n=%s", ht.name, nt.name), func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					for i := 0; i < b.N; i++ {
						sink = ht.digestString(inputString)
					}
				})
			}
		}
	}
}
