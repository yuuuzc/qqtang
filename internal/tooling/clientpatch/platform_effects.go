package clientpatch

//go:generate nasm -f bin ../../../scripts/asm/client-platform-effects.asm -o assets/client-platform-effects.bin

import (
	"bytes"
	"debug/pe"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
)

const (
	platformEffectImageBase                 = uint32(0x00400000)
	platformEffectSectionRVA                = uint32(0x00D36000)
	platformEffectKickHookRVA               = uint32(0x002007A7)
	platformEffectBossBubbleHookRVA         = uint32(0x00208573)
	platformEffectLocalActionHookRVA        = uint32(0x00219483)
	platformEffectLocalMultiBubbleHookRVA   = uint32(0x001FFE86)
	platformEffectRemoteMultiBubbleHookRVA  = uint32(0x0020085C)
	platformEffectLocalMoveBombHookRVA      = uint32(0x0020D1A6)
	platformEffectRemoteMoveBombHookRVA     = uint32(0x001FD24C)
	platformEffectUpdateHookRVA             = uint32(0x001D2F96)
	platformEffectDrawHookRVA               = uint32(0x001D3017)
	platformEffectKickEntryOffset           = uint32(0x100)
	platformEffectBossEntryOffset           = uint32(0x600)
	platformEffectLocalEntryOffset          = uint32(0x680)
	platformEffectUpdateEntryOffset         = uint32(0x800)
	platformEffectDrawEntryOffset           = uint32(0x880)
	platformEffectLocalMultiBubbleOffset    = uint32(0xA00)
	platformEffectRemoteMultiBubbleOffset   = uint32(0xA80)
	platformEffectLocalMoveBombEntryOffset  = uint32(0xB00)
	platformEffectRemoteMoveBombEntryOffset = uint32(0xB80)
	platformEffectSectionFlags              = uint32(0xE0000060)
)

var (
	//go:embed assets/client-platform-effects.bin
	platformEffectPayload []byte

	platformEffectMagic                  = []byte("QQTFX010")
	platformEffectKickOriginal           = []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x14}
	platformEffectBossOriginal           = []byte{0x8B, 0x4F, 0x04, 0x8B, 0x83, 0x00, 0x04, 0x00, 0x00}
	platformEffectLocalOriginal          = []byte{0x8D, 0x45, 0xB4, 0xBF, 0x59, 0x11, 0x00, 0x00}
	platformEffectLocalMultiOriginal     = []byte{0xBE, 0x59, 0x11, 0x00, 0x00}
	platformEffectRemoteMultiOriginal    = []byte{0x8B, 0x83, 0x00, 0x04, 0x00, 0x00}
	platformEffectLocalMoveBombOriginal  = []byte{0x8D, 0x45, 0xC8, 0xBF, 0xB4, 0x0F, 0x00, 0x00}
	platformEffectRemoteMoveBombOriginal = []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x14}
	platformEffectUpdateOriginal         = []byte{0x53, 0x56, 0x8B, 0xF1, 0x57, 0x8B, 0x5E, 0x08}
	platformEffectDrawOriginal           = []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x18}
)

type PlatformEffectHookResult struct {
	Name           string `json:"name"`
	RVA            string `json:"rva"`
	FileOffset     string `json:"file_offset"`
	TargetVA       string `json:"target_va"`
	OriginalBytes  string `json:"original_bytes"`
	PatchedBytes   string `json:"patched_bytes"`
	AlreadyPatched bool   `json:"already_patched"`
}

type PlatformEffectPatchResult struct {
	Path           string                     `json:"path"`
	BeforeSHA256   string                     `json:"before_sha256"`
	AfterSHA256    string                     `json:"after_sha256"`
	AlreadyPatched bool                       `json:"already_patched"`
	Behavior       string                     `json:"behavior"`
	SectionRVA     string                     `json:"section_rva"`
	SectionSize    uint32                     `json:"section_size"`
	MetadataPath   string                     `json:"metadata_path,omitempty"`
	MetadataSHA256 string                     `json:"metadata_sha256,omitempty"`
	Hooks          []PlatformEffectHookResult `json:"hooks"`
}

type platformEffectPELayout struct {
	peOffset         uint32
	optionalOffset   uint32
	sectionTable     uint32
	numberOfSections uint16
	sectionAlignment uint32
	fileAlignment    uint32
	sizeOfHeaders    uint32
	lastVirtualEnd   uint32
	lastRawEnd       uint32
	qqfxHeader       uint32
	qqfxRVA          uint32
	qqfxRaw          uint32
	qqfxRawSize      uint32
}

// PatchStaticPlatformEffects restores three passive function-item visuals
// whose item records and effect assets survive in 5.2 but whose action
// consumer does not. Match-start ownership is cached from reserved 0x1159
// records. Mechanical lightning renders on the native throw action; football
// visuals render on the real 0x0FB4/0x139C contact-kick pair. It also mirrors
// the native local pirate-Boss bubble appearance branch in the remote bubble
// consumer.
func PatchStaticPlatformEffects(path string) (PlatformEffectPatchResult, error) {
	data, image, layout, result, err := loadPlatformEffectImage(path)
	if err != nil {
		return result, err
	}
	defer image.Close()

	patched, alreadyPatched, err := inspectPlatformEffectHooks(data, image, &result)
	if err != nil {
		return result, err
	}
	if layout.qqfxHeader != 0 {
		if err := verifyPlatformEffectSection(data, layout); err != nil {
			return result, err
		}
		if !alreadyPatched {
			return result, fmt.Errorf("Client.exe has .qqfx but its action hooks are not fully patched")
		}
		result.AlreadyPatched = true
		result.AfterSHA256 = result.BeforeSHA256
		return result, nil
	}
	if alreadyPatched {
		return result, fmt.Errorf("Client.exe action hooks target .qqfx but the section is absent")
	}

	data, err = appendPlatformEffectSection(data, layout)
	if err != nil {
		return result, err
	}
	for _, write := range patched {
		copy(data[write.offset:write.offset+uint32(len(write.bytes))], write.bytes)
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		return result, fmt.Errorf("write platform-effect patched Client.exe: %w", err)
	}
	result.AfterSHA256 = sha256Hex(data)
	return result, nil
}

func CheckStaticPlatformEffects(path string) (PlatformEffectPatchResult, error) {
	data, image, layout, result, err := loadPlatformEffectImage(path)
	if err != nil {
		return result, err
	}
	defer image.Close()
	_, allPatched, err := inspectPlatformEffectHooks(data, image, &result)
	if err != nil {
		return result, err
	}
	if !allPatched || layout.qqfxHeader == 0 {
		return result, fmt.Errorf("Client.exe passive platform-effect adapter is not fully patched")
	}
	if err := verifyPlatformEffectSection(data, layout); err != nil {
		return result, err
	}
	result.AlreadyPatched = true
	result.AfterSHA256 = result.BeforeSHA256
	return result, nil
}

func loadPlatformEffectImage(path string) ([]byte, *pe.File, platformEffectPELayout, PlatformEffectPatchResult, error) {
	result := PlatformEffectPatchResult{
		Path:        path,
		Behavior:    "cache passive function-item ownership at match start, render local and remote native actions, and mirror RoleID 33/34 fire bubbles for remote observers",
		SectionRVA:  fmt.Sprintf("0x%X", platformEffectSectionRVA),
		SectionSize: uint32(len(platformEffectPayload)),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, platformEffectPELayout{}, result, fmt.Errorf("read Client.exe image: %w", err)
	}
	result.BeforeSHA256 = sha256Hex(data)
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, nil, platformEffectPELayout{}, result, fmt.Errorf("parse Client.exe PE: %w", err)
	}
	if image.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		image.Close()
		return nil, nil, platformEffectPELayout{}, result, fmt.Errorf("Client.exe machine 0x%X is not x86", image.FileHeader.Machine)
	}
	layout, err := parsePlatformEffectPELayout(data)
	if err != nil {
		image.Close()
		return nil, nil, platformEffectPELayout{}, result, err
	}
	return data, image, layout, result, nil
}

func parsePlatformEffectPELayout(data []byte) (platformEffectPELayout, error) {
	if len(data) < 0x100 || string(data[:2]) != "MZ" {
		return platformEffectPELayout{}, fmt.Errorf("Client.exe has no DOS header")
	}
	peOffset := binary.LittleEndian.Uint32(data[0x3C:0x40])
	if peOffset > uint32(len(data))-24 || string(data[peOffset:peOffset+4]) != "PE\x00\x00" {
		return platformEffectPELayout{}, fmt.Errorf("Client.exe has no valid PE signature")
	}
	numberOfSections := binary.LittleEndian.Uint16(data[peOffset+6 : peOffset+8])
	optionalSize := binary.LittleEndian.Uint16(data[peOffset+20 : peOffset+22])
	optionalOffset := peOffset + 24
	if optionalOffset+uint32(optionalSize) > uint32(len(data)) || optionalSize < 68 {
		return platformEffectPELayout{}, fmt.Errorf("Client.exe optional header is truncated")
	}
	if binary.LittleEndian.Uint16(data[optionalOffset:optionalOffset+2]) != 0x10B {
		return platformEffectPELayout{}, fmt.Errorf("Client.exe optional header is not PE32")
	}
	if binary.LittleEndian.Uint32(data[optionalOffset+28:optionalOffset+32]) != platformEffectImageBase {
		return platformEffectPELayout{}, fmt.Errorf("Client.exe image base is not 0x%X", platformEffectImageBase)
	}
	sectionAlignment := binary.LittleEndian.Uint32(data[optionalOffset+32 : optionalOffset+36])
	fileAlignment := binary.LittleEndian.Uint32(data[optionalOffset+36 : optionalOffset+40])
	sizeOfHeaders := binary.LittleEndian.Uint32(data[optionalOffset+60 : optionalOffset+64])
	if sectionAlignment == 0 || fileAlignment == 0 {
		return platformEffectPELayout{}, fmt.Errorf("Client.exe section/file alignment is zero")
	}
	sectionTable := optionalOffset + uint32(optionalSize)
	if sectionTable+uint32(numberOfSections)*40 > uint32(len(data)) {
		return platformEffectPELayout{}, fmt.Errorf("Client.exe section table is truncated")
	}
	layout := platformEffectPELayout{
		peOffset: peOffset, optionalOffset: optionalOffset, sectionTable: sectionTable,
		numberOfSections: numberOfSections, sectionAlignment: sectionAlignment,
		fileAlignment: fileAlignment, sizeOfHeaders: sizeOfHeaders,
	}
	for index := uint16(0); index < numberOfSections; index++ {
		header := sectionTable + uint32(index)*40
		name := string(bytes.TrimRight(data[header:header+8], "\x00"))
		virtualSize := binary.LittleEndian.Uint32(data[header+8 : header+12])
		virtualAddress := binary.LittleEndian.Uint32(data[header+12 : header+16])
		rawSize := binary.LittleEndian.Uint32(data[header+16 : header+20])
		rawOffset := binary.LittleEndian.Uint32(data[header+20 : header+24])
		if end := virtualAddress + maxUint32(virtualSize, rawSize); end > layout.lastVirtualEnd {
			layout.lastVirtualEnd = end
		}
		if end := rawOffset + rawSize; end > layout.lastRawEnd {
			layout.lastRawEnd = end
		}
		if name == ".qqfx" {
			layout.qqfxHeader, layout.qqfxRVA = header, virtualAddress
			layout.qqfxRaw, layout.qqfxRawSize = rawOffset, rawSize
		}
	}
	return layout, nil
}

type platformEffectWrite struct {
	offset uint32
	bytes  []byte
}

func inspectPlatformEffectHooks(data []byte, image *pe.File, result *PlatformEffectPatchResult) ([]platformEffectWrite, bool, error) {
	type hook struct {
		name     string
		rva      uint32
		original []byte
		target   uint32
	}
	hooks := []hook{
		{name: "remote-passive-action-visual", rva: platformEffectKickHookRVA, original: platformEffectKickOriginal, target: platformEffectImageBase + platformEffectSectionRVA + platformEffectKickEntryOffset},
		{name: "pirate-boss-remote-bubble-visual", rva: platformEffectBossBubbleHookRVA, original: platformEffectBossOriginal, target: platformEffectImageBase + platformEffectSectionRVA + platformEffectBossEntryOffset},
		{name: "local-passive-action-visual", rva: platformEffectLocalActionHookRVA, original: platformEffectLocalOriginal, target: platformEffectImageBase + platformEffectSectionRVA + platformEffectLocalEntryOffset},
		{name: "local-mechanical-multi-bubble-visual", rva: platformEffectLocalMultiBubbleHookRVA, original: platformEffectLocalMultiOriginal, target: platformEffectImageBase + platformEffectSectionRVA + platformEffectLocalMultiBubbleOffset},
		{name: "remote-mechanical-multi-bubble-visual", rva: platformEffectRemoteMultiBubbleHookRVA, original: platformEffectRemoteMultiOriginal, target: platformEffectImageBase + platformEffectSectionRVA + platformEffectRemoteMultiBubbleOffset},
		{name: "local-move-bomb-passive-action-visual", rva: platformEffectLocalMoveBombHookRVA, original: platformEffectLocalMoveBombOriginal, target: platformEffectImageBase + platformEffectSectionRVA + platformEffectLocalMoveBombEntryOffset},
		{name: "remote-move-bomb-passive-action-visual", rva: platformEffectRemoteMoveBombHookRVA, original: platformEffectRemoteMoveBombOriginal, target: platformEffectImageBase + platformEffectSectionRVA + platformEffectRemoteMoveBombEntryOffset},
		{name: "passive-effect-frame-update", rva: platformEffectUpdateHookRVA, original: platformEffectUpdateOriginal, target: platformEffectImageBase + platformEffectSectionRVA + platformEffectUpdateEntryOffset},
		{name: "passive-effect-frame-draw", rva: platformEffectDrawHookRVA, original: platformEffectDrawOriginal, target: platformEffectImageBase + platformEffectSectionRVA + platformEffectDrawEntryOffset},
	}
	result.Hooks = nil
	writes := make([]platformEffectWrite, 0, len(hooks))
	allPatched := true
	for _, hook := range hooks {
		offset, err := peRVAFileOffset(image, "Client.exe", hook.rva, uint32(len(hook.original)))
		if err != nil {
			return nil, false, err
		}
		patched := make([]byte, len(hook.original))
		patched[0] = 0xE9
		relative := int64(hook.target) - int64(platformEffectImageBase+hook.rva+5)
		binary.LittleEndian.PutUint32(patched[1:5], uint32(int32(relative)))
		for index := 5; index < len(patched); index++ {
			patched[index] = 0x90
		}
		current := data[offset : offset+uint32(len(hook.original))]
		already := bytes.Equal(current, patched)
		if !already && !bytes.Equal(current, hook.original) {
			return nil, false, fmt.Errorf("Client.exe %s signature mismatch at RVA 0x%X: got %X", hook.name, hook.rva, current)
		}
		result.Hooks = append(result.Hooks, PlatformEffectHookResult{
			Name: hook.name, RVA: fmt.Sprintf("0x%X", hook.rva), FileOffset: fmt.Sprintf("0x%X", offset),
			TargetVA: fmt.Sprintf("0x%X", hook.target), OriginalBytes: hex.EncodeToString(hook.original),
			PatchedBytes: hex.EncodeToString(patched), AlreadyPatched: already,
		})
		if !already {
			allPatched = false
			writes = append(writes, platformEffectWrite{offset: offset, bytes: patched})
		}
	}
	return writes, allPatched, nil
}

func appendPlatformEffectSection(data []byte, layout platformEffectPELayout) ([]byte, error) {
	if len(platformEffectPayload) != 0x1000 || !bytes.Equal(platformEffectPayload[:len(platformEffectMagic)], platformEffectMagic) {
		return nil, fmt.Errorf("embedded .qqfx payload is invalid")
	}
	newHeader := layout.sectionTable + uint32(layout.numberOfSections)*40
	if newHeader+40 > layout.sizeOfHeaders || newHeader+40 > uint32(len(data)) {
		return nil, fmt.Errorf("Client.exe has no section-header space for .qqfx")
	}
	if !allZero(data[newHeader : newHeader+40]) {
		return nil, fmt.Errorf("Client.exe next section header is not empty")
	}
	newRVA := alignUint32(layout.lastVirtualEnd, layout.sectionAlignment)
	newRaw := alignUint32(layout.lastRawEnd, layout.fileAlignment)
	if newRVA != platformEffectSectionRVA {
		return nil, fmt.Errorf("Client.exe next section RVA 0x%X, want fixed adapter RVA 0x%X", newRVA, platformEffectSectionRVA)
	}
	if uint32(len(data)) != newRaw {
		return nil, fmt.Errorf("Client.exe has an unsupported overlay at 0x%X (file size 0x%X)", newRaw, len(data))
	}
	rawSize := alignUint32(uint32(len(platformEffectPayload)), layout.fileAlignment)
	data = append(data, make([]byte, rawSize)...)
	copy(data[newRaw:newRaw+uint32(len(platformEffectPayload))], platformEffectPayload)
	header := data[newHeader : newHeader+40]
	copy(header[:8], []byte(".qqfx"))
	binary.LittleEndian.PutUint32(header[8:12], uint32(len(platformEffectPayload)))
	binary.LittleEndian.PutUint32(header[12:16], newRVA)
	binary.LittleEndian.PutUint32(header[16:20], rawSize)
	binary.LittleEndian.PutUint32(header[20:24], newRaw)
	binary.LittleEndian.PutUint32(header[36:40], platformEffectSectionFlags)
	binary.LittleEndian.PutUint16(data[layout.peOffset+6:layout.peOffset+8], layout.numberOfSections+1)
	sizeOfImage := alignUint32(newRVA+uint32(len(platformEffectPayload)), layout.sectionAlignment)
	binary.LittleEndian.PutUint32(data[layout.optionalOffset+56:layout.optionalOffset+60], sizeOfImage)
	// The prepared image is unsigned and the loader does not require a PE
	// checksum.  Clear the stale value rather than retaining a checksum for the
	// pre-section image.
	binary.LittleEndian.PutUint32(data[layout.optionalOffset+64:layout.optionalOffset+68], 0)
	return data, nil
}

func verifyPlatformEffectSection(data []byte, layout platformEffectPELayout) error {
	if layout.qqfxRVA != platformEffectSectionRVA || layout.qqfxRawSize < uint32(len(platformEffectPayload)) {
		return fmt.Errorf("Client.exe .qqfx layout is invalid")
	}
	end := layout.qqfxRaw + uint32(len(platformEffectPayload))
	if end > uint32(len(data)) || !bytes.Equal(data[layout.qqfxRaw:end], platformEffectPayload) {
		return fmt.Errorf("Client.exe .qqfx payload does not match the verified adapter")
	}
	return nil
}

func alignUint32(value, alignment uint32) uint32 {
	return (value + alignment - 1) &^ (alignment - 1)
}

func maxUint32(first, second uint32) uint32 {
	if first > second {
		return first
	}
	return second
}

func allZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}
