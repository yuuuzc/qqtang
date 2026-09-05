package clientpatch

import (
	"bytes"
	"debug/pe"
	"encoding/hex"
	"fmt"
	"os"
)

const (
	AvatarForgeTypeNameCallRVA  uint32 = 0x14CF70
	AvatarForgeColorNameCallRVA uint32 = 0x14D164
)

type avatarForgeBoundarySite struct {
	name       string
	rva        uint32
	contextRVA uint32
	context    []byte
}

var avatarForgeBoundarySites = []avatarForgeBoundarySite{
	{
		name:       "forge-type-name",
		rva:        AvatarForgeTypeNameCallRVA,
		contextRVA: 0x14CF65,
		context: []byte{
			0x8B, 0x95, 0xE4, 0xFE, 0xFF, 0xFF, 0x3B, 0x55, 0xE8, 0x7D, 0x76,
			0x68, 0x00, 0x01, 0x00, 0x00, 0x8D, 0x85, 0xE8, 0xFE, 0xFF, 0xFF,
			0x50, 0x8B, 0x8D, 0xE4, 0xFE, 0xFF, 0xFF, 0x51, 0x8B, 0x55, 0xEC,
			0x8B, 0x02, 0x8B, 0x4D, 0xEC, 0x51, 0xFF, 0x90, 0x4C, 0x03, 0x00, 0x00,
		},
	},
	{
		name:       "forge-color-name",
		rva:        AvatarForgeColorNameCallRVA,
		contextRVA: 0x14D159,
		context: []byte{
			0x8B, 0x85, 0xE4, 0xFE, 0xFF, 0xFF, 0x3B, 0x45, 0xE8, 0x7D, 0x7A,
			0x68, 0x00, 0x01, 0x00, 0x00, 0x8D, 0x8D, 0xE8, 0xFE, 0xFF, 0xFF,
			0x51, 0x8B, 0x95, 0xE4, 0xFE, 0xFF, 0xFF, 0x52, 0x8B, 0x45, 0x0C,
			0x50, 0x8B, 0x4D, 0xEC, 0x8B, 0x11, 0x8B, 0x45, 0xEC, 0x50, 0xFF,
			0x92, 0x54, 0x03, 0x00, 0x00,
		},
	},
}

type AvatarForgeBoundarySiteResult struct {
	Name           string `json:"name"`
	RVA            string `json:"rva"`
	FileOffset     string `json:"file_offset"`
	OriginalBytes  string `json:"original_bytes"`
	PatchedBytes   string `json:"patched_bytes"`
	AlreadyPatched bool   `json:"already_patched"`
}

type AvatarForgeBoundaryPatchResult struct {
	Path           string                          `json:"path"`
	BeforeSHA256   string                          `json:"before_sha256"`
	AfterSHA256    string                          `json:"after_sha256"`
	AlreadyPatched bool                            `json:"already_patched"`
	Behavior       string                          `json:"behavior"`
	MetadataPath   string                          `json:"metadata_path,omitempty"`
	MetadataSHA256 string                          `json:"metadata_sha256,omitempty"`
	Sites          []AvatarForgeBoundarySiteResult `json:"sites"`
}

type avatarForgeBoundaryWrite struct {
	offset uint32
	bytes  []byte
}

// PatchStaticAvatarForgeBoundary fixes two source-verified calls where Client
// passes a 256-byte payload limit to QQTSection for a 256-byte local buffer.
// QQTSection appends a NUL at dst[length], so the old 256 limit can overwrite
// the adjacent loop count. A 255-byte payload retains the full C-string
// capacity and leaves all forge protocol and item filtering behavior intact.
func PatchStaticAvatarForgeBoundary(path string) (AvatarForgeBoundaryPatchResult, error) {
	data, image, result, err := loadAvatarForgeBoundary(path)
	if err != nil {
		return result, err
	}
	defer image.Close()

	writes, allPatched, err := inspectAvatarForgeBoundary(data, image, &result)
	if err != nil {
		return result, err
	}
	result.AlreadyPatched = allPatched
	if allPatched {
		result.AfterSHA256 = result.BeforeSHA256
		return result, nil
	}

	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return result, fmt.Errorf("open Client.exe for forge boundary patching: %w", err)
	}
	for _, write := range writes {
		if _, err := file.WriteAt(write.bytes, int64(write.offset)); err != nil {
			_ = file.Close()
			return result, fmt.Errorf("write Client.exe forge boundary at file offset 0x%X: %w", write.offset, err)
		}
		copy(data[write.offset:write.offset+uint32(len(write.bytes))], write.bytes)
	}
	if err := file.Close(); err != nil {
		return result, fmt.Errorf("close patched Client.exe: %w", err)
	}
	result.AfterSHA256 = sha256Hex(data)
	return result, nil
}

func CheckStaticAvatarForgeBoundary(path string) (AvatarForgeBoundaryPatchResult, error) {
	data, image, result, err := loadAvatarForgeBoundary(path)
	if err != nil {
		return result, err
	}
	defer image.Close()
	_, allPatched, err := inspectAvatarForgeBoundary(data, image, &result)
	if err != nil {
		return result, err
	}
	result.AlreadyPatched = allPatched
	result.AfterSHA256 = result.BeforeSHA256
	if !allPatched {
		return result, fmt.Errorf("Client.exe avatar-forge string boundaries are not fully patched")
	}
	return result, nil
}

func loadAvatarForgeBoundary(path string) ([]byte, *pe.File, AvatarForgeBoundaryPatchResult, error) {
	result := AvatarForgeBoundaryPatchResult{
		Path:     path,
		Behavior: "preserve all four forge categories and colors by limiting two 256-byte local C-string payloads to 255 bytes",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, result, fmt.Errorf("read Client.exe image: %w", err)
	}
	result.BeforeSHA256 = sha256Hex(data)
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, nil, result, fmt.Errorf("parse Client.exe PE: %w", err)
	}
	if image.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		image.Close()
		return nil, nil, result, fmt.Errorf("Client.exe machine 0x%X is not x86", image.FileHeader.Machine)
	}
	return data, image, result, nil
}

func inspectAvatarForgeBoundary(data []byte, image *pe.File, result *AvatarForgeBoundaryPatchResult) ([]avatarForgeBoundaryWrite, bool, error) {
	result.Sites = nil
	writes := make([]avatarForgeBoundaryWrite, 0, len(avatarForgeBoundarySites))
	allPatched := true
	for _, site := range avatarForgeBoundarySites {
		offset, err := peRVAFileOffset(image, "Client.exe", site.contextRVA, uint32(len(site.context)))
		if err != nil {
			return nil, false, err
		}
		current := data[offset : offset+uint32(len(site.context))]
		patched, alreadyPatched, err := patchAvatarForgeContext(site, current)
		if err != nil {
			return nil, false, err
		}
		instructionOffset := offset + site.rva - site.contextRVA
		result.Sites = append(result.Sites, AvatarForgeBoundarySiteResult{
			Name: site.name, RVA: fmt.Sprintf("0x%X", site.rva), FileOffset: fmt.Sprintf("0x%X", instructionOffset),
			OriginalBytes:  hex.EncodeToString(current[site.rva-site.contextRVA : site.rva-site.contextRVA+5]),
			PatchedBytes:   hex.EncodeToString(patched[site.rva-site.contextRVA : site.rva-site.contextRVA+5]),
			AlreadyPatched: alreadyPatched,
		})
		if !alreadyPatched {
			allPatched = false
			writes = append(writes, avatarForgeBoundaryWrite{
				offset: instructionOffset,
				bytes:  append([]byte(nil), patched[site.rva-site.contextRVA:site.rva-site.contextRVA+5]...),
			})
		}
	}
	return writes, allPatched, nil
}

func patchAvatarForgeContext(site avatarForgeBoundarySite, current []byte) ([]byte, bool, error) {
	patched := append([]byte(nil), site.context...)
	patchOffset := site.rva - site.contextRVA
	copy(patched[patchOffset:patchOffset+5], []byte{0x68, 0xFF, 0x00, 0x00, 0x00})
	switch {
	case bytes.Equal(current, patched):
		return patched, true, nil
	case bytes.Equal(current, site.context):
		return patched, false, nil
	default:
		return nil, false, fmt.Errorf("Client.exe %s signature mismatch at RVA 0x%X: got %X", site.name, site.contextRVA, current)
	}
}
