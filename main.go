//go:build windows

// Command memedit is a memory scanner/editor for single-player Windows games.
// It attaches to a running process, scans its memory for a value you can see
// in-game, narrows the candidates as that value changes, then writes a new
// value.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/handsomefox/memedit/internal/repl"
	"github.com/handsomefox/memedit/internal/scan"
	"github.com/handsomefox/memedit/internal/winmem"
)

func main() {
	name := flag.String("name", "", "target process executable name, e.g. game.exe")
	pid := flag.Int("pid", 0, "target process id (alternative to --name)")
	typ := flag.String("type", "int32", "value type: int32, uint32, int64, float32, float64")
	align := flag.Int("align", 0, "scan alignment in bytes (default = value size; 1 = exhaustive)")
	workers := flag.Int("workers", runtime.NumCPU(), "number of parallel scan workers")
	chunk := flag.Int("chunk", scan.DefaultChunkBytes, "per-chunk scan size in bytes")
	includeMapped := flag.Bool("include-mapped", false, "also scan file-mapped (MEM_MAPPED) regions")
	noElevate := flag.Bool("no-elevate", false, "do not attempt to self-elevate to Administrator")
	yes := flag.Bool("yes", false, "skip confirmation prompts before scanning for very common values")
	flag.Parse()

	if err := run(*name, *pid, *typ, *align, *workers, *chunk, *includeMapped, *noElevate, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(name string, pid int, typ string, align, workers, chunk int, includeMapped, noElevate, assumeYes bool) error {
	fmt.Println("WARNING: single-player / offline use only. Do NOT use against multiplayer games " +
		"with anti-cheat (VAC / EAC / BattlEye); that is exactly the access they ban for.")

	if !noElevate {
		ensureElevated() // re-launches elevated and exits if needed
	}

	kind, err := scan.ParseKind(typ)
	if err != nil {
		return err
	}

	proc, err := openTarget(name, pid)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := proc.Close(); cerr != nil {
			fmt.Fprintln(os.Stderr, "warning: close process:", cerr)
		}
	}()

	fmt.Printf("attached to PID %d\n", proc.PID)

	cfg := repl.Config{
		Kind:          kind,
		Align:         align,
		Workers:       workers,
		ChunkSize:     chunk,
		IncludeMapped: includeMapped,
		AssumeYes:     assumeYes,
	}
	repl.New(proc, cfg, os.Stdout).Run(os.Stdin)
	return nil
}

// openTarget opens the process selected by --pid or --name.
func openTarget(name string, pid int) (*winmem.Process, error) {
	switch {
	case pid > 0:
		return winmem.OpenPID(uint32(pid))
	case name != "":
		return winmem.OpenName(name)
	default:
		return nil, errors.New("specify a target with --pid <n> or --name <exe>")
	}
}
