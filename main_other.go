//go:build !windows

// Command memedit targets windows/amd64. This stub lets the module build and
// the OS-independent tests run on any platform; the real entry point is in
// main.go, guarded by //go:build windows.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "memedit only runs on windows/amd64; build with GOOS=windows GOARCH=amd64")
	os.Exit(1)
}
