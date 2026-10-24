//go:build !windows

package main

import "fmt"

func protectToken(string) (string, error) {
	return "", fmt.Errorf("DPAPI доступен только в Windows")
}

func unprotectToken(string) (string, error) {
	return "", fmt.Errorf("DPAPI доступен только в Windows")
}
