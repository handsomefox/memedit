//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// ensureElevated re-launches the program elevated (via a UAC prompt) if it is
// not already, since OpenProcess on a game usually requires Administrator. On
// success the elevated copy starts in a new console and this process exits; if
// elevation is declined or fails, it prints a hint and continues.
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
