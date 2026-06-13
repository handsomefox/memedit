package scan

import (
	"slices"
	"testing"
)

// mustParse parses a known-valid literal for tests and benchmarks.
func mustParse(kind Kind, s string) Value {
	v, err := Parse(kind, s)
	if err != nil {
		panic(err)
	}
	return v
}

// buildBuf returns a buffer of total bytes filled with the little-endian
// encoding of filler (width-sized), then writes needle at each given byte
// offset. It is used to place known matches at precise positions.
func buildBuf(total, width int, filler, needle Value, offsets ...int) []byte {
	buf := make([]byte, total)
	fb := filler.Bytes()
	for i := 0; i+width <= total; i += width {
		copy(buf[i:], fb)
	}
	nb := needle.Bytes()
	for _, off := range offsets {
		copy(buf[off:], nb)
	}
	return buf
}

func offsets(t *testing.T, kind Kind, needleStr string, align int, buf []byte) []int {
	t.Helper()
	needle, err := Parse(kind, needleStr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	addrs := appendMatches(nil, 0, buf, needle, align)
	out := make([]int, len(addrs))
	for i, a := range addrs {
		out[i] = int(a)
	}
	return out
}

func TestMatchAlignedU32(t *testing.T) {
	filler := mustParse(KindInt32, "1")
	needle := mustParse(KindInt32, "12345")
	// Place matches at aligned offsets 0, 8, 40 and an unaligned offset 13.
	buf := buildBuf(64, 4, filler, needle)
	copy(buf[0:], needle.Bytes())
	copy(buf[8:], needle.Bytes())
	copy(buf[40:], needle.Bytes())
	copy(buf[13:], needle.Bytes()) // unaligned, must be ignored at align=4

	got := offsets(t, KindInt32, "12345", 4, buf)
	want := []int{0, 8, 40}
	if !slices.Equal(got, want) {
		t.Fatalf("aligned offsets = %v, want %v", got, want)
	}
}

func TestMatchUnalignedU32(t *testing.T) {
	filler := mustParse(KindInt32, "1")
	needle := mustParse(KindInt32, "12345")
	buf := buildBuf(64, 4, filler, needle)
	copy(buf[13:], needle.Bytes()) // only an unaligned occurrence

	if got := offsets(t, KindInt32, "12345", 4, buf); len(got) != 0 {
		t.Fatalf("align=4 found unaligned match: %v", got)
	}
	got := offsets(t, KindInt32, "12345", 1, buf)
	if !slices.Contains(got, 13) {
		t.Fatalf("align=1 missed offset 13: %v", got)
	}
}

func TestMatchU64(t *testing.T) {
	filler := mustParse(KindInt64, "1")
	needle := mustParse(KindInt64, "9999999999")
	buf := buildBuf(80, 8, filler, needle)
	copy(buf[0:], needle.Bytes())
	copy(buf[16:], needle.Bytes())
	copy(buf[24:], needle.Bytes())

	got := offsets(t, KindInt64, "9999999999", 8, buf)
	want := []int{0, 16, 24}
	if !slices.Equal(got, want) {
		t.Fatalf("u64 offsets = %v, want %v", got, want)
	}
}

func TestMatchFloat32(t *testing.T) {
	filler := mustParse(KindFloat32, "0.5")
	needle := mustParse(KindFloat32, "3.14")
	buf := buildBuf(32, 4, filler, needle)
	copy(buf[12:], needle.Bytes())

	got := offsets(t, KindFloat32, "3.14", 4, buf)
	if !slices.Equal(got, []int{12}) {
		t.Fatalf("float32 offsets = %v, want [12]", got)
	}
}

// TestMatchUnalignedScan checks that an exhaustive (align=1) scan finds values
// at unaligned offsets that an aligned scan would skip.
func TestMatchUnalignedScan(t *testing.T) {
	filler := mustParse(KindInt32, "7")
	needle := mustParse(KindInt32, "42")
	base := buildBuf(128, 4, filler, needle)
	copy(base[4:], needle.Bytes())
	copy(base[64:], needle.Bytes())

	got := offsets(t, KindInt32, "42", 1, base)
	for _, want := range []int{4, 64} {
		if !slices.Contains(got, want) {
			t.Fatalf("strided loader missed offset %d: %v", want, got)
		}
	}
}
