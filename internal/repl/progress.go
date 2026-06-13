package repl

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// newProgress returns a scan progress callback that prints a row of dots as
// bytes are scanned, so a long first scan does not look frozen. The callback is
// safe for concurrent use by worker goroutines. total is the number of bytes to
// be scanned; if it is zero the callback is a no-op.
func newProgress(out io.Writer, total uint64) func(scanned int) {
	if total == 0 {
		return func(int) {}
	}
	const dots = 40
	step := total / dots
	if step == 0 {
		step = 1
	}
	var done atomic.Uint64
	var mu sync.Mutex
	return func(scanned int) {
		cur := done.Add(uint64(scanned))
		prev := cur - uint64(scanned)
		// Emit one dot per step boundary this call crossed; summed across all
		// calls this prints ~dots dots regardless of chunk scheduling.
		for s := prev/step + 1; s <= cur/step; s++ {
			mu.Lock()
			fmt.Fprint(out, ".") //nolint:errcheck // progress output; write errors are not actionable
			mu.Unlock()
		}
	}
}

// humanBytes formats a byte count with a binary unit suffix.
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
