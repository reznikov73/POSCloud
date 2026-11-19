//go:build !windows && !linux

package main

import "fmt"

// Заглушка для платформ, под которые POSCloud не собирается.
// Windows использует DPAPI (dpapi_windows.go), Linux — AES-256-GCM (dpapi_linux.go).

func protectToken(string) (string, error) {
	return "", fmt.Errorf("защита токенов не реализована для этой платформы")
}

func unprotectToken(string) (string, error) {
	return "", fmt.Errorf("защита токенов не реализована для этой платформы")
}
