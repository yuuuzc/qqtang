package winlaunch

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const readOnlyProcessAccess = 0x0400 | 0x0010 // PROCESS_QUERY_INFORMATION | PROCESS_VM_READ

// ReadProcessMemorySnapshot copies a bounded range from one local process.
// It performs no writes and is intended for evidence gathering around live
// protocol objects recovered by the diagnostic tracers.
func ReadProcessMemorySnapshot(pid uint32, address uintptr, size int) ([]byte, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	if address == 0 || address > 0xffffffff {
		return nil, fmt.Errorf("address 0x%X is not a 32-bit process address", address)
	}
	if size <= 0 || size > 0x100000 {
		return nil, fmt.Errorf("size 0x%X is outside 1..0x100000", size)
	}
	process, err := syscall.OpenProcess(readOnlyProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	data, ok := readRemote(process, address, size)
	if !ok || len(data) != size {
		return nil, fmt.Errorf("ReadProcessMemory 0x%08X size 0x%X", address, size)
	}
	return data, nil
}

// SearchProcessMemory returns exact byte-pattern addresses from committed,
// readable memory in a 32-bit local process. It is read-only and bounded by
// maxMatches so diagnostic searches cannot accumulate unbounded output.
func SearchProcessMemory(pid uint32, pattern []byte, maxMatches int) ([]uintptr, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	if len(pattern) == 0 || len(pattern) > 4096 {
		return nil, fmt.Errorf("pattern length %d is outside 1..4096", len(pattern))
	}
	if maxMatches <= 0 || maxMatches > 4096 {
		return nil, fmt.Errorf("max matches %d is outside 1..4096", maxMatches)
	}
	process, err := syscall.OpenProcess(readOnlyProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	const (
		pageGuard    = 0x100
		pageNoAccess = 0x01
		chunkSize    = 1024 * 1024
	)
	var matches []uintptr
	for address := uintptr(0x10000); address < uintptr(0x80000000) && len(matches) < maxMatches; {
		var info memoryBasicInformation
		queried, _, _ := procVirtualQueryEx.Call(uintptr(process), address, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
		if queried == 0 || info.RegionSize == 0 {
			break
		}
		next := info.BaseAddress + info.RegionSize
		if next <= address {
			break
		}
		if info.State == memCommit && info.Protect&pageGuard == 0 && info.Protect&0xff != pageNoAccess && info.Protect&0xff != 0 {
			var overlap []byte
			for chunkAddress := info.BaseAddress; chunkAddress < next && len(matches) < maxMatches; {
				size := chunkSize
				if remaining := next - chunkAddress; remaining < uintptr(size) {
					size = int(remaining)
				}
				data, ok := readRemote(process, chunkAddress, size)
				if !ok {
					break
				}
				combined := make([]byte, 0, len(overlap)+len(data))
				combined = append(combined, overlap...)
				combined = append(combined, data...)
				combinedBase := chunkAddress - uintptr(len(overlap))
				for searchAt := 0; searchAt+len(pattern) <= len(combined) && len(matches) < maxMatches; {
					relative := bytes.Index(combined[searchAt:], pattern)
					if relative < 0 {
						break
					}
					relative += searchAt
					matches = append(matches, combinedBase+uintptr(relative))
					searchAt = relative + len(pattern)
				}
				keep := len(pattern) - 1
				if keep > len(data) {
					keep = len(data)
				}
				overlap = append(overlap[:0], data[len(data)-keep:]...)
				chunkAddress += uintptr(size)
			}
		}
		address = next
	}
	return matches, nil
}

// waitForUniqueExecutableImagePattern waits for a protected PE32 image to be
// unpacked and returns only an unambiguous match in committed executable
// image memory. Compatibility patches share this primitive; it is independent
// of any individual Client or NetCenter workaround.
func waitForUniqueExecutableImagePattern(process syscall.Handle, moduleBase uintptr, pattern []byte, timeout time.Duration) (uintptr, uint32, error) {
	deadline := time.Now().Add(timeout)
	var lastCount int
	for time.Now().Before(deadline) {
		imageSize, err := remotePE32ImageSize(process, moduleBase)
		if err != nil {
			return 0, 0, err
		}
		matches := executableImagePatternMatches(process, moduleBase, uintptr(imageSize), pattern, 2)
		lastCount = len(matches)
		if len(matches) == 1 {
			return matches[0], imageSize, nil
		}
		if len(matches) > 1 {
			return 0, imageSize, fmt.Errorf("signature is ambiguous: %d executable-image matches", len(matches))
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(process), 0)
		if waitResult == waitObject0 {
			return 0, imageSize, fmt.Errorf("client exited before runtime signature became available")
		}
		time.Sleep(25 * time.Millisecond)
	}
	return 0, 0, fmt.Errorf("signature match count %d after %s", lastCount, timeout)
}

func remotePE32ImageSize(process syscall.Handle, moduleBase uintptr) (uint32, error) {
	header, ok := readRemote(process, moduleBase, 0x1000)
	if !ok || len(header) < 0x100 || header[0] != 'M' || header[1] != 'Z' {
		return 0, fmt.Errorf("invalid remote PE32 DOS header")
	}
	peOffset := int(binary.LittleEndian.Uint32(header[0x3C:0x40]))
	optional := peOffset + 24
	if optional+60 > len(header) || binary.LittleEndian.Uint16(header[optional:optional+2]) != 0x10B {
		return 0, fmt.Errorf("remote image is not PE32")
	}
	imageSize := binary.LittleEndian.Uint32(header[optional+56 : optional+60])
	if imageSize < 0x10000 || imageSize > 0x40000000 {
		return 0, fmt.Errorf("remote PE32 image size 0x%X is invalid", imageSize)
	}
	return imageSize, nil
}

func executableImagePatternMatches(process syscall.Handle, moduleBase, imageSize uintptr, pattern []byte, limit int) []uintptr {
	const (
		pageGuard    = 0x100
		pageNoAccess = 0x01
		chunkSize    = 1024 * 1024
	)
	end := moduleBase + imageSize
	matches := make([]uintptr, 0, limit)
	for address := moduleBase; address < end && len(matches) < limit; {
		var info memoryBasicInformation
		queried, _, _ := procVirtualQueryEx.Call(uintptr(process), address, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
		if queried == 0 || info.RegionSize == 0 {
			break
		}
		next := info.BaseAddress + info.RegionSize
		if next > end {
			next = end
		}
		if next <= address {
			break
		}
		protection := info.Protect & 0xFF
		executable := protection == 0x10 || protection == 0x20 || protection == 0x40 || protection == 0x80
		if info.State == memCommit && executable && info.Protect&pageGuard == 0 && protection != pageNoAccess {
			for chunkAddress := address; chunkAddress < next && len(matches) < limit; {
				size := chunkSize
				if remaining := next - chunkAddress; remaining < uintptr(size) {
					size = int(remaining)
				}
				data, ok := readRemote(process, chunkAddress, size)
				if !ok {
					break
				}
				for offset := 0; offset+len(pattern) <= len(data) && len(matches) < limit; {
					match := bytes.Index(data[offset:], pattern)
					if match < 0 {
						break
					}
					match += offset
					matches = append(matches, chunkAddress+uintptr(match))
					offset = match + len(pattern)
				}
				chunkAddress += uintptr(size)
			}
		}
		address = next
	}
	return matches
}
