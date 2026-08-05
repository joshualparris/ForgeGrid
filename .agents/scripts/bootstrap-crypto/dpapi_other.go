//go:build !windows

package main

import (
	"fmt"
)

func protectData(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("unsupported platform")
}

func unprotectData(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("unsupported platform")
}
