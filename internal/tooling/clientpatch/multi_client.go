package clientpatch

import (
	"bytes"
	"debug/pe"
	"encoding/hex"
	"fmt"
	"os"
)

const (
	CoreSingletonSignatureRVA uint32 = 0x26F0
	CoreSingletonBranchRVA    uint32 = 0x2701
)

var coreSingletonOriginal = []byte{
	0x3B, 0xC6, 0x89, 0x07, 0x74, 0x63, 0xFF, 0x15, 0xB4, 0xC4, 0x00, 0x10,
	0x3D, 0xB7, 0x00, 0x00, 0x00, 0x75, 0x56,
}

type MultiClientPatchResult struct {
	Path           string `json:"path"`
	SignatureRVA   string `json:"signature_rva"`
	BranchRVA      string `json:"branch_rva"`
	FileOffset     string `json:"file_offset"`
	OriginalBytes  string `json:"original_bytes"`
	PatchedBytes   string `json:"patched_bytes"`
	BeforeSHA256   string `json:"before_sha256"`
	AfterSHA256    string `json:"after_sha256"`
	AlreadyPatched bool   `json:"already_patched"`
	Behavior       string `json:"behavior"`
}

// PatchStaticMultiClient removes only Core.dll's verified second-instance
// exit edge. Keeping this in the prepared file eliminates the former race in
// which Core could execute its QQTangWinClass check before the runtime module
// watcher suspended the process on slower Windows 10 machines.
func PatchStaticMultiClient(path string) (MultiClientPatchResult, error) {
	data, image, offset, result, err := loadCoreSingleton(path)
	if err != nil {
		return result, err
	}
	defer image.Close()
	patched := append([]byte(nil), coreSingletonOriginal...)
	patched[CoreSingletonBranchRVA-CoreSingletonSignatureRVA] = 0xEB
	current := data[offset : offset+uint32(len(patched))]
	result.OriginalBytes = hex.EncodeToString(current)
	result.PatchedBytes = hex.EncodeToString(patched)
	switch {
	case bytes.Equal(current, patched):
		result.AlreadyPatched = true
		result.AfterSHA256 = result.BeforeSHA256
		return result, nil
	case bytes.Equal(current, coreSingletonOriginal):
		copy(current, patched)
	default:
		return result, fmt.Errorf("Core.dll singleton signature mismatch at RVA 0x%X: got %X", CoreSingletonSignatureRVA, current)
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return result, fmt.Errorf("open Core.dll for patching: %w", err)
	}
	branchOffset := offset + CoreSingletonBranchRVA - CoreSingletonSignatureRVA
	_, writeErr := file.WriteAt([]byte{0xEB}, int64(branchOffset))
	closeErr := file.Close()
	if writeErr != nil {
		return result, fmt.Errorf("write Core.dll singleton branch: %w", writeErr)
	}
	if closeErr != nil {
		return result, fmt.Errorf("close patched Core.dll: %w", closeErr)
	}
	result.AfterSHA256 = sha256Hex(data)
	return result, nil
}

func CheckStaticMultiClient(path string) (MultiClientPatchResult, error) {
	data, image, offset, result, err := loadCoreSingleton(path)
	if err != nil {
		return result, err
	}
	defer image.Close()
	patched := append([]byte(nil), coreSingletonOriginal...)
	patched[CoreSingletonBranchRVA-CoreSingletonSignatureRVA] = 0xEB
	current := data[offset : offset+uint32(len(patched))]
	result.OriginalBytes = hex.EncodeToString(current)
	result.PatchedBytes = hex.EncodeToString(patched)
	result.AlreadyPatched = bytes.Equal(current, patched)
	result.AfterSHA256 = result.BeforeSHA256
	if !result.AlreadyPatched {
		return result, fmt.Errorf("Core.dll static multi-client branch is not patched")
	}
	return result, nil
}

func loadCoreSingleton(path string) ([]byte, *pe.File, uint32, MultiClientPatchResult, error) {
	result := MultiClientPatchResult{
		Path: path, SignatureRVA: fmt.Sprintf("0x%X", CoreSingletonSignatureRVA),
		BranchRVA: fmt.Sprintf("0x%X", CoreSingletonBranchRVA),
		Behavior:  "static prepared-client compatibility: skip only Core.dll's verified QQTangWinClass existing-instance exit edge",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, 0, result, fmt.Errorf("read Core.dll image: %w", err)
	}
	result.BeforeSHA256 = sha256Hex(data)
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, nil, 0, result, fmt.Errorf("parse Core.dll PE: %w", err)
	}
	if image.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		image.Close()
		return nil, nil, 0, result, fmt.Errorf("Core.dll machine 0x%X is not x86", image.FileHeader.Machine)
	}
	offset, err := peRVAFileOffset(image, "Core.dll", CoreSingletonSignatureRVA, uint32(len(coreSingletonOriginal)))
	if err != nil {
		image.Close()
		return nil, nil, 0, result, err
	}
	result.FileOffset = fmt.Sprintf("0x%X", offset+CoreSingletonBranchRVA-CoreSingletonSignatureRVA)
	return data, image, offset, result, nil
}
