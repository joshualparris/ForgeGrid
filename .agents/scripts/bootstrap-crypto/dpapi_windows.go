//go:build windows

package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const CRYPTPROTECT_UI_FORBIDDEN = 0x1

func protectData(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("no data to protect")
	}

	var dataIn windows.DataBlob
	dataIn.Data = &data[0]
	dataIn.Size = uint32(len(data))

	var dataOut windows.DataBlob

	err := windows.CryptProtectData(&dataIn, nil, nil, 0, nil, CRYPTPROTECT_UI_FORBIDDEN, &dataOut)
	if err != nil {
		return nil, fmt.Errorf("CryptProtectData failed: %v", err)
	}

	res := make([]byte, dataOut.Size)
	copy(res, unsafe.Slice(dataOut.Data, dataOut.Size))

	windows.LocalFree(windows.Handle(unsafe.Pointer(dataOut.Data)))
	return res, nil
}

func unprotectData(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("no data to unprotect")
	}

	var dataIn windows.DataBlob
	dataIn.Data = &data[0]
	dataIn.Size = uint32(len(data))

	var dataOut windows.DataBlob

	err := windows.CryptUnprotectData(&dataIn, nil, nil, 0, nil, CRYPTPROTECT_UI_FORBIDDEN, &dataOut)
	if err != nil {
		return nil, fmt.Errorf("CryptUnprotectData failed: %v", err)
	}

	res := make([]byte, dataOut.Size)
	copy(res, unsafe.Slice(dataOut.Data, dataOut.Size))
	// Clear the plaintext buffer from DPAPI after copying
	for i := 0; i < int(dataOut.Size); i++ {
		*(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(dataOut.Data)) + uintptr(i))) = 0
	}

	windows.LocalFree(windows.Handle(unsafe.Pointer(dataOut.Data)))
	return res, nil
}
