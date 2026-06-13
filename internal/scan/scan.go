package scan

import (
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
)

// Region is a contiguous span of target memory to scan.
type Region struct {
	Base uintptr
	Size uintptr
}

// Reader reads memory from the target process. A short read (n < len(buf)) is
// not an error; the first n bytes are valid.
type Reader interface {
	ReadInto(addr uintptr, buf []byte) (n int, err error)
}

// DefaultChunkBytes is the per-chunk scan granularity.
const DefaultChunkBytes = 2 << 20 // 2 MiB

// chunk is one unit of work: read readLen bytes at addr, then scan them.
type chunk struct {
	addr    uintptr
	readLen int
}

// planChunks tiles a region into chunks. Each chunk owns the start offsets in
// [0, stride) and reads stride+width-1 bytes, so a value straddling the
// boundary is fully present and matched exactly once (the trailing width-1
// bytes belong to the next chunk). The final chunk truncates to the region end.
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

// Options configures a full scan. Zero fields take their defaults.
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
	// chunk's owned window.
	o.ChunkSize = max(o.ChunkSize, o.Align)
	o.ChunkSize -= o.ChunkSize % o.Align
	return o
}

// Scan searches every region for needle and returns the matching target
// addresses in ascending order. Chunks that cannot be read are skipped.
func Scan(r Reader, regions []Region, needle Value, opts Options) []uintptr {
	width := needle.Kind.Size()
	opts = opts.normalized(width)

	// Plan all work up front so one huge region is split across workers.
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
			if !ok { // unreachable: the pool only holds *[]byte
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
					continue // unreadable page; skip the chunk
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
