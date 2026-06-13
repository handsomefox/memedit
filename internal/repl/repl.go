// Package repl implements the interactive command loop that drives a memory
// scan: an initial full scan, successive narrowing of the candidate set as the
// in-game value changes, and writing new values. It depends only on the scan
// package and a small Target interface, so it is OS-independent and testable
// with an in-memory target.
package repl

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/handsomefox/memedit/internal/scan"
)

// Target is the process the REPL operates on: it must support memory reads
// (for scanning and narrowing), writes (for set), and region enumeration.
type Target interface {
	scan.Reader
	WriteInto(addr uintptr, buf []byte) (int, error)
	Regions(includeMapped bool) ([]scan.Region, error)
}

// Config holds the scan parameters fixed for a session.
type Config struct {
	Kind          scan.Kind
	Align         int
	Workers       int
	ChunkSize     int
	IncludeMapped bool
}

// REPL is the stateful scanner session.
type REPL struct {
	target Target
	cfg    Config
	out    io.Writer

	// cands and last are parallel slices: cands[i] is a candidate address and
	// last[i] is the value read there at the previous scan, used by comparison
	// scans (next>, nextchanged, ...).
	cands  []uintptr
	last   []scan.Value
	listed []uintptr // candidates shown by the most recent `list`, for `set <index>`
}

// New creates a REPL for target with the given configuration.
func New(target Target, cfg Config, out io.Writer) *REPL {
	return &REPL{target: target, cfg: cfg, out: out}
}

// Run reads commands from in until EOF or a quit command. Per-command failures
// are reported to the output and the loop continues, which suits an
// interactive session.
func (r *REPL) Run(in io.Reader) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	r.printf("memedit (%s). Type 'help' for commands.\n", r.cfg.Kind)
	r.prompt()
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && r.dispatch(line) {
			return // quit requested
		}
		r.prompt()
	}
	if err := sc.Err(); err != nil {
		r.printf("\ninput error: %v\n", err)
	}
}

// printf is the single output sink; a failure writing to the terminal is not
// recoverable or actionable here, so the write error is intentionally ignored.
func (r *REPL) printf(format string, a ...any) {
	fmt.Fprintf(r.out, format, a...) //nolint:errcheck // terminal write errors are not actionable
}
func (r *REPL) prompt() { r.printf("> ") }

// dispatch runs one command line and returns true if the session should quit.
func (r *REPL) dispatch(line string) (quit bool) {
	cmd, arg, _ := strings.Cut(line, " ")
	arg = strings.TrimSpace(arg)
	switch cmd {
	case "quit", "exit", "q":
		return true
	case "help", "h", "?":
		r.help()
	case "first":
		r.cmdFirst(arg)
	case "next":
		r.cmdNext(arg, predEqual)
	case "next>":
		r.cmdNext(arg, predGreater)
	case "next<":
		r.cmdNext(arg, predLess)
	case "nextchanged":
		r.cmdCompare(predChanged)
	case "nextunchanged":
		r.cmdCompare(predUnchanged)
	case "list":
		r.cmdList(arg)
	case "set":
		r.cmdSet(arg)
	case "count":
		r.printf("%d candidate(s)\n", len(r.cands))
	case "reset":
		r.cands, r.last, r.listed = nil, nil, nil
		r.printf("candidates cleared\n")
	default:
		r.printf("unknown command %q (try 'help')\n", cmd)
	}
	return false
}

func (r *REPL) help() {
	r.printf(`commands:
  first <value>        full scan; record every matching address
  next <value>         keep candidates whose current value == <value>
  next> <value>        keep candidates whose current value >  <value>
  next< <value>        keep candidates whose current value <  <value>
  nextchanged          keep candidates whose value changed since last scan
  nextunchanged        keep candidates whose value is unchanged since last scan
  list [n]             show up to n candidates (default 20) with current values
  set <value>          write <value> to every remaining candidate
  set <index> <value>  write <value> to one candidate from the last 'list'
  count                print the number of candidates
  reset                discard all candidates
  help | quit
`)
}

func (r *REPL) parse(s string) (scan.Value, bool) {
	v, err := scan.Parse(r.cfg.Kind, s)
	if err != nil {
		r.printf("error: %v\n", err)
		return scan.Value{}, false
	}
	return v, true
}

// cmdFirst runs the full initial scan.
func (r *REPL) cmdFirst(arg string) {
	if arg == "" {
		r.printf("usage: first <value>\n")
		return
	}
	needle, ok := r.parse(arg)
	if !ok {
		return
	}
	regions, err := r.target.Regions(r.cfg.IncludeMapped)
	if err != nil {
		r.printf("error: enumerate regions: %v\n", err)
		return
	}
	var totalBytes uint64
	for _, reg := range regions {
		totalBytes += uint64(reg.Size)
	}
	r.printf("scanning %d region(s), %s ...\n", len(regions), humanBytes(totalBytes))

	opts := scan.Options{
		Align:     r.cfg.Align,
		Workers:   r.cfg.Workers,
		ChunkSize: r.cfg.ChunkSize,
		Progress:  newProgress(r.out, totalBytes),
	}
	r.cands = scan.Scan(r.target, regions, needle, opts)
	r.last = fill(needle, len(r.cands)) // every match currently holds needle
	r.listed = nil
	r.printf("\nfound %d candidate(s)\n", len(r.cands))
}

// pred decides whether a candidate survives a narrowing scan, given its current
// value and the value recorded at the previous scan.
type pred func(cur, prev, target scan.Value) bool

func predEqual(cur, _, target scan.Value) bool   { return cur.Equal(target) }
func predGreater(cur, _, target scan.Value) bool { return cur.Cmp(target) > 0 }
func predLess(cur, _, target scan.Value) bool    { return cur.Cmp(target) < 0 }
func predChanged(cur, prev, _ scan.Value) bool   { return !cur.Equal(prev) }
func predUnchanged(cur, prev, _ scan.Value) bool { return cur.Equal(prev) }

// cmdNext narrows against a user-supplied value.
func (r *REPL) cmdNext(arg string, p pred) {
	if len(r.cands) == 0 {
		r.printf("no candidates yet; run 'first <value>' first\n")
		return
	}
	if arg == "" {
		r.printf("usage: next[<|>] <value>\n")
		return
	}
	target, ok := r.parse(arg)
	if !ok {
		return
	}
	r.narrow(func(cur, prev scan.Value) bool { return p(cur, prev, target) })
	r.printf("%d candidate(s) remain\n", len(r.cands))
}

// cmdCompare narrows against the previous snapshot (no target value).
func (r *REPL) cmdCompare(p pred) {
	if len(r.cands) == 0 {
		r.printf("no candidates yet; run 'first <value>' first\n")
		return
	}
	r.narrow(func(cur, prev scan.Value) bool { return p(cur, prev, scan.Value{}) })
	r.printf("%d candidate(s) remain\n", len(r.cands))
}

// narrow re-reads every candidate and keeps those for which keep returns true,
// updating the recorded last value for survivors. It compacts cands and last
// in place. Candidates that can no longer be read are dropped.
func (r *REPL) narrow(keep func(cur, prev scan.Value) bool) {
	width := r.cfg.Kind.Size()
	var buf [8]byte
	outC := r.cands[:0]
	outL := r.last[:0]
	for i, addr := range r.cands {
		n, err := r.target.ReadInto(addr, buf[:width])
		if err != nil || n < width {
			continue // unreadable now; drop it
		}
		cur := scan.Decode(r.cfg.Kind, buf[:width])
		if keep(cur, r.last[i]) {
			outC = append(outC, addr)
			outL = append(outL, cur)
		}
	}
	r.cands = outC
	r.last = outL
	r.listed = nil
}

// cmdList shows up to n candidates with their current values.
func (r *REPL) cmdList(arg string) {
	n := 20
	if arg != "" {
		v, err := strconv.Atoi(arg)
		if err != nil || v < 0 {
			r.printf("usage: list [n]\n")
			return
		}
		n = v
	}
	n = min(n, len(r.cands))
	width := r.cfg.Kind.Size()
	var buf [8]byte
	r.listed = r.listed[:0]
	for i := range n {
		addr := r.cands[i]
		cur := "?"
		if rn, err := r.target.ReadInto(addr, buf[:width]); err == nil && rn >= width {
			cur = scan.Decode(r.cfg.Kind, buf[:width]).String()
		}
		r.printf("  [%d] %#x = %s\n", i, addr, cur)
		r.listed = append(r.listed, addr)
	}
	r.printf("showing %d of %d candidate(s)\n", n, len(r.cands))
}

// cmdSet writes a value to all candidates, or to a single listed candidate.
func (r *REPL) cmdSet(arg string) {
	fields := strings.Fields(arg)
	switch len(fields) {
	case 1:
		val, ok := r.parse(fields[0])
		if !ok {
			return
		}
		if len(r.cands) == 0 {
			r.printf("no candidates to set\n")
			return
		}
		r.writeAll(r.cands, val)
	case 2:
		idx, err := strconv.Atoi(fields[0])
		if err != nil {
			r.printf("usage: set <value> | set <index> <value>\n")
			return
		}
		if idx < 0 || idx >= len(r.listed) {
			r.printf("index %d out of range; run 'list' first (0..%d)\n", idx, len(r.listed)-1)
			return
		}
		val, ok := r.parse(fields[1])
		if !ok {
			return
		}
		r.writeAll([]uintptr{r.listed[idx]}, val)
	default:
		r.printf("usage: set <value> | set <index> <value>\n")
	}
}

func (r *REPL) writeAll(addrs []uintptr, val scan.Value) {
	encoded := val.Bytes()
	written, failed := 0, 0
	for _, addr := range addrs {
		if _, err := r.target.WriteInto(addr, encoded); err != nil {
			failed++
			continue
		}
		written++
	}
	if failed > 0 {
		r.printf("wrote %s to %d address(es); %d failed\n", val, written, failed)
	} else {
		r.printf("wrote %s to %d address(es)\n", val, written)
	}
}

// fill returns a slice of n copies of v.
func fill(v scan.Value, n int) []scan.Value {
	s := make([]scan.Value, n)
	for i := range s {
		s[i] = v
	}
	return s
}
