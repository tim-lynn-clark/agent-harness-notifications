//go:build windows

package main

import "errors"

func execReplace(string, []string) error {
	return errors.New("not supported on Windows")
}
