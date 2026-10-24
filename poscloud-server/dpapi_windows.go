//go:build windows

package main

import (
	"encoding/base64"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// dataBlob — структура DATA_BLOB для DPAPI.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32DLL             = windows.NewLazySystemDLL("crypt32.dll")
	kernel32DLL            = windows.NewLazySystemDLL("kernel32.dll")
	procCryptProtectData   = crypt32DLL.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32DLL.NewProc("CryptUnprotectData")
	procLocalFree          = kernel32DLL.NewProc("LocalFree")
)

// CRYPTPROTECT_UI_FORBIDDEN — не показывать системные диалоги.
const cryptProtectUIForbidden = 0x1

// protectToken шифрует токен через DPAPI в контексте текущего пользователя.
func protectToken(token string) (string, error) {
	enc, err := dpapiCrypt([]byte(token), true)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(enc), nil
}

// unprotectToken расшифровывает токен, сохранённый protectToken.
func unprotectToken(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("некорректный шифротекст токена: %w", err)
	}
	dec, err := dpapiCrypt(raw, false)
	if err != nil {
		return "", err
	}
	return string(dec), nil
}

func dpapiCrypt(data []byte, protect bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("DPAPI: пустые данные")
	}
	in := dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	var out dataBlob
	var r uintptr
	var callErr error
	if protect {
		r, _, callErr = procCryptProtectData.Call(
			uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0,
			uintptr(cryptProtectUIForbidden), uintptr(unsafe.Pointer(&out)),
		)
	} else {
		r, _, callErr = procCryptUnprotectData.Call(
			uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0,
			uintptr(cryptProtectUIForbidden), uintptr(unsafe.Pointer(&out)),
		)
	}
	if r == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
			return nil, fmt.Errorf("DPAPI: %w", errno)
		}
		return nil, fmt.Errorf("DPAPI: не удалось обработать данные")
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	if out.pbData == nil || out.cbData == 0 {
		return nil, fmt.Errorf("DPAPI: пустой результат")
	}
	b := unsafe.Slice(out.pbData, int(out.cbData))
	res := make([]byte, len(b))
	copy(res, b)
	return res, nil
}
