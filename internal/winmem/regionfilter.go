// Package winmem opens a target process and provides region enumeration and
// read/write access to its memory. The system-call layer is Windows-only; the
// region-selection predicate below is OS-independent and unit-tested anywhere.
package winmem

// Windows memory constants, redefined here so this file builds on any platform.
const (
	memCommit = 0x00001000
	memMapped = 0x00040000

	pageReadwrite        = 0x00000004
	pageWritecopy        = 0x00000008
	pageExecuteReadwrite = 0x00000040
	pageExecuteWritecopy = 0x00000080
	pageGuard            = 0x00000100

	// protectBaseMask isolates the base protection from modifier flags such as
	// PAGE_GUARD, PAGE_NOCACHE and PAGE_WRITECOMBINE that live in the high bits.
	protectBaseMask = 0x000000ff
)

// scannable reports whether a region with the given flags should be scanned:
// committed, writable, non-guard pages, excluding mapped regions unless
// includeMapped is set.
func scannable(state, protect, typ uint32, includeMapped bool) bool {
	if state != memCommit {
		return false
	}
	if protect&pageGuard != 0 {
		return false
	}
	if !includeMapped && typ == memMapped {
		return false
	}
	switch protect & protectBaseMask {
	case pageReadwrite, pageWritecopy, pageExecuteReadwrite, pageExecuteWritecopy:
		return true
	default:
		return false
	}
}
