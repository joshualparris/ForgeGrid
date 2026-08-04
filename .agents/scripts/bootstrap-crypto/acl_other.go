//go:build !windows

package main

import (
	"os"
)

func secureBootstrapDirectory(dir string) error {
	return os.MkdirAll(dir, 0700)
}
