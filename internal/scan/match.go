package scan

import (
	"bytes"
	"encoding/binary"
)

// Little-endian word helpers. These wrap encoding/binary so the rest of the
// package reads cleanly; the compiler inlines them to single loads/stores.

func leU32(b []byte) uint32     { return binary.LittleEndian.Uint32(b) }
func leU64(b []byte) uint64     { return binary.LittleEndian.Uint64(b) }
func putU32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
func putU64(b []byte, v uint64) { binary.LittleEndian.PutUint64(b, v) }

// appendMatches scans buf for occurrences of needle's little-endian byte
// pattern at offsets that are multiples of align, appending base+offset for
// each match to dst and returning the grown slice. base is the target address
// corresponding to buf[0]; passing 0 yields raw offsets (used by the tests).
//
// Implementation note: this uses bytes.Index rather than an unsafe word-by-word
// compare. The matcher is the hot path, so the choice was benchmarked (see
// bench_test.go) over a 16 MiB buffer:
//
//	aligned, rare anchor byte:  unsafe ~4.7 GB/s, naive ~5.5 GB/s, Index ~52 GB/s
//	aligned, common (0x00):     unsafe ~4.9 GB/s,                 Index ~8.9 GB/s
//	unaligned (--align 1):      naive  ~0.9 GB/s,                 Index ~49 GB/s
//
// bytes.Index wins decisively in every case because it dispatches to a
// vectorized assembly substring search; it also makes aligned and unaligned
// scans the same code path. We honour the value's alignment by discarding hits
// whose offset is not a multiple of align.
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
