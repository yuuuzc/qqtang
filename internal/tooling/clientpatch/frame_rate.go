package clientpatch

//go:generate nasm -f bin ../../../scripts/asm/client-frame-rate.asm -o assets/client-frame-rate.bin

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
	// Client's primary frame limiter already reads [options] limitfps from
	// config/GameCFG.ini. This older rendering throttle independently waits for
	// a fixed 20 ms and therefore caps the configured limiter at about 50 FPS.
	legacyRenderThrottleContextRVA uint32 = 0x3220E
	legacyRenderThrottleCompareRVA uint32 = 0x32211
	legacyRenderThrottleTargetRVA  uint32 = 0x32221

	frameRateImageBase          uint32 = 0x00400000
	frameRateSectionRVA         uint32 = 0x00D37000
	frameRatePhantomHookRVA     uint32 = 0x0004B3DE
	frameRatePhantomEntryOffset uint32 = 0x100
	frameRateBananaCtorHookRVA  uint32 = 0x001DB4C7
	frameRateBananaCtorEntry    uint32 = 0x200
	frameRateBananaTickHookRVA  uint32 = 0x001DB52B
	frameRateBananaTickEntry    uint32 = 0x300
	frameRateSectionFlags       uint32 = 0xE0000060
)

var (
	//go:embed assets/client-frame-rate.bin
	frameRatePayload []byte

	frameRateMagic              = []byte("QQTFPS04")
	previousFrameRateMagics     = [][]byte{[]byte("QQTFPS01"), []byte("QQTFPS02"), []byte("QQTFPS03")}
	frameRatePhantomOriginal    = []byte{0x8B, 0x45, 0xFC, 0x8B, 0x08}
	frameRateBananaCtorOriginal = []byte{
		0xC7, 0x06, 0x94, 0x34, 0x7A, 0x00,
	}
	frameRateBananaTickOriginal = []byte{
		0x56, 0x8B, 0x71, 0x04, 0x85, 0xF6,
	}
	legacyRenderThrottleContext = []byte{
		0x83, 0x7D, 0xF8, 0x14, 0x7C, 0x0C,
		0xC7, 0x85, 0x20, 0xFE, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00,
		0xEB, 0x0E,
		0xB8, 0x14, 0x00, 0x00, 0x00,
		0x2B, 0x45, 0xF8,
		0x89, 0x85, 0x20, 0xFE, 0xFF, 0xFF,
	}
)

type FrameRatePatchSiteResult struct {
	Name           string `json:"name"`
	RVA            string `json:"rva"`
	FileOffset     string `json:"file_offset"`
	OriginalBytes  string `json:"original_bytes"`
	PatchedBytes   string `json:"patched_bytes"`
	AlreadyPatched bool   `json:"already_patched"`
}

type FrameRatePatchResult struct {
	Path           string                     `json:"path"`
	BeforeSHA256   string                     `json:"before_sha256"`
	AfterSHA256    string                     `json:"after_sha256"`
	AlreadyPatched bool                       `json:"already_patched"`
	Behavior       string                     `json:"behavior"`
	MetadataPath   string                     `json:"metadata_path,omitempty"`
	MetadataSHA256 string                     `json:"metadata_sha256,omitempty"`
	SectionRVA     string                     `json:"section_rva"`
	SectionSize    uint32                     `json:"section_size"`
	Sites          []FrameRatePatchSiteResult `json:"sites"`
}

type frameRatePELayout struct {
	peOffset         uint32
	optionalOffset   uint32
	sectionTable     uint32
	numberOfSections uint16
	sectionAlignment uint32
	fileAlignment    uint32
	sizeOfHeaders    uint32
	lastVirtualEnd   uint32
	lastRawEnd       uint32
	sectionHeader    uint32
	sectionRVA       uint32
	sectionRaw       uint32
	sectionRawSize   uint32
}

// PatchStaticFrameRate removes the independent fixed 20 ms rendering wait and
// restores the native phantom trail's original time scale. It also makes the
// banana forced-slide status independent of render frequency by sampling its
// native integer-position stop test at the legacy ~30 Hz logic cadence for
// the entire slide. Client's original configurable limiter remains intact and
// continues to read [options] limitfps, so one native setting remains
// authoritative.
func PatchStaticFrameRate(path string) (FrameRatePatchResult, error) {
	data, image, layout, result, err := loadFrameRateImage(path)
	if err != nil {
		return result, err
	}
	defer image.Close()

	throttleOffset, throttlePatch, throttlePatched, err := inspectFrameRateThrottle(data, image, &result)
	if err != nil {
		return result, err
	}
	phantomOffset, phantomPatch, phantomPatched, err := inspectFrameRatePhantomHook(data, image, &result)
	if err != nil {
		return result, err
	}
	bananaCtorOffset, bananaCtorPatch, bananaCtorPatched, err := inspectFrameRateJumpHook(
		data, image, &result, "banana-forced-slide-constructor", frameRateBananaCtorHookRVA,
		frameRateBananaCtorEntry, frameRateBananaCtorOriginal,
	)
	if err != nil {
		return result, err
	}
	bananaTickOffset, bananaTickPatch, bananaTickPatched, err := inspectFrameRateJumpHook(
		data, image, &result, "banana-forced-slide-position-sampling", frameRateBananaTickHookRVA,
		frameRateBananaTickEntry, frameRateBananaTickOriginal,
	)
	if err != nil {
		return result, err
	}
	payloadPatched := false
	if layout.sectionHeader != 0 {
		payloadPatched, err = inspectFrameRateSection(data, layout)
		if err != nil {
			return result, err
		}
	} else {
		if phantomPatched || bananaCtorPatched || bananaTickPatched {
			return result, fmt.Errorf("Client.exe timing hook targets .qqfps but the section is absent")
		}
		data, err = appendFrameRateSection(data, layout)
		if err != nil {
			return result, err
		}
	}
	result.AlreadyPatched = throttlePatched && phantomPatched && bananaCtorPatched && bananaTickPatched && payloadPatched
	if result.AlreadyPatched {
		result.AfterSHA256 = result.BeforeSHA256
		return result, nil
	}
	if !throttlePatched {
		copy(data[throttleOffset:throttleOffset+uint32(len(throttlePatch))], throttlePatch)
	}
	if !phantomPatched {
		copy(data[phantomOffset:phantomOffset+uint32(len(phantomPatch))], phantomPatch)
	}
	if !bananaCtorPatched {
		copy(data[bananaCtorOffset:bananaCtorOffset+uint32(len(bananaCtorPatch))], bananaCtorPatch)
	}
	if !bananaTickPatched {
		copy(data[bananaTickOffset:bananaTickOffset+uint32(len(bananaTickPatch))], bananaTickPatch)
	}
	if layout.sectionHeader != 0 && !payloadPatched {
		copy(data[layout.sectionRaw:layout.sectionRaw+uint32(len(frameRatePayload))], frameRatePayload)
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		return result, fmt.Errorf("write frame-rate patched Client.exe: %w", err)
	}
	result.AfterSHA256 = sha256Hex(data)
	return result, nil
}

func CheckStaticFrameRate(path string) (FrameRatePatchResult, error) {
	data, image, layout, result, err := loadFrameRateImage(path)
	if err != nil {
		return result, err
	}
	defer image.Close()
	_, _, bananaCtorPatched, err := inspectFrameRateJumpHook(
		data, image, &result, "banana-forced-slide-constructor", frameRateBananaCtorHookRVA,
		frameRateBananaCtorEntry, frameRateBananaCtorOriginal,
	)
	if err != nil {
		return result, err
	}
	_, _, bananaTickPatched, err := inspectFrameRateJumpHook(
		data, image, &result, "banana-forced-slide-position-sampling", frameRateBananaTickHookRVA,
		frameRateBananaTickEntry, frameRateBananaTickOriginal,
	)
	if err != nil {
		return result, err
	}
	_, _, throttlePatched, err := inspectFrameRateThrottle(data, image, &result)
	if err != nil {
		return result, err
	}
	_, _, phantomPatched, err := inspectFrameRatePhantomHook(data, image, &result)
	if err != nil {
		return result, err
	}
	if layout.sectionHeader == 0 {
		return result, fmt.Errorf("Client.exe has no .qqfps timing section")
	}
	if err := verifyFrameRateSection(data, layout); err != nil {
		return result, err
	}
	result.AlreadyPatched = throttlePatched && phantomPatched && bananaCtorPatched && bananaTickPatched
	result.AfterSHA256 = result.BeforeSHA256
	if !result.AlreadyPatched {
		return result, fmt.Errorf("Client.exe frame-rate timing patch is incomplete")
	}
	return result, nil
}

func loadFrameRateImage(path string) ([]byte, *pe.File, frameRatePELayout, FrameRatePatchResult, error) {
	result := FrameRatePatchResult{
		Path:        path,
		Behavior:    "retain native [options] limitfps, remove the conflicting fixed 20 ms wait, sample phantom pose history every 33 ms, and sample banana forced-slide stop detection every 33 ms",
		SectionRVA:  fmt.Sprintf("0x%X", frameRateSectionRVA),
		SectionSize: uint32(len(frameRatePayload)),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, frameRatePELayout{}, result, fmt.Errorf("read Client.exe image: %w", err)
	}
	result.BeforeSHA256 = sha256Hex(data)
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, nil, frameRatePELayout{}, result, fmt.Errorf("parse Client.exe PE: %w", err)
	}
	if image.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		image.Close()
		return nil, nil, frameRatePELayout{}, result, fmt.Errorf("Client.exe machine 0x%X is not x86", image.FileHeader.Machine)
	}
	layout, err := parseFrameRatePELayout(data)
	if err != nil {
		image.Close()
		return nil, nil, frameRatePELayout{}, result, err
	}
	return data, image, layout, result, nil
}

func inspectFrameRateThrottle(data []byte, image *pe.File, result *FrameRatePatchResult) (uint32, []byte, bool, error) {
	offset, err := peRVAFileOffset(image, "Client.exe", legacyRenderThrottleContextRVA, uint32(len(legacyRenderThrottleContext)))
	if err != nil {
		return 0, nil, false, err
	}
	current := data[offset : offset+uint32(len(legacyRenderThrottleContext))]
	patched := patchedFrameRateContext()
	var alreadyPatched bool
	switch {
	case bytes.Equal(current, patched):
		alreadyPatched = true
	case bytes.Equal(current, legacyRenderThrottleContext), bytes.Equal(current, previousFrameRateContext()):
		alreadyPatched = false
	default:
		return 0, nil, false, fmt.Errorf("Client.exe legacy render-throttle signature mismatch at RVA 0x%X: got %X", legacyRenderThrottleContextRVA, current)
	}
	result.Sites = append(result.Sites,
		FrameRatePatchSiteResult{
			Name: "legacy-render-throttle-compare", RVA: fmt.Sprintf("0x%X", legacyRenderThrottleCompareRVA),
			FileOffset:    fmt.Sprintf("0x%X", offset+legacyRenderThrottleCompareRVA-legacyRenderThrottleContextRVA),
			OriginalBytes: "14", PatchedBytes: "00", AlreadyPatched: alreadyPatched,
		},
		FrameRatePatchSiteResult{
			Name: "legacy-render-throttle-target", RVA: fmt.Sprintf("0x%X", legacyRenderThrottleTargetRVA),
			FileOffset:    fmt.Sprintf("0x%X", offset+legacyRenderThrottleTargetRVA-legacyRenderThrottleContextRVA),
			OriginalBytes: "14000000", PatchedBytes: "00000000", AlreadyPatched: alreadyPatched,
		},
	)
	return offset, patched, alreadyPatched, nil
}

func inspectFrameRatePhantomHook(data []byte, image *pe.File, result *FrameRatePatchResult) (uint32, []byte, bool, error) {
	offset, err := peRVAFileOffset(image, "Client.exe", frameRatePhantomHookRVA, uint32(len(frameRatePhantomOriginal)))
	if err != nil {
		return 0, nil, false, err
	}
	patched := make([]byte, len(frameRatePhantomOriginal))
	patched[0] = 0xE9
	target := frameRateImageBase + frameRateSectionRVA + frameRatePhantomEntryOffset
	relative := int64(target) - int64(frameRateImageBase+frameRatePhantomHookRVA+5)
	binary.LittleEndian.PutUint32(patched[1:5], uint32(int32(relative)))
	current := data[offset : offset+uint32(len(frameRatePhantomOriginal))]
	alreadyPatched := bytes.Equal(current, patched)
	if !alreadyPatched && !bytes.Equal(current, frameRatePhantomOriginal) {
		return 0, nil, false, fmt.Errorf("Client.exe phantom-history signature mismatch at RVA 0x%X: got %X", frameRatePhantomHookRVA, current)
	}
	result.Sites = append(result.Sites, FrameRatePatchSiteResult{
		Name: "phantom-history-time-sampling", RVA: fmt.Sprintf("0x%X", frameRatePhantomHookRVA),
		FileOffset: fmt.Sprintf("0x%X", offset), OriginalBytes: hex.EncodeToString(frameRatePhantomOriginal),
		PatchedBytes: hex.EncodeToString(patched), AlreadyPatched: alreadyPatched,
	})
	return offset, patched, alreadyPatched, nil
}

func inspectFrameRateJumpHook(
	data []byte,
	image *pe.File,
	result *FrameRatePatchResult,
	name string,
	hookRVA uint32,
	entryOffset uint32,
	original []byte,
) (uint32, []byte, bool, error) {
	if len(original) < 5 {
		return 0, nil, false, fmt.Errorf("%s original signature is shorter than a near jump", name)
	}
	offset, err := peRVAFileOffset(image, "Client.exe", hookRVA, uint32(len(original)))
	if err != nil {
		return 0, nil, false, err
	}
	patched := bytes.Repeat([]byte{0x90}, len(original))
	patched[0] = 0xE9
	target := frameRateImageBase + frameRateSectionRVA + entryOffset
	relative := int64(target) - int64(frameRateImageBase+hookRVA+5)
	binary.LittleEndian.PutUint32(patched[1:5], uint32(int32(relative)))
	current := data[offset : offset+uint32(len(original))]
	alreadyPatched := bytes.Equal(current, patched)
	if !alreadyPatched && !bytes.Equal(current, original) {
		return 0, nil, false, fmt.Errorf("Client.exe %s signature mismatch at RVA 0x%X: got %X", name, hookRVA, current)
	}
	result.Sites = append(result.Sites, FrameRatePatchSiteResult{
		Name: name, RVA: fmt.Sprintf("0x%X", hookRVA), FileOffset: fmt.Sprintf("0x%X", offset),
		OriginalBytes: hex.EncodeToString(original), PatchedBytes: hex.EncodeToString(patched),
		AlreadyPatched: alreadyPatched,
	})
	return offset, patched, alreadyPatched, nil
}

func parseFrameRatePELayout(data []byte) (frameRatePELayout, error) {
	if len(data) < 0x100 || string(data[:2]) != "MZ" {
		return frameRatePELayout{}, fmt.Errorf("Client.exe has no DOS header")
	}
	peOffset := binary.LittleEndian.Uint32(data[0x3C:0x40])
	if peOffset > uint32(len(data))-24 || string(data[peOffset:peOffset+4]) != "PE\x00\x00" {
		return frameRatePELayout{}, fmt.Errorf("Client.exe has no valid PE signature")
	}
	numberOfSections := binary.LittleEndian.Uint16(data[peOffset+6 : peOffset+8])
	optionalSize := binary.LittleEndian.Uint16(data[peOffset+20 : peOffset+22])
	optionalOffset := peOffset + 24
	if optionalOffset+uint32(optionalSize) > uint32(len(data)) || optionalSize < 68 {
		return frameRatePELayout{}, fmt.Errorf("Client.exe optional header is truncated")
	}
	if binary.LittleEndian.Uint16(data[optionalOffset:optionalOffset+2]) != 0x10B {
		return frameRatePELayout{}, fmt.Errorf("Client.exe optional header is not PE32")
	}
	if binary.LittleEndian.Uint32(data[optionalOffset+28:optionalOffset+32]) != frameRateImageBase {
		return frameRatePELayout{}, fmt.Errorf("Client.exe image base is not 0x%X", frameRateImageBase)
	}
	sectionAlignment := binary.LittleEndian.Uint32(data[optionalOffset+32 : optionalOffset+36])
	fileAlignment := binary.LittleEndian.Uint32(data[optionalOffset+36 : optionalOffset+40])
	sizeOfHeaders := binary.LittleEndian.Uint32(data[optionalOffset+60 : optionalOffset+64])
	sectionTable := optionalOffset + uint32(optionalSize)
	if sectionAlignment == 0 || fileAlignment == 0 || sectionTable+uint32(numberOfSections)*40 > uint32(len(data)) {
		return frameRatePELayout{}, fmt.Errorf("Client.exe section layout is invalid")
	}
	layout := frameRatePELayout{
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
		if name == ".qqfps" {
			layout.sectionHeader, layout.sectionRVA = header, virtualAddress
			layout.sectionRaw, layout.sectionRawSize = rawOffset, rawSize
		}
	}
	return layout, nil
}

func appendFrameRateSection(data []byte, layout frameRatePELayout) ([]byte, error) {
	if len(frameRatePayload) != 0x1000 || !bytes.Equal(frameRatePayload[:len(frameRateMagic)], frameRateMagic) {
		return nil, fmt.Errorf("embedded .qqfps payload is invalid")
	}
	newHeader := layout.sectionTable + uint32(layout.numberOfSections)*40
	if newHeader+40 > layout.sizeOfHeaders || newHeader+40 > uint32(len(data)) || !allZero(data[newHeader:newHeader+40]) {
		return nil, fmt.Errorf("Client.exe has no empty section-header space for .qqfps")
	}
	newRVA := alignUint32(layout.lastVirtualEnd, layout.sectionAlignment)
	newRaw := alignUint32(layout.lastRawEnd, layout.fileAlignment)
	if newRVA != frameRateSectionRVA {
		return nil, fmt.Errorf("Client.exe next section RVA 0x%X, want .qqfps RVA 0x%X; apply the platform-effect patch first", newRVA, frameRateSectionRVA)
	}
	if uint32(len(data)) != newRaw {
		return nil, fmt.Errorf("Client.exe has an unsupported overlay at 0x%X (file size 0x%X)", newRaw, len(data))
	}
	rawSize := alignUint32(uint32(len(frameRatePayload)), layout.fileAlignment)
	data = append(data, make([]byte, rawSize)...)
	copy(data[newRaw:newRaw+uint32(len(frameRatePayload))], frameRatePayload)
	header := data[newHeader : newHeader+40]
	copy(header[:8], []byte(".qqfps"))
	binary.LittleEndian.PutUint32(header[8:12], uint32(len(frameRatePayload)))
	binary.LittleEndian.PutUint32(header[12:16], newRVA)
	binary.LittleEndian.PutUint32(header[16:20], rawSize)
	binary.LittleEndian.PutUint32(header[20:24], newRaw)
	binary.LittleEndian.PutUint32(header[36:40], frameRateSectionFlags)
	binary.LittleEndian.PutUint16(data[layout.peOffset+6:layout.peOffset+8], layout.numberOfSections+1)
	sizeOfImage := alignUint32(newRVA+uint32(len(frameRatePayload)), layout.sectionAlignment)
	binary.LittleEndian.PutUint32(data[layout.optionalOffset+56:layout.optionalOffset+60], sizeOfImage)
	binary.LittleEndian.PutUint32(data[layout.optionalOffset+64:layout.optionalOffset+68], 0)
	return data, nil
}

func inspectFrameRateSection(data []byte, layout frameRatePELayout) (bool, error) {
	if layout.sectionRVA != frameRateSectionRVA || layout.sectionRawSize < uint32(len(frameRatePayload)) {
		return false, fmt.Errorf("Client.exe .qqfps layout is invalid")
	}
	end := layout.sectionRaw + uint32(len(frameRatePayload))
	if end > uint32(len(data)) {
		return false, fmt.Errorf("Client.exe .qqfps payload is truncated")
	}
	current := data[layout.sectionRaw:end]
	if bytes.Equal(current, frameRatePayload) {
		return true, nil
	}
	for _, previousMagic := range previousFrameRateMagics {
		if len(current) >= len(previousMagic) && bytes.Equal(current[:len(previousMagic)], previousMagic) {
			return false, nil
		}
	}
	return false, fmt.Errorf("Client.exe .qqfps payload does not match a supported timing adapter")
}

func verifyFrameRateSection(data []byte, layout frameRatePELayout) error {
	current, err := inspectFrameRateSection(data, layout)
	if err != nil {
		return err
	}
	if !current {
		return fmt.Errorf("Client.exe .qqfps timing adapter requires an upgrade")
	}
	return nil
}

func patchedFrameRateContext() []byte {
	patched := append([]byte(nil), legacyRenderThrottleContext...)
	patched[legacyRenderThrottleCompareRVA-legacyRenderThrottleContextRVA] = 0
	copy(patched[legacyRenderThrottleTargetRVA-legacyRenderThrottleContextRVA:], []byte{0, 0, 0, 0})
	return patched
}

func previousFrameRateContext() []byte {
	patched := append([]byte(nil), legacyRenderThrottleContext...)
	patched[legacyRenderThrottleCompareRVA-legacyRenderThrottleContextRVA] = 1
	copy(patched[legacyRenderThrottleTargetRVA-legacyRenderThrottleContextRVA:], []byte{1, 0, 0, 0})
	return patched
}

func frameRatePatchHex() string {
	return hex.EncodeToString(patchedFrameRateContext())
}
