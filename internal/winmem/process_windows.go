//go:build windows

package winmem

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"github.com/handsomefox/memedit/internal/scan"
	"golang.org/x/sys/windows"
)

// processAccess is the rights set needed to scan and edit another process:
// query basic info, read and write its memory, and perform VM operations.
const processAccess = windows.PROCESS_QUERY_INFORMATION |
	windows.PROCESS_VM_READ |
	windows.PROCESS_VM_WRITE |
	windows.PROCESS_VM_OPERATION

// Process is an open handle to a target process.
type Process struct {
	handle windows.Handle
	PID    uint32
}

// OpenPID opens the process with the given PID.
func OpenPID(pid uint32) (*Process, error) {
	h, err := windows.OpenProcess(processAccess, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, fmt.Errorf("open process %d: access denied; run from an elevated (Administrator) terminal: %w", pid, err)
		}
		return nil, fmt.Errorf("open process %d: %w", pid, err)
	}
	return &Process{handle: h, PID: pid}, nil
}

// OpenName finds the first process whose executable name matches name
// (case-insensitive, e.g. "game.exe") and opens it.
func OpenName(name string) (*Process, error) {
	pid, err := FindPID(name)
	if err != nil {
		return nil, err
	}
	return OpenPID(pid)
}

// FindPID returns the PID of the first running process whose executable name
// equals name (case-insensitive).
func FindPID(name string) (uint32, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, fmt.Errorf("create process snapshot: %w", err)
	}
	defer windows.CloseHandle(snap) //nolint:errcheck // closing a read-only snapshot

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	want := strings.ToLower(name)

	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		exe := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		if exe == want {
			return entry.ProcessID, nil
		}
	}
	return 0, fmt.Errorf("no running process named %q", name)
}

// Close releases the process handle.
func (p *Process) Close() error {
	if err := windows.CloseHandle(p.handle); err != nil {
		return fmt.Errorf("close process handle: %w", err)
	}
	return nil
}

// ReadInto reads len(buf) bytes at addr into buf, returning the number read.
// On a short read or error the first n bytes remain valid.
func (p *Process) ReadInto(addr uintptr, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	var read uintptr
	err := windows.ReadProcessMemory(p.handle, addr, &buf[0], uintptr(len(buf)), &read)
	if err != nil {
		return int(read), fmt.Errorf("read %d bytes at %#x: %w", len(buf), addr, err)
	}
	return int(read), nil
}

// WriteInto writes buf to addr, returning the number of bytes written.
func (p *Process) WriteInto(addr uintptr, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	var wrote uintptr
	err := windows.WriteProcessMemory(p.handle, addr, &buf[0], uintptr(len(buf)), &wrote)
	if err != nil {
		return int(wrote), fmt.Errorf("write %d bytes at %#x: %w", len(buf), addr, err)
	}
	return int(wrote), nil
}

// Regions walks the address space with VirtualQueryEx and returns the regions
// passing the scannable predicate.
func (p *Process) Regions(includeMapped bool) ([]scan.Region, error) {
	var regions []scan.Region
	var addr uintptr
	for {
		var mbi windows.MemoryBasicInformation
		err := windows.VirtualQueryEx(p.handle, addr, &mbi, unsafe.Sizeof(mbi))
		if err != nil {
			break // past the top of the address space: end of the walk
		}
		if mbi.RegionSize == 0 {
			break
		}
		if scannable(mbi.State, mbi.Protect, mbi.Type, includeMapped) {
			regions = append(regions, scan.Region{Base: mbi.BaseAddress, Size: mbi.RegionSize})
		}
		next := mbi.BaseAddress + mbi.RegionSize
		if next <= addr {
			break // no forward progress / address wrap-around
		}
		addr = next
	}
	//nolint:nilerr // a VirtualQueryEx error is the expected end-of-walk signal, not a failure
	return regions, nil
}
