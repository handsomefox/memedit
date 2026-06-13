package scan

import (
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
)

// Region is a contiguous span of target memory to scan, identified by its base
// address and size in bytes.
type Region struct {
	Base uintptr
	Size uintptr
}

// Reader reads memory from the target process. ReadInto fills buf with the
// bytes at addr and returns the number read; a short read (n < len(buf)) is not
// an error and the first n bytes are still valid.
type Reader interface {
	ReadInto(addr uintptr, buf []byte) (n int, err error)
}

// DefaultChunkBytes is the per-chunk scan granularity. Splitting regions into
// fixed-size chunks keeps one giant heap region from bottlenecking a single
// worker.
const DefaultChunkBytes = 2 << 20 // 2 MiB

// chunk describes one unit of work: read readLen bytes at addr, then scan the
// owned start offsets within them.
type chunk struct {
	addr    uintptr
	readLen int
}

// planChunks tiles a region into chunks. Each chunk owns the start offsets in
// [0, stride) and reads stride+width-1 bytes so a value straddling the boundary
// to the next chunk is still fully present and found exactly once (the trailing
// width-1 bytes are owned by the following chunk). The final chunk truncates to
// the region end. Reads never cross into a neighbouring region.
func planChunks(r Region, width, stride int) []chunk {
	if r.Size < uintptr(width) {
		return nil
	}
	var chunks []chunk
	ustride := uintptr(stride)
	overlap := uintptr(width - 1)
	for off := uintptr(0); off < r.Size; off += ustride {
		remaining := r.Size - off
		readLen := min(ustride+overlap, remaining)
		if readLen < uintptr(width) {
			break // trailing bytes too short to hold a value
		}
		chunks = append(chunks, chunk{addr: r.Base + off, readLen: int(readLen)})
	}
	return chunks
}

// Options configures a full scan.
type Options struct {
	Align     int // start-offset alignment; defaults to value size
	Workers   int // goroutine count; defaults to runtime.NumCPU()
	ChunkSize int // per-chunk bytes; defaults to DefaultChunkBytes
	// Progress, if set, is called from worker goroutines with the number of
	// bytes just scanned. It must be safe for concurrent use.
	Progress func(scanned int)
}

func (o Options) normalized(width int) Options {
	if o.Align <= 0 {
		o.Align = width
	}
	if o.Workers <= 0 {
		o.Workers = runtime.NumCPU()
	}
	if o.ChunkSize <= 0 {
		o.ChunkSize = DefaultChunkBytes
	}
	// Stride must be a multiple of align so aligned values never straddle a
	// chunk's owned window, and at least one alignment step wide.
	if o.ChunkSize < o.Align {
		o.ChunkSize = o.Align
	}
	o.ChunkSize -= o.ChunkSize % o.Align
	return o
}

// Scan searches every region for needle and returns the matching target
// addresses in ascending order. Read failures on individual chunks (guard
// pages, races with the target freeing memory) are skipped rather than
// aborting the scan.
func Scan(r Reader, regions []Region, needle Value, opts Options) []uintptr {
	width := needle.Kind.Size()
	opts = opts.normalized(width)

	// Plan all work up front so a single huge region is split across workers.
	work := make([]chunk, 0, len(regions))
	for _, reg := range regions {
		work = append(work, planChunks(reg, width, opts.ChunkSize)...)
	}
	if len(work) == 0 {
		return nil
	}

	bufLen := opts.ChunkSize + width - 1
	bufPool := sync.Pool{New: func() any { b := make([]byte, bufLen); return &b }}

	var next atomic.Int64
	results := make([][]uintptr, opts.Workers)

	var wg sync.WaitGroup
	for w := range opts.Workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			bufp, ok := bufPool.Get().(*[]byte)
			if !ok { // unreachable: the pool only ever holds *[]byte
				b := make([]byte, bufLen)
				bufp = &b
			}
			buf := *bufp
			defer bufPool.Put(bufp)

			local := results[id]
			for {
				i := int(next.Add(1)) - 1
				if i >= len(work) {
					break
				}
				c := work[i]
				n, err := r.ReadInto(c.addr, buf[:c.readLen])
				if err != nil && n < width {
					// Skip the whole chunk. ReadProcessMemory fails the entire
					// requested range if any page in it is unreadable; that's
					// safe here because the region filter only admits
					// homogeneously committed, readable regions, so mid-region
					// holes are unlikely.
					continue
				}
				local = appendMatches(local, c.addr, buf[:n], needle, opts.Align)
				if opts.Progress != nil {
					opts.Progress(n)
				}
			}
			results[id] = local
		}(w)
	}
	wg.Wait()

	var total int
	for _, r := range results {
		total += len(r)
	}
	merged := make([]uintptr, 0, total)
	for _, r := range results {
		merged = append(merged, r...)
	}
	slices.Sort(merged)
	return merged
}
