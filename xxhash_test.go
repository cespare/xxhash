package xxhash

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/crc32"
	"hash/fnv"
	"strings"
	"testing"

	"github.com/spaolacci/murmur3"
)

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
			}
			if got := x.Sum64(); got != tt.want {
				t.Fatalf("[i=%d,chunkSize=%d] got 0x%x; want 0x%x",
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
	}
}

func TestReset(t *testing.T) {
	parts := []string{"The quic", "k br", "o", "wn fox jumps", " ov", "er the lazy ", "dog."}
	x := New()
	for _, part := range parts {
		x.Write([]byte(part))
	}
	h0 := x.Sum64()

	x.Reset()
	x.Write([]byte(strings.Join(parts, "")))
	h1 := x.Sum64()

	if h0 != h1 {
		t.Errorf("0x%x != 0x%x", h0, h1)
	}
}

func BenchmarkHashes(b *testing.B) {
	for _, ht := range []struct {
		name string
		h    hash.Hash
	}{
		{"xxhash", New()},
		{"murmur3", murmur3.New64()},
		{"SHA-1", sha1.New()},
		{"SHA256", sha256.New()},
		{"CRC-32", crc32.NewIEEE()},
		{"MD5", md5.New()},
		{"FNV", fnv.New64()},
	} {
		for _, nt := range []struct {
			name string
			n    int
		}{
			{"5b", 5},
			{"20b", 20},
			{"100b", 100},
			{"4KB", 4e3},
			{"10MB", 10e6},
		} {
			s := make([]byte, nt.n)
			for i := range s {
				s[i] = byte(i)
			}
			b.Run(
				fmt.Sprintf("%s,n=%s", ht.name, nt.name),
				func(b *testing.B) { bench(b, ht.h, s) },
			)
		}
	}
}

func bench(b *testing.B, h hash.Hash, s []byte) {
	b.SetBytes(int64(len(s)))
	for i := 0; i < b.N; i++ {
		h.Write(s)
	}
}
