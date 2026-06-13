package scan

import (
	"bytes"
	"testing"
	"unsafe"
)

// benchU32Unsafe is the unsafe.Slice word-compare variant: it reinterprets the
// buffer as []uint32 and compares whole words. Benchmarked here against
// appendMatches (bytes.Index), which is faster.
func benchU32Unsafe(buf []byte, needle uint32, dst []uintptr) []uintptr {
	const width = 4
	if len(buf) < width {
		return dst
	}
	words := unsafe.Slice((*uint32)(unsafe.Pointer(&buf[0])), len(buf)/width) //nolint:gosec // G103: reinterpret read buffer as words for the benchmark comparison
	for i, w := range words {
		if w == needle {
			dst = append(dst, uintptr(i*width))
		}
	}
	return dst
}

// These benchmarks compare three 4-byte matcher implementations: an unsafe
// word compare, a naive strided loader, and a bytes.Index needle search. Run:
//
//	go test -bench=Match -benchmem ./internal/scan
//
// benchU32Naive is the strided little-endian loader.
func benchU32Naive(buf []byte, needle uint32, align int, dst []uintptr) []uintptr {
	last := len(buf) - 4
	for off := 0; off <= last; off += align {
		if leU32(buf[off:]) == needle {
			dst = append(dst, uintptr(off))
		}
	}
	return dst
}

// benchU32BytesIndex searches for the needle's byte encoding with bytes.Index,
// filtering hits by alignment.
func benchU32BytesIndex(buf []byte, needle uint32, align int, dst []uintptr) []uintptr {
	var nb [4]byte
	putU32(nb[:], needle)
	base := 0
	for {
		i := bytes.Index(buf[base:], nb[:])
		if i < 0 {
			break
		}
		pos := base + i
		if pos%align == 0 {
			dst = append(dst, uintptr(pos))
		}
		base = pos + 1
		if base > len(buf)-4 {
			break
		}
	}
	return dst
}

func benchBuf() (buf []byte, needleBits uint32) {
	const size = 16 << 20 // 16 MiB
	filler := mustParse(KindInt32, "1")
	needle := mustParse(KindInt32, "12345")
	buf = buildBuf(size, 4, filler, needle)
	// Sprinkle ~1000 matches so result appends are exercised but rare.
	for i := range 1000 {
		copy(buf[i*16384:], needle.Bytes())
	}
	return buf, uint32(needle.Bits)
}

func BenchmarkMatchAlignedUnsafe(b *testing.B) {
	buf, needle := benchBuf()
	dst := make([]uintptr, 0, 2048)
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for b.Loop() {
		dst = benchU32Unsafe(buf, needle, dst[:0])
	}
	_ = dst
}

func BenchmarkMatchAlignedNaive(b *testing.B) {
	buf, needle := benchBuf()
	dst := make([]uintptr, 0, 2048)
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for b.Loop() {
		dst = benchU32Naive(buf, needle, 4, dst[:0])
	}
	_ = dst
}

func BenchmarkMatchAlignedBytesIndex(b *testing.B) {
	buf, needle := benchBuf()
	dst := make([]uintptr, 0, 2048)
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for b.Loop() {
		dst = benchU32BytesIndex(buf, needle, 4, dst[:0])
	}
	_ = dst
}

func BenchmarkMatchUnalignedNaive(b *testing.B) {
	buf, needle := benchBuf()
	dst := make([]uintptr, 0, 2048)
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for b.Loop() {
		dst = benchU32Naive(buf, needle, 1, dst[:0])
	}
	_ = dst
}

func BenchmarkMatchUnalignedBytesIndex(b *testing.B) {
	buf, needle := benchBuf()
	dst := make([]uintptr, 0, 2048)
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for b.Loop() {
		dst = benchU32BytesIndex(buf, needle, 1, dst[:0])
	}
	_ = dst
}
