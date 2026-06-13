package repl

import (
	"strings"
	"testing"

	"github.com/handsomefox/memedit/internal/scan"
)

// memTarget is an in-memory Target: a single region backed by a byte slice,
// letting the whole REPL be exercised without a real process.
type memTarget struct {
	base uintptr
	data []byte
}

func (m *memTarget) ReadInto(addr uintptr, buf []byte) (int, error) {
	if addr < m.base || addr >= m.base+uintptr(len(m.data)) {
		return 0, errOOB
	}
	return copy(buf, m.data[addr-m.base:]), nil
}

func (m *memTarget) WriteInto(addr uintptr, buf []byte) (int, error) {
	if addr < m.base || addr >= m.base+uintptr(len(m.data)) {
		return 0, errOOB
	}
	return copy(m.data[addr-m.base:], buf), nil
}

func (m *memTarget) Regions(bool) ([]scan.Region, error) {
	return []scan.Region{{Base: m.base, Size: uintptr(len(m.data))}}, nil
}

type oobError struct{}

func (oobError) Error() string { return "out of bounds" }

var errOOB = oobError{}

func newREPL(t *testing.T, kind scan.Kind, data []byte) (*REPL, *strings.Builder) {
	t.Helper()
	tgt := &memTarget{base: 0x10000, data: data}
	out := &strings.Builder{}
	r := New(tgt, Config{Kind: kind, Workers: 2, ChunkSize: 256}, out)
	return r, out
}

func mustParseT(kind scan.Kind, s string) scan.Value {
	v, err := scan.Parse(kind, s)
	if err != nil {
		panic(err)
	}
	return v
}

func put(data []byte, off int, kind scan.Kind, s string) {
	copy(data[off:], mustParseT(kind, s).Bytes())
}

// run feeds newline-separated commands through the dispatcher.
func run(r *REPL, lines ...string) {
	for _, l := range lines {
		r.dispatch(l)
	}
}

func TestFirstThenNextNarrows(t *testing.T) {
	data := make([]byte, 1024)
	// Two locations hold 100 initially.
	put(data, 16, scan.KindInt32, "100")
	put(data, 64, scan.KindInt32, "100")
	put(data, 128, scan.KindInt32, "7") // unrelated

	r, _ := newREPL(t, scan.KindInt32, data)
	run(r, "first 100")
	if len(r.cands) != 2 {
		t.Fatalf("after first 100: %d candidates, want 2 (%v)", len(r.cands), r.cands)
	}

	// One of them changes to 110; narrowing on 110 should leave exactly that one.
	put(data, 64, scan.KindInt32, "110")
	run(r, "next 110")
	if len(r.cands) != 1 || r.cands[0] != 0x10000+64 {
		t.Fatalf("after next 110: %v, want [0x%x]", r.cands, uintptr(0x10000+64))
	}
}

func TestComparisonScans(t *testing.T) {
	data := make([]byte, 512)
	put(data, 0, scan.KindInt32, "50")
	put(data, 8, scan.KindInt32, "50")
	put(data, 16, scan.KindInt32, "50")
	r, _ := newREPL(t, scan.KindInt32, data)
	run(r, "first 50")
	if len(r.cands) != 3 {
		t.Fatalf("first 50: %d candidates, want 3", len(r.cands))
	}

	// Increase two of them; next> 50 keeps those two.
	put(data, 0, scan.KindInt32, "60")
	put(data, 8, scan.KindInt32, "55")
	run(r, "next> 50")
	if len(r.cands) != 2 {
		t.Fatalf("next> 50: %d candidates, want 2 (%v)", len(r.cands), r.cands)
	}

	// nextchanged with no further change keeps none.
	run(r, "nextchanged")
	if len(r.cands) != 0 {
		t.Fatalf("nextchanged after no change: %d, want 0", len(r.cands))
	}
}

func TestNextUnchanged(t *testing.T) {
	data := make([]byte, 256)
	put(data, 0, scan.KindInt32, "5")
	put(data, 8, scan.KindInt32, "5")
	r, _ := newREPL(t, scan.KindInt32, data)
	run(r, "first 5")
	put(data, 8, scan.KindInt32, "9") // one changes
	run(r, "nextunchanged")
	if len(r.cands) != 1 || r.cands[0] != 0x10000 {
		t.Fatalf("nextunchanged: %v, want [0x10000]", r.cands)
	}
}

func TestSetAll(t *testing.T) {
	data := make([]byte, 256)
	put(data, 0, scan.KindInt32, "42")
	put(data, 32, scan.KindInt32, "42")
	r, _ := newREPL(t, scan.KindInt32, data)
	run(r, "first 42", "set 999")

	got := scan.Decode(scan.KindInt32, data[0:])
	got2 := scan.Decode(scan.KindInt32, data[32:])
	want := mustParseT(scan.KindInt32, "999")
	if !got.Equal(want) || !got2.Equal(want) {
		t.Fatalf("set 999 not applied: %s, %s", got, got2)
	}
}

func TestSetByIndex(t *testing.T) {
	data := make([]byte, 256)
	put(data, 0, scan.KindInt32, "3")
	put(data, 16, scan.KindInt32, "3")
	r, out := newREPL(t, scan.KindInt32, data)
	run(r, "first 3", "list", "set 1 77")

	// Only the second listed candidate (index 1, addr 0x10010) should change.
	if v := scan.Decode(scan.KindInt32, data[0:]); v.String() != "3" {
		t.Fatalf("index 0 changed unexpectedly: %s", v)
	}
	want := mustParseT(scan.KindInt32, "77")
	if v := scan.Decode(scan.KindInt32, data[16:]); !v.Equal(want) {
		t.Fatalf("index 1 = %s, want 77", v)
	}
	if !strings.Contains(out.String(), "[1]") {
		t.Fatalf("list output missing index 1: %q", out.String())
	}
}

func TestFloatScan(t *testing.T) {
	data := make([]byte, 256)
	put(data, 0, scan.KindFloat32, "3.5")
	put(data, 64, scan.KindFloat32, "3.5")
	r, _ := newREPL(t, scan.KindFloat32, data)
	run(r, "first 3.5")
	if len(r.cands) != 2 {
		t.Fatalf("float first 3.5: %d candidates, want 2", len(r.cands))
	}
}

func TestQuit(t *testing.T) {
	r, _ := newREPL(t, scan.KindInt32, make([]byte, 64))
	if !r.dispatch("quit") {
		t.Fatal("quit did not request exit")
	}
	if r.dispatch("count") {
		t.Fatal("count requested exit")
	}
}

func TestRunReadsLines(t *testing.T) {
	data := make([]byte, 256)
	put(data, 0, scan.KindInt32, "1")
	r, out := newREPL(t, scan.KindInt32, data)
	r.Run(strings.NewReader("count\nquit\n"))
	if !strings.Contains(out.String(), "candidate") {
		t.Fatalf("Run output missing candidate count: %q", out.String())
	}
}
