package clientpatch

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
)

const ShopServerSelectorRVA uint32 = 0x2030B

var shopServerSelectorOriginal = []byte{
	0x9C, 0x89, 0x2C, 0x24, 0x8B, 0xEC, 0x8D, 0xA4,
	0x24, 0xFC, 0xFF, 0xFF, 0xFF, 0x89, 0x0C,
}

type ShopServerPatchResult struct {
	Path           string `json:"path"`
	RVA            string `json:"rva"`
	FileOffset     string `json:"file_offset"`
	ServerID       uint32 `json:"server_id"`
	OriginalBytes  string `json:"original_bytes"`
	PatchedBytes   string `json:"patched_bytes"`
	BeforeSHA256   string `json:"before_sha256"`
	AfterSHA256    string `json:"after_sha256"`
	AlreadyPatched bool   `json:"already_patched"`
	Behavior       string `json:"behavior"`
}

// PatchStaticShopServer replaces only QQTDir::GetRandShopServerID in a
// prepared client copy. The historical selector depends on an online
// directory candidate pool that is empty in the local service. Keeping the
// replacement in the file removes the per-process writer and its lifetime
// helper while preserving the original shop protocol and server lookup.
func PatchStaticShopServer(path string, serverID uint32) (ShopServerPatchResult, error) {
	result := ShopServerPatchResult{
		Path: path, RVA: fmt.Sprintf("0x%X", ShopServerSelectorRVA), ServerID: serverID,
		Behavior: "static prepared-client compatibility: return the configured local shop server ID; no runtime process injection",
	}
	if serverID == 0 {
		return result, fmt.Errorf("shop server ID must be non-zero")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, fmt.Errorf("read QQTDir image: %w", err)
	}
	result.BeforeSHA256 = sha256Hex(data)

	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return result, fmt.Errorf("parse QQTDir PE: %w", err)
	}
	defer image.Close()
	if image.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		return result, fmt.Errorf("QQTDir machine 0x%X is not x86", image.FileHeader.Machine)
	}
	patch := staticShopServerPatch(serverID)
	offset, err := peRVAFileOffset(image, "QQTDir", ShopServerSelectorRVA, uint32(len(patch)))
	if err != nil {
		return result, err
	}
	result.FileOffset = fmt.Sprintf("0x%X", offset)
	if uint64(offset)+uint64(len(patch)) > uint64(len(data)) {
		return result, fmt.Errorf("shop selector patch extends beyond QQTDir file")
	}
	current := data[offset : offset+uint32(len(patch))]
	result.OriginalBytes = hex.EncodeToString(current)
	result.PatchedBytes = hex.EncodeToString(patch)
	switch {
	case bytes.Equal(current, patch):
		result.AlreadyPatched = true
		result.AfterSHA256 = result.BeforeSHA256
		return result, nil
	case bytes.Equal(current, shopServerSelectorOriginal):
		copy(current, patch)
	default:
		return result, fmt.Errorf("QQTDir shop selector signature mismatch at RVA 0x%X: got %X", ShopServerSelectorRVA, current)
	}

	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return result, fmt.Errorf("open QQTDir for patching: %w", err)
	}
	_, writeErr := file.WriteAt(patch, int64(offset))
	closeErr := file.Close()
	if writeErr != nil {
		return result, fmt.Errorf("write QQTDir shop selector: %w", writeErr)
	}
	if closeErr != nil {
		return result, fmt.Errorf("close patched QQTDir: %w", closeErr)
	}
	result.AfterSHA256 = sha256Hex(data)
	return result, nil
}

func CheckStaticShopServer(path string, serverID uint32) (ShopServerPatchResult, error) {
	if serverID == 0 {
		return ShopServerPatchResult{}, fmt.Errorf("shop server ID must be non-zero")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ShopServerPatchResult{}, fmt.Errorf("read QQTDir image: %w", err)
	}
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return ShopServerPatchResult{}, fmt.Errorf("parse QQTDir PE: %w", err)
	}
	defer image.Close()
	if image.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		return ShopServerPatchResult{}, fmt.Errorf("QQTDir machine 0x%X is not x86", image.FileHeader.Machine)
	}
	patch := staticShopServerPatch(serverID)
	offset, err := peRVAFileOffset(image, "QQTDir", ShopServerSelectorRVA, uint32(len(patch)))
	if err != nil {
		return ShopServerPatchResult{}, err
	}
	current := data[offset : offset+uint32(len(patch))]
	result := ShopServerPatchResult{
		Path: path, RVA: fmt.Sprintf("0x%X", ShopServerSelectorRVA), FileOffset: fmt.Sprintf("0x%X", offset),
		ServerID: serverID, OriginalBytes: hex.EncodeToString(current), PatchedBytes: hex.EncodeToString(patch),
		BeforeSHA256: sha256Hex(data), AfterSHA256: sha256Hex(data), AlreadyPatched: bytes.Equal(current, patch),
		Behavior: "verify static prepared-client shop server compatibility",
	}
	if !result.AlreadyPatched {
		return result, fmt.Errorf("QQTDir static shop selector is not patched for server ID %d", serverID)
	}
	return result, nil
}

func staticShopServerPatch(serverID uint32) []byte {
	// mov eax,[esp+8]; mov dword ptr [eax],serverID; xor eax,eax; ret 8
	patch := []byte{0x8B, 0x44, 0x24, 0x08, 0xC7, 0x00, 0, 0, 0, 0, 0x33, 0xC0, 0xC2, 0x08, 0x00}
	binary.LittleEndian.PutUint32(patch[6:10], serverID)
	return patch
}

func peRVAFileOffset(image *pe.File, module string, rva, size uint32) (uint32, error) {
	for _, section := range image.Sections {
		start := section.VirtualAddress
		if rva < start {
			continue
		}
		delta := rva - start
		if delta > section.Size || size > section.Size-delta {
			continue
		}
		if section.Characteristics&pe.IMAGE_SCN_MEM_EXECUTE == 0 {
			return 0, fmt.Errorf("%s RVA 0x%X is not in an executable section", module, rva)
		}
		return section.Offset + delta, nil
	}
	return 0, fmt.Errorf("%s RVA 0x%X is outside raw PE sections", module, rva)
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
