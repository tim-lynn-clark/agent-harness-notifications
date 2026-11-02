//go:build !windows

package main

import (
	"os"
	"syscall"
)

// execReplace replaces this process with path, so launchd supervises it directly.
func execReplace(path string, argv []string) error {
	return syscall.Exec(path, argv, os.Environ())
}
