//go:build windows

package main

import "syscall"

// createNoWindow is CREATE_NO_WINDOW. Unlike DETACHED_PROCESS it gives `fire`
// a hidden console, which the PowerShell player inherits, so playing a sound
// never opens a visible console window.
const createNoWindow = 0x08000000

func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: createNoWindow | syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}
