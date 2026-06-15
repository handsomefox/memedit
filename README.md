# memedit

A command-line memory scanner/editor for single-player Windows games, written
in Go with no CGo. It performs the same workflow as Cheat Engine: attach to a
running process, scan its memory for a value you can see in-game, narrow the
candidate addresses as that value changes, then write a new value.

## Single-player / offline use only

Do NOT use this against multiplayer games protected by anti-cheat (VAC, EAC,
BattlEye, etc.). Reading and writing another process's memory is exactly the
access those systems detect and ban for. This tool is for offline, single-player
games and for learning how memory scanning works.

It requires Administrator rights to open most game processes. It will offer to
self-elevate via a UAC prompt unless you pass `--no-elevate`.

## Build

The binary targets `windows/amd64`:

```sh
make build                                       # produces memedit.exe
# or:
GOOS=windows GOARCH=amd64 go build -o memedit.exe .
```

`make help` lists the other targets (`test`, `bench`, `vet`, `lint`, `check`).

The value-matching and scan-driver code is OS-independent, so its tests and
benchmarks run on any platform:

```sh
go test ./...
go test -bench=Match -benchmem ./internal/scan
```

## Usage

```
memedit --name <exe> | --pid <n> [flags]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `--name <exe>` | | Target process executable name, e.g. `game.exe` |
| `--pid <n>` | | Target process id (alternative to `--name`) |
| `--type <t>` | `int32` | Value type: `int32`, `uint32`, `int64`, `float32`, `float64` |
| `--align <n>` | value size | Scan alignment in bytes; `1` = exhaustive (slower) |
| `--workers <n>` | `NumCPU` | Parallel scan workers |
| `--chunk <n>` | 2 MiB | Per-chunk scan size in bytes |
| `--include-mapped` | `false` | Also scan file-mapped (`MEM_MAPPED`) regions |
| `--no-elevate` | `false` | Do not attempt to self-elevate to Administrator |
| `--yes` | `false` | Skip the confirmation prompt before scanning for very common values like `0` or `1` |

### REPL commands

Attaching and then narrowing candidates is stateful (the candidate set must
survive between actions while the value changes in-game), so the tool is an
interactive REPL rather than a one-shot command.

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

The game shows 2500 gold and you want to change it.

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

Spend some gold so the value becomes 2375, then narrow:

```
> next 2375
121 candidate(s) remain
```

Earn a little so it reads 2400, and narrow again until one or a few remain:

```
> next 2400
1 candidate(s) remain
> list
  [0] 0x1f3a4c10 = 2400
showing 1 of 1 candidate(s)
```

Set the value:

```
> set 999999
wrote 999999 to 1 address(es)
> quit
```

If many candidates remain and you cannot change the value precisely, use the
comparison scans: `nextchanged` / `nextunchanged` after the value does or does
not change, or `next>` / `next<` when you only know the direction.

## Notes and limitations

- Scanning for a very common value (`0` or `1`, or exact `0.0` for the float
  types) can match millions of addresses and use a lot of memory, so `first`
  warns and asks `proceed? [y/N]` first. Because that prompt reuses the command
  input stream, pass `--yes` for scripted / non-interactive (piped) runs —
  otherwise the next piped line is consumed as the answer and the prompt
  defaults to no.
- Float matching is exact (bit-for-bit). A health bar that displays `100` is
  rarely stored as exactly `100.0`. If an exact float scan finds nothing, scan
  for the value as actually stored, or use the comparison scans.
- 64-bit target processes on `windows/amd64` only.
