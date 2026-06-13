# memedit

A fast command-line memory scanner/editor for **single-player Windows games**,
written in Go with no CGo. It performs the same workflow as Cheat Engine:
attach to a running process, scan its memory for a value you can see in-game,
narrow the candidate addresses as that value changes, then write a new value.

The point of this tool is **raw scan speed** on multi-GB game heaps: the scan is
parallel across CPU cores, regions are split into fixed-size chunks fed to a
worker pool, and the inner matcher uses Go's vectorized `bytes.Index`.

## ⚠️ Single-player / offline use only

> **Do NOT use this against multiplayer games protected by anti-cheat (VAC,
> EAC, BattlEye, etc.).** Reading and writing another process's memory is
> exactly the access those systems detect and ban for. This tool is intended
> for offline, single-player games and for learning how memory scanning works.

It also requires Administrator rights to open most game processes (it will offer
to self-elevate via a UAC prompt).

## Build

Targets `windows/amd64`:

```sh
make build                                          # -> memedit.exe
# or, equivalently:
GOOS=windows GOARCH=amd64 go build -o memedit.exe .
```

Run `make help` for the other targets (`test`, `bench`, `vet`, `lint`, `check`).

The OS-independent value-matching and scan-driver code builds and its tests run
on any platform, so you can develop and test on Linux/macOS:

```sh
go test ./...
go test -bench=Match -benchmem ./internal/scan   # matcher benchmarks
```

## Usage

```
memedit --name <exe> | --pid <n> [flags]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `--name <exe>` | – | Target process executable name, e.g. `game.exe` |
| `--pid <n>` | – | Target process id (alternative to `--name`) |
| `--type <t>` | `int32` | Value type: `int32`, `uint32`, `int64`, `float32`, `float64` |
| `--align <n>` | value size | Scan alignment in bytes; `1` = exhaustive (slower) |
| `--workers <n>` | `NumCPU` | Parallel scan workers |
| `--chunk <n>` | 2 MiB | Per-chunk scan size in bytes |
| `--include-mapped` | `false` | Also scan file-mapped (`MEM_MAPPED`) regions |
| `--no-elevate` | `false` | Do not attempt to self-elevate to Administrator |

### REPL commands

Once attached you get an interactive prompt (the candidate set must survive
between actions while the value changes in-game, so this is a REPL, not a
one-shot command):

```
first <value>        full scan; record every matching address
next <value>         keep candidates whose current value == <value>
next> <value>        keep candidates whose current value >  <value>
next< <value>        keep candidates whose current value <  <value>
nextchanged          keep candidates whose value changed since the last scan
nextunchanged        keep candidates whose value is unchanged since the last scan
list [n]             show up to n candidates (address, current value)
set <value>          write <value> to every remaining candidate
set <index> <value>  write <value> to one candidate from the last `list`
count                print the number of candidates
reset                discard all candidates
help | quit
```

## Worked example: find and edit a currency value

Say your game shows **2500 gold** and you want to change it.

```
$ memedit.exe --name game.exe --type int32
WARNING: single-player / offline use only. ...
attached to PID 12345
memedit (int32). Type 'help' for commands.

> first 2500
scanning 214 region(s), 3.4 GiB ...
........................................
found 18342 candidate(s)
```

Spend some gold in-game so the value becomes, say, **2375**, then narrow:

```
> next 2375
121 candidate(s) remain
```

Earn a little so it reads **2400**, narrow again until one or a few remain:

```
> next 2400
1 candidate(s) remain
> list
  [0] 0x1f3a4c10 = 2400
showing 1 of 1 candidate(s)
```

Set it to whatever you like:

```
> set 999999
wrote 999999 to 1 address(es)
> quit
```

If many candidates remain and you can't change the value precisely, the
comparison scans help: use `nextchanged` / `nextunchanged` after the value does
or doesn't change, or `next>` / `next<` when you only know the direction.

## How it's optimized

The scan is the hot path, so the design is built around it:

- **Parallel chunked scan.** All target regions are enumerated, then split into
  fixed-size chunks (default 2 MiB) and fed to a pool of `--workers` goroutines,
  so one giant heap region can't bottleneck a single worker. Each worker reuses
  a single read buffer (via `sync.Pool`) — zero per-chunk allocation — and
  appends to a per-worker result slice, so there's no lock contention in the hot
  path. Results are merged and sorted once at the end.
- **Region filtering.** Only committed (`MEM_COMMIT`), writable, non-guard pages
  are scanned; file-mapped regions are skipped unless `--include-mapped`.
- **Exact chunk ownership.** Each chunk owns the start offsets in `[0, stride)`
  and reads `stride + valueSize − 1` bytes, so a value straddling a chunk
  boundary is fully present and matched exactly once — no overlap/dedup pass
  needed.
- **Vectorized matcher.** The inner loop was benchmarked three ways over a
  16 MiB buffer: an `unsafe.Slice` word-by-word compare, a naive strided
  `binary.LittleEndian` loop, and a `bytes.Index` needle search. `bytes.Index`
  (which dispatches to SIMD assembly) won decisively and is what's used:

  | matcher | aligned | unaligned (`--align 1`) |
  |---------|--------:|------------------------:|
  | unsafe word-compare | ~4.4 GB/s | n/a |
  | naive strided loop  | ~4.5 GB/s | ~1.8 GB/s |
  | **bytes.Index**     | **~57 GB/s** | **~58 GB/s** |

  Run `go test -bench=Match -benchmem ./internal/scan` to reproduce.

Subsequent `next` scans only touch the existing candidate list, so they are
effectively instant.

## Notes & limitations

- **Float matching is exact (bit-for-bit).** A health bar that displays `100`
  is rarely stored as exactly `100.0`; Cheat Engine uses a tolerance for this.
  If an exact float scan finds nothing, try scanning as the value actually
  stored, or use the comparison scans (`nextchanged`, `next>`, …).
- 64-bit target processes on `windows/amd64` only.

## Project layout

```
main.go, elevate_windows.go   flag wiring + admin self-escalation (//go:build windows)
main_other.go                 non-Windows build stub
internal/scan                 OS-independent: value parsing, matcher, parallel driver (+ tests, benchmark)
internal/winmem               Windows syscall layer: open, regions, read/write (+ OS-independent region filter test)
internal/repl                 interactive command loop (+ tests with an in-memory target)
```
