package scan

import (
	"errors"
	"slices"
	"testing"
)

var errUnmapped = errors.New("unmapped")

// fakeReader is an in-memory stand-in for a target process address space,
// letting the parallel driver be tested on any OS. Reads past a segment's end
// return a short count, mimicking ReadProcessMemory at a region boundary.
type fakeReader struct {
	segs []fakeSeg
}

type fakeSeg struct {
	base uintptr
	data []byte
}

func (f *fakeReader) ReadInto(addr uintptr, buf []byte) (int, error) {
	for _, s := range f.segs {
		end := s.base + uintptr(len(s.data))
		if addr >= s.base && addr < end {
			return copy(buf, s.data[addr-s.base:]), nil
		}
	}
	return 0, errUnmapped
}

func regionsOf(f *fakeReader) []Region {
	rs := make([]Region, len(f.segs))
	for i, s := range f.segs {
		rs[i] = Region{Base: s.base, Size: uintptr(len(s.data))}
	}
	return rs
}

func TestPlanChunksCoversWithoutOverlap(t *testing.T) {
	const width, stride = 4, 16
	r := Region{Base: 0x1000, Size: 50}
	chunks := planChunks(r, width, stride)

	// Owned windows tile [0,Size) exactly. Each non-final chunk reads
	// stride+width-1 bytes; the final chunks truncate to the region end, and a
	// trailing run shorter than width is dropped (offsets 48..49 here).
	want := []chunk{
		{addr: 0x1000, readLen: stride + width - 1}, // owns [0,16), reads [0,19)
		{addr: 0x1010, readLen: stride + width - 1}, // owns [16,32), reads [16,35)
		{addr: 0x1020, readLen: 50 - 32},            // owns [32,48), reads [32,50)
	}
	if !slices.Equal(chunks, want) {
		t.Fatalf("planChunks = %+v, want %+v", chunks, want)
	}
}

func TestScanFindsAllAligned(t *testing.T) {
	filler := mustParse(KindInt32, "1")
	needle := mustParse(KindInt32, "777")
	const base = 0x140000000
	data := buildBuf(4096, 4, filler, needle)
	wantOffsets := []int{0, 100, 2048, 4092}
	for _, o := range wantOffsets {
		copy(data[o:], needle.Bytes())
	}
	f := &fakeReader{segs: []fakeSeg{{base: base, data: data}}}

	// Small chunk size forces many chunks and exercises the worker split.
	got := Scan(f, regionsOf(f), needle, Options{Workers: 4, ChunkSize: 256})
	want := make([]uintptr, len(wantOffsets))
	for i, o := range wantOffsets {
		want[i] = base + uintptr(o)
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("Scan = %v, want %v", got, want)
	}
}

// TestScanStraddlingChunkBoundary places a value that begins in one owned
// window but extends into the next chunk's read overlap, asserting it is found
// exactly once.
func TestScanStraddlingChunkBoundary(t *testing.T) {
	filler := mustParse(KindInt32, "1")
	needle := mustParse(KindInt32, "555")
	const base = 0x200000
	data := buildBuf(512, 4, filler, needle)
	// With align=1 and ChunkSize=64, chunk 0 owns [0,64). Put a value at
	// offset 63: its bytes span 63..66, into the next chunk's territory.
	copy(data[63:], needle.Bytes())
	f := &fakeReader{segs: []fakeSeg{{base: base, data: data}}}

	got := Scan(f, regionsOf(f), needle, Options{Workers: 4, ChunkSize: 64, Align: 1})
	count := 0
	for _, a := range got {
		if a == base+63 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("straddling match found %d times, want 1 (all: %v)", count, got)
	}
}

func TestScanMultipleRegionsSkipsUnmapped(t *testing.T) {
	filler := mustParse(KindInt32, "1")
	needle := mustParse(KindInt32, "9")
	d1 := buildBuf(1024, 4, filler, needle)
	d2 := buildBuf(1024, 4, filler, needle)
	copy(d1[16:], needle.Bytes())
	copy(d2[32:], needle.Bytes())
	f := &fakeReader{segs: []fakeSeg{
		{base: 0x1000, data: d1},
		{base: 0x9000, data: d2}, // gap between regions is unmapped
	}}

	got := Scan(f, regionsOf(f), needle, Options{Workers: 2, ChunkSize: 256})
	want := []uintptr{0x1000 + 16, 0x9000 + 32}
	if !slices.Equal(got, want) {
		t.Fatalf("Scan multi-region = %v, want %v", got, want)
	}
}

func TestFilterNarrows(t *testing.T) {
	// Two candidates; the value at one of them changes so it is dropped.
	data := make([]byte, 64)
	v10 := mustParse(KindInt32, "10")
	v20 := mustParse(KindInt32, "20")
	copy(data[0:], v10.Bytes())
	copy(data[8:], v10.Bytes())
	f := &fakeReader{segs: []fakeSeg{{base: 0x500, data: data}}}

	cands := []uintptr{0x500, 0x508}
	// keep only those currently equal to 10.
	keep := func(_ uintptr, buf []byte) bool { return Decode(KindInt32, buf).Equal(v10) }
	got := Filter(f, cands, 4, keep)
	if !slices.Equal(got, []uintptr{0x500, 0x508}) {
		t.Fatalf("Filter(==10) = %v, want both", got)
	}

	// Change addr 0x508 to 20, re-filter for ==10.
	copy(data[8:], v20.Bytes())
	got = Filter(f, []uintptr{0x500, 0x508}, 4, keep)
	if !slices.Equal(got, []uintptr{0x500}) {
		t.Fatalf("Filter after change = %v, want [0x500]", got)
	}
}
