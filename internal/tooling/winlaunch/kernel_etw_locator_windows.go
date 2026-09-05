package winlaunch

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const imageSectionMemExecute = uint32(0x20000000)

type remoteExecutableSection struct {
	name            string
	virtualAddress  uint32
	virtualSize     uint32
	characteristics uint32
	data            []byte
}

type remoteExecutableImage struct {
	base     uintptr
	size     uint32
	path     string
	version  string
	sections []remoteExecutableSection
}

type kernelETWWrapperCandidate struct {
	address         uintptr
	helper          uintptr
	continuation    uintptr
	rva             uint32
	helperRVA       uint32
	continuationRVA uint32
	sectionName     string
	signature       []byte
	tail            []byte
	context         []byte
}

func currentWindowsVersion() string {
	version := windows.RtlGetVersion()
	if version == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", version.MajorVersion, version.MinorVersion, version.BuildNumber)
}

func loadRemoteExecutableImage(process syscall.Handle, moduleBase uintptr) (remoteExecutableImage, error) {
	const initialHeaderSize = 4096
	header, ok := readRemote(process, moduleBase, initialHeaderSize)
	if !ok || len(header) < 0x40 || header[0] != 'M' || header[1] != 'Z' {
		return remoteExecutableImage{}, fmt.Errorf("module 0x%X has no readable DOS header", moduleBase)
	}
	peOffset := int(binary.LittleEndian.Uint32(header[0x3C:0x40]))
	if peOffset < 0x40 || peOffset > 0x10000 {
		return remoteExecutableImage{}, fmt.Errorf("module 0x%X has invalid PE offset 0x%X", moduleBase, peOffset)
	}
	if peOffset+24 > len(header) || string(header[peOffset:peOffset+4]) != "PE\x00\x00" {
		return remoteExecutableImage{}, fmt.Errorf("module 0x%X has no valid PE signature", moduleBase)
	}
	sectionCount := int(binary.LittleEndian.Uint16(header[peOffset+6 : peOffset+8]))
	optionalSize := int(binary.LittleEndian.Uint16(header[peOffset+20 : peOffset+22]))
	if sectionCount <= 0 || sectionCount > 96 || optionalSize < 64 || optionalSize > 4096 {
		return remoteExecutableImage{}, fmt.Errorf("module 0x%X has invalid PE table sizes: sections=%d optional=0x%X", moduleBase, sectionCount, optionalSize)
	}
	headerSize := peOffset + 24 + optionalSize + sectionCount*40
	if headerSize > 0x20000 {
		return remoteExecutableImage{}, fmt.Errorf("module 0x%X PE headers exceed the bounded reader", moduleBase)
	}
	if headerSize > len(header) {
		header, ok = readRemote(process, moduleBase, headerSize)
		if !ok || len(header) != headerSize {
			return remoteExecutableImage{}, fmt.Errorf("module 0x%X PE section table is not fully readable", moduleBase)
		}
	}
	optional := peOffset + 24
	if binary.LittleEndian.Uint16(header[optional:optional+2]) != 0x10B {
		return remoteExecutableImage{}, fmt.Errorf("module 0x%X is not a 32-bit PE image", moduleBase)
	}
	imageSize := binary.LittleEndian.Uint32(header[optional+56 : optional+60])
	if imageSize < uint32(headerSize) || imageSize > 0x10000000 {
		return remoteExecutableImage{}, fmt.Errorf("module 0x%X has invalid image size 0x%X", moduleBase, imageSize)
	}
	image := remoteExecutableImage{base: moduleBase, size: imageSize}
	pathBuffer := make([]uint16, 32768)
	if length, _, _ := procGetModuleFileNameEx.Call(
		uintptr(process), moduleBase, uintptr(unsafe.Pointer(&pathBuffer[0])), uintptr(len(pathBuffer)),
	); length != 0 {
		image.path = syscall.UTF16ToString(pathBuffer[:length])
		image.version = fileVersion(image.path)
	}
	sectionTable := optional + optionalSize
	for index := 0; index < sectionCount; index++ {
		entry := header[sectionTable+index*40 : sectionTable+(index+1)*40]
		name := strings.TrimRight(string(entry[0:8]), "\x00")
		virtualSize := binary.LittleEndian.Uint32(entry[8:12])
		virtualAddress := binary.LittleEndian.Uint32(entry[12:16])
		rawSize := binary.LittleEndian.Uint32(entry[16:20])
		characteristics := binary.LittleEndian.Uint32(entry[36:40])
		if characteristics&imageSectionMemExecute == 0 {
			continue
		}
		scanSize := virtualSize
		if scanSize == 0 {
			scanSize = rawSize
		}
		if scanSize == 0 || virtualAddress >= imageSize || scanSize > imageSize-virtualAddress {
			return remoteExecutableImage{}, fmt.Errorf("module %s has invalid executable section %q at RVA 0x%X size 0x%X", filepath.Base(image.path), name, virtualAddress, scanSize)
		}
		data, err := readRemoteExact(process, moduleBase+uintptr(virtualAddress), int(scanSize))
		if err != nil {
			return remoteExecutableImage{}, fmt.Errorf("read executable section %s+0x%X: %w", filepath.Base(image.path), virtualAddress, err)
		}
		image.sections = append(image.sections, remoteExecutableSection{
			name:            name,
			virtualAddress:  virtualAddress,
			virtualSize:     scanSize,
			characteristics: characteristics,
			data:            data,
		})
	}
	if len(image.sections) == 0 {
		return remoteExecutableImage{}, fmt.Errorf("module %s has no readable executable sections", filepath.Base(image.path))
	}
	return image, nil
}

func readRemoteExact(process syscall.Handle, address uintptr, size int) ([]byte, error) {
	if size <= 0 || size > 0x10000000 {
		return nil, fmt.Errorf("invalid bounded read size 0x%X", size)
	}
	const chunkSize = 256 * 1024
	result := make([]byte, 0, size)
	for len(result) < size {
		length := size - len(result)
		if length > chunkSize {
			length = chunkSize
		}
		chunk, ok := readRemote(process, address+uintptr(len(result)), length)
		if !ok || len(chunk) != length {
			return nil, fmt.Errorf("ReadProcessMemory 0x%X returned %d/%d bytes", address+uintptr(len(result)), len(chunk), length)
		}
		result = append(result, chunk...)
	}
	return result, nil
}

func locateKernelETWWrapper(image remoteExecutableImage) (kernelETWWrapperCandidate, error) {
	var candidates []kernelETWWrapperCandidate
	for _, section := range image.sections {
		for offset := 0; offset+10 <= len(section.data); offset++ {
			code := section.data[offset : offset+9]
			if code[0] != 0x8B || code[1] != 0xFF || code[2] != 0x51 || code[3] != 0x51 ||
				code[4] != 0xE8 || !hasHotpatchFunctionBoundary(section.data, offset) {
				continue
			}
			tailLength, ok := compactWrapperTailLength(section.data[offset+9:])
			if !ok {
				continue
			}
			rva := section.virtualAddress + uint32(offset)
			callNext := int64(image.base) + int64(rva) + 9
			helper := callNext + int64(int32(binary.LittleEndian.Uint32(code[5:9])))
			if helper < int64(image.base) || helper >= int64(image.base)+int64(image.size) {
				continue
			}
			helperRVA := uint32(helper - int64(image.base))
			if !image.executableRVA(helperRVA) || helperRVA == rva {
				continue
			}
			contextStart := offset - 16
			if contextStart < 0 {
				contextStart = 0
			}
			contextEnd := offset + 27
			if contextEnd > len(section.data) {
				contextEnd = len(section.data)
			}
			candidates = append(candidates, kernelETWWrapperCandidate{
				address:         image.base + uintptr(rva),
				helper:          uintptr(helper),
				continuation:    image.base + uintptr(rva+9),
				rva:             rva,
				helperRVA:       helperRVA,
				continuationRVA: rva + 9,
				sectionName:     section.name,
				signature:       append([]byte(nil), code...),
				tail:            append([]byte(nil), section.data[offset+9:offset+9+tailLength]...),
				context:         append([]byte(nil), section.data[contextStart:contextEnd]...),
			})
		}
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return kernelETWWrapperCandidate{}, fmt.Errorf("no bounded x86 ETW wrapper shape found in executable sections of %s", filepath.Base(image.path))
	default:
		rvas := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			rvas = append(rvas, fmt.Sprintf("%s+0x%X", candidate.sectionName, candidate.rva))
		}
		return kernelETWWrapperCandidate{}, fmt.Errorf("ETW wrapper signature is not unique in %s: %s", filepath.Base(image.path), strings.Join(rvas, ", "))
	}
}

func hasHotpatchFunctionBoundary(code []byte, offset int) bool {
	if offset < 2 {
		return false
	}
	left := code[offset-2 : offset]
	return (left[0] == 0xCC && left[1] == 0xCC) ||
		(left[0] == 0x90 && left[1] == 0x90)
}

// compactWrapperTailLength accepts only straight-line stack cleanup followed by
// a return. The wrapper's build-specific tail remains in KERNEL32 and is not
// copied into the injected stub; this parser only proves that the candidate is
// the small hotpatch wrapper shape used around the internal ETW helper.
func compactWrapperTailLength(code []byte) (int, bool) {
	const maxTailLength = 16
	limit := len(code)
	if limit > maxTailLength {
		limit = maxTailLength
	}
	for offset := 0; offset < limit; {
		switch code[offset] {
		case 0x58, 0x59, 0x5A, 0x5B, 0x5C, 0x5D, 0x5E, 0x5F, 0x90, 0xC9:
			offset++
		case 0x66:
			if offset+2 > limit || code[offset+1] != 0x90 {
				return 0, false
			}
			offset += 2
		case 0x83:
			if offset+3 > limit || code[offset+1] != 0xC4 {
				return 0, false
			}
			offset += 3
		case 0x81:
			if offset+6 > limit || code[offset+1] != 0xC4 {
				return 0, false
			}
			offset += 6
		case 0x8B:
			if offset+2 > limit || code[offset+1] != 0xE5 {
				return 0, false
			}
			offset += 2
		case 0x8D:
			if offset+4 > limit || code[offset+1] != 0x64 || code[offset+2] != 0x24 {
				return 0, false
			}
			offset += 4
		case 0xC3:
			return offset + 1, true
		case 0xC2:
			if offset+3 > limit {
				return 0, false
			}
			return offset + 3, true
		default:
			return 0, false
		}
	}
	return 0, false
}

func (image remoteExecutableImage) executableRVA(rva uint32) bool {
	for _, section := range image.sections {
		if rva >= section.virtualAddress && rva-section.virtualAddress < section.virtualSize {
			return true
		}
	}
	return false
}

func patchRemoteExecutableBytes(
	process syscall.Handle,
	moduleBase uintptr,
	address uintptr,
	expected []byte,
	replacement []byte,
) error {
	if len(expected) == 0 || len(expected) != len(replacement) {
		return fmt.Errorf("patch byte lengths differ: expected=%d replacement=%d", len(expected), len(replacement))
	}
	if err := validateRemoteExecutableAddress(process, moduleBase, address, len(replacement)); err != nil {
		return err
	}
	current, err := readRemoteExact(process, address, len(expected))
	if err != nil {
		return fmt.Errorf("verify original bytes at 0x%X: %w", address, err)
	}
	if string(current) != string(expected) {
		return fmt.Errorf("original bytes changed at 0x%X: got %X want %X", address, current, expected)
	}
	var oldProtect uint32
	protected, _, protectErr := procVirtualProtectEx.Call(
		uintptr(process), address, uintptr(len(replacement)), pageExecuteReadWrite, uintptr(unsafe.Pointer(&oldProtect)),
	)
	if protected == 0 {
		return fmt.Errorf("VirtualProtectEx writable at 0x%X: %w", address, protectErr)
	}
	writeErr := writeRemote(process, address, replacement)
	var ignored uint32
	restored, _, restoreErr := procVirtualProtectEx.Call(
		uintptr(process), address, uintptr(len(replacement)), uintptr(oldProtect), uintptr(unsafe.Pointer(&ignored)),
	)
	if writeErr != nil {
		return writeErr
	}
	if restored == 0 {
		return fmt.Errorf("restore page protection at 0x%X: %w", address, restoreErr)
	}
	flushed, _, flushErr := procFlushInstruction.Call(uintptr(process), address, uintptr(len(replacement)))
	if flushed == 0 {
		return fmt.Errorf("FlushInstructionCache at 0x%X: %w", address, flushErr)
	}
	written, err := readRemoteExact(process, address, len(replacement))
	if err != nil {
		return fmt.Errorf("verify patched bytes at 0x%X: %w", address, err)
	}
	if string(written) != string(replacement) {
		return fmt.Errorf("patched bytes mismatch at 0x%X: got %X want %X", address, written, replacement)
	}
	return nil
}

func validateRemoteExecutableAddress(process syscall.Handle, moduleBase, address uintptr, size int) error {
	if size <= 0 || address > ^uintptr(0)-uintptr(size) {
		return fmt.Errorf("invalid executable address range 0x%X+0x%X", address, size)
	}
	var info memoryBasicInformation
	queried, _, queryErr := procVirtualQueryEx.Call(
		uintptr(process), address, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
	)
	if queried == 0 {
		return fmt.Errorf("VirtualQueryEx 0x%X: %w", address, queryErr)
	}
	if info.AllocationBase != moduleBase || info.State != memCommit || address < info.BaseAddress ||
		address+uintptr(size) > info.BaseAddress+info.RegionSize || !isExecutableProtection(info.Protect) {
		return fmt.Errorf("target 0x%X is not committed executable memory owned by module 0x%X", address, moduleBase)
	}
	return nil
}

func isExecutableProtection(protection uint32) bool {
	switch protection & 0xFF {
	case 0x10, 0x20, 0x40, 0x80: // PAGE_EXECUTE variants
		return true
	default:
		return false
	}
}

func fileVersion(path string) string {
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || size == 0 {
		return ""
	}
	buffer := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&buffer[0])); err != nil {
		return ""
	}
	var fixed *windows.VS_FIXEDFILEINFO
	var fixedSize uint32
	if err := windows.VerQueryValue(unsafe.Pointer(&buffer[0]), `\`, unsafe.Pointer(&fixed), &fixedSize); err != nil ||
		fixed == nil || fixedSize < uint32(unsafe.Sizeof(*fixed)) || fixed.Signature != 0xFEEF04BD {
		return ""
	}
	versionMS := fixed.ProductVersionMS
	versionLS := fixed.ProductVersionLS
	if versionMS == 0 && versionLS == 0 {
		versionMS = fixed.FileVersionMS
		versionLS = fixed.FileVersionLS
	}
	return fmt.Sprintf("%d.%d.%d.%d",
		versionMS>>16,
		versionMS&0xFFFF,
		versionLS>>16,
		versionLS&0xFFFF,
	)
}
