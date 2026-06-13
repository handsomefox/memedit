package scan

import (
	"bytes"
	"encoding/binary"
)

// Little-endian word helpers.

func leU32(b []byte) uint32     { return binary.LittleEndian.Uint32(b) }
func leU64(b []byte) uint64     { return binary.LittleEndian.Uint64(b) }
func putU32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
func putU64(b []byte, v uint64) { binary.LittleEndian.PutUint64(b, v) }

// appendMatches scans buf for occurrences of needle's little-endian byte
// pattern at offsets that are multiples of align, appending base+offset for
// each match to dst and returning the grown slice. base is the target address
// corresponding to buf[0]; passing 0 yields raw offsets.
//
// It uses bytes.Index (a vectorized assembly substring search) rather than an
// unsafe word-by-word compare; bench_test.go measures both and bytes.Index is
// faster in every case. Hits whose offset is not a multiple of align are
// discarded.
func appendMatches(dst []uintptr, base uintptr, buf []byte, needle Value, align int) []uintptr {
	nb := needle.Bytes() // 4 or 8 little-endian bytes
	width := len(nb)
	if len(buf) < width {
		return dst
	}
	last := len(buf) - width
	for start := 0; start <= last; {
		i := bytes.Index(buf[start:], nb)
		if i < 0 {
			break
		}
		pos := start + i
		if align == 1 || pos%align == 0 {
			dst = append(dst, base+uintptr(pos))
		}
		start = pos + 1
	}
	return dst
}
