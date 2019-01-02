// +build appengine

// This file contains the safe implementations of otherwise unsafe-using code.

package xxhash

func sum64String(s string) uint64 {
	return Sum64([]byte(s))
}

func (d *Digest) writeString(s string) (n int, err error) {
	return d.Write([]byte(s))
}
