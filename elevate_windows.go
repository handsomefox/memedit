//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// ensureElevated re-launches the program with Administrator rights if the
// current process is not elevated. OpenProcess on a game typically requires
// elevation; rather than fail later with access-denied, we trigger a UAC
// prompt now. On success the elevated copy starts in a new console and the
// current (unelevated) process exits. If elevation is declined or fails, we
// print a hint and continue so the user still gets the access-denied path.
func ensureElevated() {
	if windows.GetCurrentProcessToken().IsElevated() {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: cannot determine executable path for self-elevation:", err)
		return
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return
	}
	args, err := windows.UTF16PtrFromString(strings.Join(os.Args[1:], " "))
	if err != nil {
		return
	}
	var cwd *uint16
	if wd, werr := os.Getwd(); werr == nil {
		cwd, _ = windows.UTF16PtrFromString(wd) //nolint:errcheck // optional working dir; nil is acceptable
	}

	const swShowNormal = 1
	if err := windows.ShellExecute(0, verb, file, args, cwd, swShowNormal); err != nil {
		fmt.Fprintln(os.Stderr, "warning: self-elevation failed; run from an Administrator terminal:", err)
		return
	}
	// The elevated instance is now running in its own window; this one is done.
	os.Exit(0)
}
